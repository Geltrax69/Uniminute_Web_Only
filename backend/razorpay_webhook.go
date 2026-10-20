package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const maxRazorpayWebhookBytes = 1 << 20

type razorpayWebhookEntity struct {
	ID        string `json:"id"`
	OrderID   string `json:"order_id"`
	PaymentID string `json:"payment_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
}

type razorpayWebhook struct {
	Entity    string `json:"entity"`
	Event     string `json:"event"`
	CreatedAt int64  `json:"created_at"`
	Payload   struct {
		Payment struct {
			Entity razorpayWebhookEntity `json:"entity"`
		} `json:"payment"`
		Order struct {
			Entity razorpayWebhookEntity `json:"entity"`
		} `json:"order"`
		Refund struct {
			Entity razorpayWebhookEntity `json:"entity"`
		} `json:"refund"`
	} `json:"payload"`
}

func validWebhookSignature(body []byte, signature, secret string) bool {
	got, err := hex.DecodeString(signature)
	if err != nil || len(got) != sha256.Size || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// Webhooks are a durable, signed audit inbox. They never create a customer
// order from an unbound provider event or overwrite a verified order on a late
// failure. Checkout still checks the payment, cart, amount and stock itself.
func (a *API) handleRazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	secret := strings.TrimSpace(os.Getenv("RAZORPAY_WEBHOOK_SECRET"))
	if secret == "" {
		writeError(w, http.StatusServiceUnavailable, "webhook is not configured")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRazorpayWebhookBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "webhook is too large")
		} else {
			writeError(w, http.StatusBadRequest, "could not read webhook")
		}
		return
	}
	if !validWebhookSignature(body, r.Header.Get("X-Razorpay-Signature"), secret) {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	eventID := strings.TrimSpace(r.Header.Get("X-Razorpay-Event-Id"))
	if eventID == "" || len(eventID) > 200 {
		writeError(w, http.StatusBadRequest, "webhook event ID is required")
		return
	}
	var event razorpayWebhook
	if json.Unmarshal(body, &event) != nil || event.Entity != "event" || event.Event == "" {
		writeError(w, http.StatusBadRequest, "invalid webhook event")
		return
	}
	var entity razorpayWebhookEntity
	var paymentID, orderID, refundID string
	switch event.Event {
	case "payment.captured", "payment.failed":
		entity = event.Payload.Payment.Entity
		paymentID, orderID = entity.ID, entity.OrderID
	case "order.paid":
		entity = event.Payload.Order.Entity
		orderID, paymentID = entity.ID, event.Payload.Payment.Entity.ID
	case "refund.processed":
		entity = event.Payload.Refund.Entity
		refundID, paymentID = entity.ID, entity.PaymentID
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"received": true, "ignored": true})
		return
	}
	if entity.ID == "" || entity.Amount < 0 || entity.Currency == "" || entity.Status == "" {
		writeError(w, http.StatusBadRequest, "webhook entity is incomplete")
		return
	}
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	// A single atomic insert acknowledges a delivery only after it is durable.
	result, err := a.db.sql.ExecContext(ctx, `
  INSERT INTO razorpay_webhook_events
   (event_id, event_type, payload_hash, payment_id, order_id, refund_id,
    amount_paise, currency, provider_status, provider_created_at)
  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
  ON CONFLICT (event_id) DO NOTHING`, eventID, event.Event, hash, paymentID,
		orderID, refundID, entity.Amount, entity.Currency, entity.Status, event.CreatedAt)
	if err != nil {
		log.Printf("razorpay webhook persistence failed: %v", err)
		writeError(w, http.StatusServiceUnavailable, "could not save webhook; retry delivery")
		return
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not confirm webhook storage")
		return
	}
	if inserted == 0 {
		var previousHash string
		if err := a.db.sql.QueryRowContext(ctx, `SELECT payload_hash FROM razorpay_webhook_events WHERE event_id=$1`, eventID).Scan(&previousHash); err != nil {
			writeError(w, http.StatusServiceUnavailable, "could not confirm previous webhook")
			return
		}
		if previousHash != hash {
			writeError(w, http.StatusConflict, "event ID has different content")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"received": true, "duplicate": inserted == 0})
}
