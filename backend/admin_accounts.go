package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
)

// Buyer-only deletion removes the live identity. Financial order rows retain
// their totals, but lose every buyer identifier and recipient snapshot.
func (a *API) handleAdminDeleteBuyer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in) != nil {
		writeError(w, 400, "Invalid deletion request.")
		return
	}
	admin := a.staffOf(r)
	if !a.allowPasswordAttempt(w, r, "admin-delete-buyer", admin) {
		return
	}
	var passwordHash string
	if err := a.db.sql.QueryRowContext(r.Context(), `SELECT pass_hash FROM admins WHERE username=$1`, admin).Scan(&passwordHash); err != nil {
		writeError(w, 503, "Could not verify administrator.")
		return
	}
	if !passwordMatches(passwordHash, in.Password) {
		writeError(w, 401, "Check your current admin password.")
		return
	}
	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 503, "Could not start deletion.")
		return
	}
	defer tx.Rollback()
	var email string
	err = tx.QueryRowContext(r.Context(), `SELECT email FROM users WHERE public_id=$1`, r.PathValue("id")).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "This buyer no longer exists. Refresh the People list.")
		return
	}
	if err != nil {
		writeError(w, 503, "Could not read buyer.")
		return
	}
	if strings.TrimSpace(in.Email) != email {
		writeError(w, 400, "Type the buyer’s email address exactly to confirm deletion.")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "account-lifecycle:"+email); err != nil {
		writeError(w, 503, "Could not lock buyer account.")
		return
	}
	var locked string
	if err = tx.QueryRowContext(r.Context(), `SELECT email FROM users WHERE public_id=$1 AND email=$2 FOR UPDATE`, r.PathValue("id"), email).Scan(&locked); err != nil {
		writeError(w, 409, "The buyer changed. Refresh the People list.")
		return
	}
	var seller, active, payment bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM seller_stores WHERE owner=$1),EXISTS(SELECT 1 FROM orders WHERE buyer_email=$1 AND stage NOT IN ('delivered','rejected')),EXISTS(SELECT 1 FROM payment_gateway_attempts p WHERE account_hash=$2 AND created_at>now()-interval '30 minutes' AND NOT EXISTS(SELECT 1 FROM orders o WHERE o.razorpay_order_id=p.order_id AND p.order_id<>''))`, email, hashCode(email)).Scan(&seller, &active, &payment); err != nil {
		writeError(w, 503, "Could not check linked records.")
		return
	}
	if seller {
		writeError(w, 409, "Store owners cannot be deleted here. Suspend their store instead.")
		return
	}
	if active {
		writeError(w, 409, "This buyer has active orders. Complete or reject those orders before deleting the account.")
		return
	}
	if payment {
		writeError(w, 409, "This buyer has a recent payment attempt. Wait for its payment status to settle before deleting the account.")
		return
	}
	queries := []struct{ query, argument string }{
		{`DELETE FROM auth_sessions WHERE email=$1`, email},
		{`DELETE FROM login_codes WHERE email=$1`, email},
		{`DELETE FROM push_subscriptions WHERE email=$1`, email},
		{`DELETE FROM notifications WHERE email=$1`, email},
		{`DELETE FROM checkout_attempts WHERE buyer_email=$1`, email},
		{`DELETE FROM order_reviews WHERE buyer_email=$1 OR order_id IN(SELECT id FROM orders WHERE buyer_email=$1)`, email},
		{`UPDATE orders SET buyer_email='deleted-buyer',receiver_name='Deleted buyer',receiver_phone='',receiver_address='',delivery_code='',reject_reason='' WHERE buyer_email=$1`, email},
		{`DELETE FROM payment_gateway_attempts WHERE account_hash=$1`, hashCode(email)},
		{`DELETE FROM users WHERE email=$1`, email},
	}
	for _, statement := range queries {
		if _, err = tx.ExecContext(r.Context(), statement.query, statement.argument); err != nil {
			writeError(w, 503, "Deletion failed. No account data was changed.")
			return
		}
	}
	// Counters store only hashes, but clear account-scoped keys as well so
	// the new signup does not inherit an old account's sign-in lockout.
	for _, realm := range []string{"shopper", "profile-password"} {
		if _, err = tx.ExecContext(r.Context(), `DELETE FROM password_attempts WHERE key_hash=$1`, hashCode("account:"+realm+":"+email)); err != nil {
			writeError(w, 503, "Could not clear sign-in state.")
			return
		}
	}
	for _, prefix := range []string{"identity:/api/login:", "identity:/api/login/forgot:", "checkout:", "start:", "new:", "confirm:", "write:", "upload-day:", "abuse:upload:", "abuse:write:"} {
		if _, err = tx.ExecContext(r.Context(), `DELETE FROM payment_request_limits WHERE key_hash=$1`, hashCode(prefix+email)); err != nil {
			writeError(w, 503, "Could not clear account request state.")
			return
		}
	}

	// Prevent environment bootstrap from recreating an intentionally deleted
	// demo buyer on the next restart. The marker contains no personal data.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SEED_USER")), email) {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO storefront_migrations(id) VALUES('shopper-bootstrap-disabled') ON CONFLICT DO NOTHING`); err != nil {
			writeError(w, 503, "Could not disable account bootstrap.")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 503, "Could not finish deletion.")
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}

func (a *API) handleSuspendStore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, 400, "Enter a suspension reason.")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if err := textLimit(in.Reason, "suspension reason", 500, true); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	a.changeSuspension(w, r, "approved", "suspended", in.Reason)
}
func (a *API) handleRestoreStore(w http.ResponseWriter, r *http.Request) {
	a.changeSuspension(w, r, "suspended", "approved", "")
}
func (a *API) changeSuspension(w http.ResponseWriter, r *http.Request, from, to, reason string) {
	result, err := a.db.sql.ExecContext(r.Context(), `UPDATE seller_stores SET status=$3,reject_reason=$4,reviewed_at=now() WHERE owner=$1 AND status=$2`, r.PathValue("owner"), from, to, reason)
	if err != nil {
		writeError(w, 503, "Could not change store status.")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		writeError(w, 409, "The store status changed. Refresh and try again.")
		return
	}
	writeJSON(w, 200, map[string]string{"status": to})
}
