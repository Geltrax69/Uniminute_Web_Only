package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

func allowedBrowserOrigin(origin string) bool {
	for _, v := range strings.Split("https://www.uniminute.in,https://uniminute.in,https://uniminute.vercel.app,"+os.Getenv("ALLOWED_ORIGINS"), ",") {
		if strings.TrimSpace(v) == origin {
			return true
		}
	}
	// Local development must explicitly opt in through ALLOWED_ORIGINS.
	return false
}

func (a *API) handleSessionLogout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Access  string `json:"accessToken"`
		Refresh string `json:"refreshToken"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in) != nil {
		writeError(w, 400, "invalid sign-out request")
		return
	}
	if _, err := a.db.sql.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE access_hash=$1 OR refresh_hash=$2`, hashCode(in.Access), hashCode(in.Refresh)); err != nil {
		writeError(w, 503, "could not revoke the session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) handleStaffLogout(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if _, err := a.db.sql.ExecContext(r.Context(), `DELETE FROM staff_sessions WHERE token_hash=$1`, hashCode(token)); err != nil {
		writeError(w, 503, "could not revoke the session")
		return
	}
	w.WriteHeader(204)
}

func (a *API) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/admin/") || needsSession(r) {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		subject := a.owner(r)
		if subject == "" {
			subject = a.staffOf(r)
		}
		if subject != "" && r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && !a.allowPaymentRequests(w, r, "write:"+subject, 120, 60) {
			return
		}
		// Separate cost budgets avoid an upload flood consuming the search budget.
		bucket, limit := "", 0
		switch {
		case r.Method == "GET" && (r.URL.Path == "/api/products" || r.URL.Path == "/api/search"):
			bucket = "catalog"
			limit = 600
		case (strings.HasPrefix(r.URL.Path, "/api/login/") || r.URL.Path == "/api/login"):
			bucket = "auth"
			limit = 600
		case r.Method != "GET" && strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data"):
			bucket = "upload"
			limit = 20
		}
		if bucket == "upload" {
			subject := a.owner(r)
			if subject == "" {
				subject = a.staffOf(r)
			}
			if subject != "" && !a.allowPaymentRequests(w, r, "upload-day:"+subject, 100, 86400) {
				return
			}
		}
		if bucket != "" {
			subject := a.owner(r)
			if subject == "" {
				subject = a.staffOf(r)
			}
			if subject == "" {
				ip, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					ip = r.RemoteAddr
				}
				subject = "connection:" + ip
				if bucket == "upload" {
					limit = 100
				}
			}
			if !a.allowPaymentRequests(w, r, "abuse:"+bucket+":"+subject, limit, 60) {
				return
			}
		}
		// Audit metadata only. Never record request bodies, passwords, tokens, or
		// customer contact details. Failed writes retain their real response status.
		if strings.HasPrefix(r.URL.Path, "/api/admin/") && (r.Method != "GET" && r.Method != "OPTIONS" || strings.HasSuffix(r.URL.Path, "/reports/export")) {
			recorder := &auditWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(recorder, r)
			sum := sha256.Sum256([]byte(r.URL.Path))
			if _, err := a.db.sql.ExecContext(r.Context(), `INSERT INTO security_audit_events(actor,action,path_hash,status) VALUES($1,$2,$3,$4)`, a.staffOf(r), r.Method+" "+auditAction(r.URL.Path), hex.EncodeToString(sum[:]), recorder.status); err != nil {
				log.Printf("security audit unavailable: %v", err)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}
func auditAction(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/api/admin/"), "/")
	if len(parts) > 2 {
		return parts[0] + "/" + parts[len(parts)-1]
	}
	return parts[0]
}

type auditWriter struct {
	http.ResponseWriter
	status  int
	written bool
}

func (w *auditWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	w.written = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}

func (a *API) upgradePassword(ctx context.Context, table, column, subject, old, password string) {
	// Identifiers are a hard-coded allow-list, never request values.
	if !((table == "users" && column == "email") || (table == "admins" && column == "username") || (table == "riders" && column == "phone")) {
		return
	}
	var iterations int
	if _, err := fmt.Sscanf(old, "pbkdf2$%d$", &iterations); err != nil || iterations >= pbkdfIterations {
		return
	}
	hash, err := hashPassword(password)
	if err != nil {
		return
	}
	field := "pass_hash"
	if table == "riders" {
		field = "pin_hash"
	}
	_, err = a.db.sql.ExecContext(ctx, "UPDATE "+table+" SET "+field+"=$1 WHERE "+column+"=$2 AND "+field+"=$3", hash, subject, old)
	if err != nil {
		log.Printf("password hash upgrade unavailable: %v", err)
	}
}
