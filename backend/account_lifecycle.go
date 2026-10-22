package main

import (
	"database/sql"
	"net/http"
	"strings"
)

// Account erasure and writes share a database lock. A request authenticated
// before erasure must recheck its session after acquiring the write lock.
func (a *API) lockAccountWrite(w http.ResponseWriter, r *http.Request, tx *sql.Tx) bool {
	if _, err := tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "account-lifecycle:"+a.owner(r)); err != nil {
		writeError(w, 503, "Could not lock account. Please try again.")
		return false
	}
	var valid bool
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE access_hash=$1 AND email=$2 AND expires_at>now())`, hashCode(token), a.owner(r)).Scan(&valid); err != nil || !valid {
		writeError(w, 401, "Your account session ended. Sign in again.")
		return false
	}
	return true
}
