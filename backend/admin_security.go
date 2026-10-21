package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func mfaCipher() (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("ADMIN_MFA_KEY")))
	if err != nil || len(key) != 32 {
		return nil, errors.New("admin authenticator storage is not configured")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func sealMFA(subject, secret string) (string, error) {
	aead, err := mfaCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(secret), []byte(subject))), nil
}
func openMFA(subject, sealed string) (string, error) {
	aead, err := mfaCipher()
	if err != nil {
		return "", err
	}
	data, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(data) < aead.NonceSize() {
		return "", errors.New("invalid authenticator secret")
	}
	raw, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(subject))
	return string(raw), err
}
func totpCode(secret string, counter int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return ""
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	code := (uint32(sum[offset])&127)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000)
}
func totpCounter(secret, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return 0, false
	}
	for delta := int64(-1); delta <= 1; delta++ {
		counter := now.Unix()/30 + delta
		if hmac.Equal([]byte(totpCode(secret, counter)), []byte(code)) {
			return counter, true
		}
	}
	return 0, false
}
func recoveryHash(code string) string {
	return hashCode(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", "")))
}

// Consume a time step or recovery code atomically. Password authentication and
// its account/IP budgets run before this; codes cannot be replayed in two tabs.
func (a *API) consumeAdminMFA(r *http.Request, user, code string) (bool, error) {
	var sealed string
	if err := a.db.sql.QueryRowContext(r.Context(), `SELECT mfa_secret FROM admins WHERE username=$1`, user).Scan(&sealed); err != nil {
		return false, err
	}
	if sealed == "" {
		return true, nil
	}
	secret, err := openMFA(user, sealed)
	if err != nil {
		return false, err
	}
	if counter, ok := totpCounter(secret, strings.TrimSpace(code), time.Now()); ok {
		result, err := a.db.sql.ExecContext(r.Context(), `UPDATE admins SET mfa_last_counter=$2 WHERE username=$1 AND mfa_last_counter<$2 AND mfa_secret=$3`, user, counter, sealed)
		if err != nil {
			return false, err
		}
		n, _ := result.RowsAffected()
		return n == 1, nil
	}
	if len(code) < 12 {
		return false, nil
	}
	result, err := a.db.sql.ExecContext(r.Context(), `UPDATE admins SET mfa_recovery_hashes=array_remove(mfa_recovery_hashes,$2) WHERE username=$1 AND $2=ANY(mfa_recovery_hashes)`, user, recoveryHash(code))
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n == 1, nil
}

func (a *API) handleAdminMFA(w http.ResponseWriter, r *http.Request) {
	user := a.staffOf(r)
	var sealed, hash string
	if err := a.db.sql.QueryRowContext(r.Context(), `SELECT mfa_secret,pass_hash FROM admins WHERE username=$1`, user).Scan(&sealed, &hash); err != nil {
		writeError(w, 503, "could not read admin security settings")
		return
	}
	if r.Method == "GET" {
		_, keyErr := mfaCipher()
		var recovery int
		_ = a.db.sql.QueryRowContext(r.Context(), `SELECT cardinality(mfa_recovery_hashes) FROM admins WHERE username=$1`, user).Scan(&recovery)
		writeJSON(w, 200, map[string]any{"enabled": sealed != "", "configured": keyErr == nil, "recoveryCodesRemaining": recovery})
		return
	}
	if !a.allowPasswordAttempt(w, r, "admin-security", user) {
		return
	}
	var in struct {
		Action   string `json:"action"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in) != nil {
		writeError(w, 400, "invalid security request")
		return
	}
	if !passwordMatches(hash, in.Password) {
		writeError(w, 401, "check your current admin password")
		return
	}
	switch in.Action {
	case "begin":
		if sealed != "" {
			writeError(w, 409, "an authenticator is already enabled")
			return
		}
		raw := make([]byte, 20)
		if _, err := rand.Read(raw); err != nil {
			writeError(w, 503, "could not create authenticator")
			return
		}
		secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
		encrypted, err := sealMFA(user, secret)
		if err != nil {
			writeError(w, 503, "authenticator storage is not configured on the server")
			return
		}
		if _, err = a.db.sql.ExecContext(r.Context(), `UPDATE admins SET mfa_pending=$2,mfa_pending_expires=now()+interval '10 minutes' WHERE username=$1 AND mfa_secret=''`, user, encrypted); err != nil {
			writeError(w, 503, "could not start authenticator setup")
			return
		}
		uri := "otpauth://totp/" + url.PathEscape("Uniminute:"+user) + "?secret=" + secret + "&issuer=Uniminute&digits=6&period=30"
		writeJSON(w, 200, map[string]string{"secret": secret, "uri": uri})
	case "confirm":
		if sealed != "" {
			writeError(w, 409, "an authenticator is already enabled")
			return
		}
		var pending string
		err := a.db.sql.QueryRowContext(r.Context(), `SELECT mfa_pending FROM admins WHERE username=$1 AND mfa_pending_expires>now() AND mfa_pending<>''`, user).Scan(&pending)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 400, "setup expired; start authenticator setup again")
			return
		}
		if err != nil {
			writeError(w, 503, "could not check authenticator setup")
			return
		}
		secret, err := openMFA(user, pending)
		if err != nil {
			writeError(w, 503, "could not read authenticator setup")
			return
		}
		counter, ok := totpCounter(secret, strings.TrimSpace(in.Code), time.Now())
		if !ok {
			writeError(w, 400, "enter the current six-digit authenticator code")
			return
		}
		codes := make([]string, 10)
		hashes := make([]string, 10)
		for i := range codes {
			raw := make([]byte, 10)
			if _, err = rand.Read(raw); err != nil {
				writeError(w, 503, "could not create recovery codes")
				return
			}
			codes[i] = hex.EncodeToString(raw)
			hashes[i] = recoveryHash(codes[i])
		}
		tx, err := a.db.sql.BeginTx(r.Context(), nil)
		if err != nil {
			writeError(w, 503, "could not enable authenticator")
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `UPDATE admins SET mfa_secret=$2,mfa_pending='',mfa_pending_expires=NULL,mfa_last_counter=$3,mfa_recovery_hashes=$4 WHERE username=$1 AND mfa_secret='' AND mfa_pending=$2`, user, pending, counter, hashes)
		if err != nil {
			writeError(w, 503, "could not enable authenticator")
			return
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			writeError(w, 409, "setup changed; start again")
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, err = tx.ExecContext(r.Context(), `DELETE FROM staff_sessions WHERE role='admin' AND subject=$1 AND token_hash<>$2`, user, hashCode(token)); err != nil {
			writeError(w, 503, "could not revoke other admin sessions")
			return
		}
		if tx.Commit() != nil {
			writeError(w, 503, "could not enable authenticator")
			return
		}
		writeJSON(w, 200, map[string]any{"enabled": true, "recoveryCodes": codes})
	case "disable":
		if sealed == "" {
			writeError(w, 409, "no authenticator is enabled")
			return
		}
		ok, err := a.consumeAdminMFA(r, user, in.Code)
		if err != nil {
			writeError(w, 503, "could not verify authenticator")
			return
		}
		if !ok {
			writeError(w, 401, "authenticator or recovery code is invalid or already used")
			return
		}
		if _, err = a.db.sql.ExecContext(r.Context(), `UPDATE admins SET mfa_secret='',mfa_pending='',mfa_pending_expires=NULL,mfa_recovery_hashes='{}',mfa_last_counter=-1 WHERE username=$1`, user); err != nil {
			writeError(w, 503, "could not disable authenticator")
			return
		}
		writeJSON(w, 200, map[string]bool{"enabled": false})
	default:
		writeError(w, 400, "choose a valid security action")
	}
}

func (a *API) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.sql.QueryContext(r.Context(), `SELECT actor,action,status,created_at FROM security_audit_events ORDER BY id DESC LIMIT 100`)
	if err != nil {
		writeError(w, 503, "could not load security events")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var actor, action string
		var status int
		var at time.Time
		if rows.Scan(&actor, &action, &status, &at) != nil {
			writeError(w, 503, "could not read security events")
			return
		}
		if actor == "" {
			actor = "Sign-in attempt"
		}
		out = append(out, map[string]any{"actor": actor, "action": action, "status": status, "time": at.In(reportIST).Format("02 Jan 2006, 15:04:05")})
	}
	if rows.Err() != nil {
		writeError(w, 503, "could not read security events")
		return
	}
	writeJSON(w, 200, out)
}

func (a *API) handleAdminOperations(w http.ResponseWriter, r *http.Request) {
	// These files contain status timestamps/booleans only, never backup keys or
	// database settings. Missing/unreadable status is reported as unknown.
	read := func(path string) map[string]any {
		data, err := os.ReadFile(path)
		if err != nil {
			return map[string]any{"available": false}
		}
		var out map[string]any
		if json.Unmarshal(data, &out) != nil {
			return map[string]any{"available": false}
		}
		out["available"] = true
		return out
	}
	writeJSON(w, 200, map[string]any{"backup": read("/var/backups/uniminute/status.json"), "health": read("/var/lib/uniminute-health/status.json")})
}
