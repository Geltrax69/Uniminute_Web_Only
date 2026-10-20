package site

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStorefrontForwardsWebhookBodyAndSignatureUnchanged(t *testing.T) {
	const body = "{\n  \"entity\": \"event\", \"event\": \"payment.captured\"\n}\n"
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		raw, err := io.ReadAll(r.Body)
		if err != nil || string(raw) != body {
			t.Errorf("webhook raw body changed in proxy")
		}
		if r.URL.Path != "/api/razorpay/webhook" || r.Header.Get("X-Razorpay-Signature") != "signature-fixture" || r.Header.Get("X-Razorpay-Event-Id") != "event-fixture" {
			t.Errorf("webhook route or signature headers changed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"received":true}`))
	}))
	defer upstream.Close()
	handler := New(upstream.URL)
	r := httptest.NewRequest(http.MethodPost, "https://www.uniminute.in/api/razorpay/webhook", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Razorpay-Signature", "signature-fixture")
	r.Header.Set("X-Razorpay-Event-Id", "event-fixture")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if !called || w.Code != 200 || !strings.Contains(w.Body.String(), `"received":true`) {
		t.Fatalf("webhook proxy status=%d called=%v", w.Code, called)
	}
}
