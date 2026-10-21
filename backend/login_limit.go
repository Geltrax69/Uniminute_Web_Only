package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
)

// Shared database counters survive restarts and coordinate API replicas.
// Count before password hashing so parallel requests cannot outrun the limit.
// Keys are hashed to avoid persisting attempted account names or IP addresses.
func (a *API) allowPasswordAttempt(w http.ResponseWriter, r *http.Request, realm, account string) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	ipBudget := 100
	if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLoopback() {
		ipBudget = 1000
	}
	// Do not trust user-controlled forwarding headers. Behind a reverse proxy
	// this is a shared proxy budget; account limits still apply independently.
	// Keep it generous enough for a campus sharing a NAT or the website proxy.
	for _, bucket := range []struct {
		key   string
		limit int
	}{
		{"ip:" + ip, ipBudget}, {"account:" + realm + ":" + account, 10},
	} {
		var retry int
		err := a.db.sql.QueryRowContext(r.Context(), `
   INSERT INTO password_attempts (key_hash, attempts, expires_at)
   VALUES ($1, 1, now() + interval '15 minutes')
   ON CONFLICT (key_hash) DO UPDATE SET
    attempts = CASE WHEN password_attempts.expires_at <= now()
      THEN 1 ELSE password_attempts.attempts + 1 END,
    expires_at = CASE WHEN password_attempts.expires_at <= now()
      THEN now() + interval '15 minutes' ELSE password_attempts.expires_at END
   WHERE password_attempts.expires_at <= now() OR password_attempts.attempts < $2
   RETURNING GREATEST(1, ceil(extract(epoch from (expires_at - now())))::int)`,
			hashCode(bucket.key), bucket.limit).Scan(&retry)
		if errors.Is(err, sql.ErrNoRows) {
			// The denied request does not extend the lockout.
			retry = 900
			_ = a.db.sql.QueryRowContext(r.Context(), `SELECT GREATEST(1,
    ceil(extract(epoch from (expires_at - now())))::int)
    FROM password_attempts WHERE key_hash = $1`, hashCode(bucket.key)).Scan(&retry)
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeError(w, http.StatusTooManyRequests, "too many sign-in attempts — try again in a few minutes")
			return false
		}
		if err != nil {
			log.Printf("sign-in limiter: %v", err)
			writeError(w, http.StatusServiceUnavailable, "sign-in is temporarily unavailable")
			return false
		}
	}
	return true
}

func (a *API) clearPasswordAttempts(ctx context.Context, realm, account string) {
	// Retain the IP budget even after success; only this account is reset.
	if _, err := a.db.sql.ExecContext(ctx, `DELETE FROM password_attempts
  WHERE key_hash = $1 OR expires_at <= now()`, hashCode("account:"+realm+":"+account)); err != nil {
		log.Printf("clear sign-in attempts: %v", err)
	}
}
