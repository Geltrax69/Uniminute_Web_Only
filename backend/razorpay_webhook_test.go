package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const webhookTestSecret = "isolated-webhook-test-secret"

func signedWebhookRequest(body, id string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/razorpay/webhook", strings.NewReader(body))
	mac := hmac.New(sha256.New, []byte(webhookTestSecret))
	mac.Write([]byte(body))
	r.Header.Set("X-Razorpay-Signature", hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-Razorpay-Event-Id", id)
	return r
}

const capturedWebhook = `{"entity":"event","event":"payment.captured","created_at":1700000000,"payload":{"payment":{"entity":{"id":"pay_fixture","order_id":"order_fixture","amount":100,"currency":"INR","status":"captured"}}}}`

func TestWebhookRejectsUnsignedTamperedAndUnconfiguredRequests(t *testing.T) {
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", webhookTestSecret)
	handler := routes(&API{})
	for _, signature := range []string{"", "not-hex", strings.Repeat("0", 64)} {
		r := signedWebhookRequest(capturedWebhook, "evt-fixture")
		r.Header.Set("X-Razorpay-Signature", signature)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid signature returned %d", w.Code)
		}
	}
	r := signedWebhookRequest(capturedWebhook, "evt-fixture")
	r.Body = http.NoBody
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("tampered body returned %d", w.Code)
	}
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, signedWebhookRequest(capturedWebhook, "evt-fixture"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured webhook returned %d", w.Code)
	}
}

func TestWebhookValidatesBodyAndAcknowledgesUnsupportedEvents(t *testing.T) {
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", webhookTestSecret)
	handler := routes(&API{})
	for _, tc := range []struct {
		body, id string
		status   int
	}{
		{capturedWebhook, "", 400}, {`{`, "evt-invalid", 400},
		{`{"entity":"event","event":"payment.captured","payload":{}}`, "evt-incomplete", 400},
		{`{"entity":"event","event":"endpoint.validation"}`, "evt-probe", 200},
		{strings.Repeat("x", maxRazorpayWebhookBytes+1), "evt-large", 413},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, signedWebhookRequest(tc.body, tc.id))
		if w.Code != tc.status {
			t.Fatalf("event %q: status %d, want %d", tc.id, w.Code, tc.status)
		}
	}
}

func TestWebhookPersistsOnceUnderConcurrentRetries(t *testing.T) {
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", webhookTestSecret)
	db := testDB(t)
	handler := routes(&API{db: db})
	var group sync.WaitGroup
	statuses := make(chan int, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, signedWebhookRequest(capturedWebhook, "evt-same"))
			statuses <- w.Code
		}()
	}
	group.Wait()
	close(statuses)
	for code := range statuses {
		if code != 200 {
			t.Fatalf("duplicate delivery returned %d", code)
		}
	}
	var count int
	if err := db.sql.QueryRow(`SELECT count(*) FROM razorpay_webhook_events WHERE event_id='evt-same'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("stored events=%d, err=%v", count, err)
	}
	changed := strings.Replace(capturedWebhook, `"amount":100`, `"amount":200`, 1)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, signedWebhookRequest(changed, "evt-same"))
	if w.Code != 409 {
		t.Fatalf("conflicting reused event ID returned %d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, signedWebhookRequest(capturedWebhook, "evt-same"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"duplicate":true`) {
		t.Fatalf("retry response=%d %s", w.Code, w.Body.String())
	}
}

func TestWebhookRecordsOutOfOrderPaymentsAndRefundsWithoutPlacingOrders(t *testing.T) {
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", webhookTestSecret)
	db := testDB(t)
	handler := routes(&API{db: db})
	bodies := []string{
		capturedWebhook,
		strings.ReplaceAll(strings.ReplaceAll(capturedWebhook, "payment.captured", "payment.failed"), `"status":"captured"`, `"status":"failed"`),
		`{"entity":"event","event":"order.paid","payload":{"order":{"entity":{"id":"order_fixture","amount":100,"currency":"INR","status":"paid"}},"payment":{"entity":{"id":"pay_fixture"}}}}`,
		`{"entity":"event","event":"refund.processed","payload":{"refund":{"entity":{"id":"rfnd_fixture","payment_id":"pay_fixture","amount":100,"currency":"INR","status":"processed"}}}}`,
	}
	for i, body := range bodies {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, signedWebhookRequest(body, fmt.Sprintf("evt-%d", i)))
		if w.Code != 200 {
			t.Fatalf("delivery %d returned %d %s", i, w.Code, w.Body.String())
		}
	}
	var events, orders int
	if err := db.sql.QueryRow(`SELECT count(*) FROM razorpay_webhook_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRow(`SELECT count(*) FROM orders`).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if events != 4 || orders != 0 {
		t.Fatalf("events=%d, unexpected customer orders=%d", events, orders)
	}
	if _, err := db.sql.Exec(`DROP TABLE razorpay_webhook_events`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, signedWebhookRequest(capturedWebhook, "evt-storage-unavailable"))
	if w.Code != 503 {
		t.Fatalf("failed durable write acknowledged with %d", w.Code)
	}
}
