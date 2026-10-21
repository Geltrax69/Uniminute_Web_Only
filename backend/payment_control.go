package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// This internal RPC is signed by the credentialed storefront after it validates
// the shopper and recalculates the basket. A browser cannot supply its own owner,
// fingerprint or amount. PostgreSQL coordinates all storefront processes.
type paymentControl struct {
	Operation   string `json:"operation"`
	Account     string `json:"account"`
	Fingerprint string `json:"fingerprint"`
	CartID      string `json:"cart_id"`
	Amount      int64  `json:"amount"`
	Receipt     string `json:"receipt"`
	Lease       string `json:"lease"`
	OrderID     string `json:"order_id"`
	Timestamp   int64  `json:"timestamp"`
}

func (a *API) handlePaymentControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	secret := strings.TrimSpace(os.Getenv("RAZORPAY_KEY_SECRET"))
	signature, sigErr := hex.DecodeString(r.Header.Get("X-Uniminute-Payment-Signature"))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if err != nil || sigErr != nil || secret == "" || !hmac.Equal(signature, mac.Sum(nil)) {
		writeError(w, 401, "invalid payment service signature")
		return
	}
	var in paymentControl
	if json.Unmarshal(body, &in) != nil || in.Timestamp < time.Now().Unix()-30 || in.Timestamp > time.Now().Unix()+30 || len(in.Account) < 3 || len(in.Account) > 254 {
		writeError(w, 400, "invalid payment control request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if in.Operation == "verify" {
		if !a.allowPaymentRequests(w, r, "confirm:"+in.Account, 30, 60) {
			return
		}
		writeJSON(w, 200, map[string]bool{"allowed": true})
		return
	}
	if len(in.Fingerprint) != 64 || len(in.CartID) < 8 || len(in.CartID) > 100 || in.Amount < 100 {
		writeError(w, 400, "invalid payment attempt")
		return
	}
	key := hashCode(in.Account + ":" + in.Fingerprint)
	if in.Operation == "finish" || in.Operation == "release" {
		var result sql.Result
		if in.Operation == "finish" && strings.HasPrefix(in.OrderID, "order_") && len(in.OrderID) < 100 {
			result, err = a.db.sql.ExecContext(ctx, `UPDATE payment_gateway_attempts SET order_id=$3 WHERE attempt_key=$1 AND lease=$2 AND (order_id='' OR order_id=$3)`, key, in.Lease, in.OrderID)
		} else if in.Operation == "release" {
			// Only a definite provider rejection may release a reservation. Timeouts
			// retain it because the provider may already have created an order.
			result, err = a.db.sql.ExecContext(ctx, `DELETE FROM payment_gateway_attempts WHERE attempt_key=$1 AND lease=$2 AND order_id=''`, key, in.Lease)
		} else {
			writeError(w, 400, "invalid payment operation")
			return
		}
		if err != nil {
			writeError(w, 503, "could not save payment status")
			return
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			writeError(w, 409, "payment attempt changed")
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
		return
	}
	if in.Operation != "reserve" {
		writeError(w, 400, "invalid payment operation")
		return
	}
	if !a.allowPaymentRequests(w, r, "start:"+in.Account, 20, 60) {
		return
	}
	tx, err := a.db.sql.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, 503, "payment protection is unavailable")
		return
	}
	defer tx.Rollback()
	// Try rather than queue on a busy account. Aborted HTTP requests cannot leave
	// a process mutex or a leased database connection locked.
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, "payment:"+in.Account).Scan(&locked); err != nil || !locked {
		w.Header().Set("Retry-After", "2")
		writeError(w, 409, "Payment is already starting. Wait a moment and try again.")
		return
	}
	var orderID, receipt, lease, cartID string
	var created time.Time
	err = tx.QueryRowContext(ctx, `SELECT order_id,receipt,lease,cart_id,created_at FROM payment_gateway_attempts WHERE attempt_key=$1`, key).Scan(&orderID, &receipt, &lease, &cartID, &created)
	state := "reuse"
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Independent creation budget: refreshing a reused order does not consume it.
		// This counter shares the account lock, so concurrent baskets cannot bypass it.
		var count int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM payment_gateway_attempts WHERE account_hash=$1 AND created_at>now()-interval '10 minutes'`, hashCode(in.Account)).Scan(&count)
		if err != nil {
			writeError(w, 503, "could not check payment attempts")
			return
		}
		// Keep the budget even when a definitely rejected reservation is released.
		if !a.allowPaymentRequests(w, r, "new:"+in.Account, 5, 600) {
			return
		}
		if count >= 5 {
			w.Header().Set("Retry-After", "600")
			writeError(w, 429, "Too many new payment attempts. Wait a few minutes before trying again.")
			return
		}
		receipt = randomPaymentToken()
		lease = randomPaymentToken()
		cartID = in.CartID
		_, err = tx.ExecContext(ctx, `INSERT INTO payment_gateway_attempts(attempt_key,account_hash,cart_id,receipt,lease) VALUES($1,$2,$3,$4,$5)`, key, hashCode(in.Account), cartID, receipt, lease)
		state = "create"
	case err != nil:
		writeError(w, 503, "could not check payment status")
		return
	case time.Since(created) > 30*time.Minute:
		writeError(w, 409, "This payment attempt expired. Check your orders and payment status before changing the cart or starting another payment.")
		return
	case orderID == "" && time.Since(created) < 30*time.Second:
		w.Header().Set("Retry-After", "3")
		writeError(w, 409, "Payment is already starting. Wait a moment and try again.")
		return
	case orderID == "":
		state = "recover"
	}
	if err != nil || tx.Commit() != nil {
		writeError(w, 503, "could not reserve payment attempt")
		return
	}
	writeJSON(w, 200, map[string]string{"state": state, "order_id": orderID, "receipt": receipt, "lease": lease, "cart_id": cartID})
}

func randomPaymentToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// Atomic fixed-window counters are shared across replicas and survive restarts.
// Denials do not extend the cooldown. Failure is closed before provider calls.
func (a *API) allowPaymentRequests(w http.ResponseWriter, r *http.Request, key string, limit, seconds int) bool {
	var retry int
	err := a.db.sql.QueryRowContext(r.Context(), `INSERT INTO payment_request_limits(key_hash,attempts,expires_at)
 VALUES($1,1,now()+$3*interval '1 second') ON CONFLICT(key_hash) DO UPDATE SET
 attempts=CASE WHEN payment_request_limits.expires_at<=now() THEN 1 ELSE payment_request_limits.attempts+1 END,
 expires_at=CASE WHEN payment_request_limits.expires_at<=now() THEN now()+$3*interval '1 second' ELSE payment_request_limits.expires_at END
 WHERE payment_request_limits.expires_at<=now() OR payment_request_limits.attempts<$2
 RETURNING GREATEST(1,ceil(extract(epoch from(expires_at-now())))::int)`, hashCode(key), limit, seconds).Scan(&retry)
	if errors.Is(err, sql.ErrNoRows) {
		retry = seconds
		_ = a.db.sql.QueryRowContext(r.Context(), `SELECT GREATEST(1,ceil(extract(epoch from(expires_at-now())))::int) FROM payment_request_limits WHERE key_hash=$1`, hashCode(key)).Scan(&retry)
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeError(w, 429, "Too many payment requests. Please wait before trying again.")
		return false
	}
	if err != nil {
		log.Printf("payment limiter: %v", err)
		writeError(w, 503, "payment protection is temporarily unavailable")
		return false
	}
	return true
}
