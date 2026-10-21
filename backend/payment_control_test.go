package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func controlCall(h http.Handler, in paymentControl, secret string) (int, map[string]string, string) {
	in.Timestamp = time.Now().Unix()
	body, _ := json.Marshal(in)
	req := httptest.NewRequest("POST", "/api/payments/control", bytes.NewReader(body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req.Header.Set("X-Uniminute-Payment-Signature", hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var result map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return w.Code, result, w.Header().Get("Retry-After")
}

func TestPaymentReservationConcurrentRefreshAndRestart(t *testing.T) {
	t.Setenv("RAZORPAY_KEY_SECRET", "fixture-secret")
	db := testDB(t)
	h := routes(&API{db: db})
	in := paymentControl{Operation: "reserve", Account: "buyer@example.test", Fingerprint: strings.Repeat("a", 64), CartID: "cart-fixture", Amount: 100}
	var wg sync.WaitGroup
	var mu sync.Mutex
	creates := 0
	var reservation map[string]string
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, out, _ := controlCall(h, in, "fixture-secret")
			mu.Lock()
			defer mu.Unlock()
			if status == 200 && out["state"] == "create" {
				creates++
				reservation = out
			} else if status != 409 {
				t.Errorf("parallel reservation: %d %v", status, out)
			}
		}()
	}
	wg.Wait()
	if creates != 1 {
		t.Fatalf("created %d reservations", creates)
	}
	finish := in
	finish.Operation = "finish"
	finish.Lease = reservation["lease"]
	finish.OrderID = "order_fixture"
	if status, out, _ := controlCall(h, finish, "fixture-secret"); status != 200 {
		t.Fatalf("finish: %d %v", status, out)
	}
	// New handler/process has no in-memory state but must reuse the same order.
	status, out, _ := controlCall(routes(&API{db: db}), in, "fixture-secret")
	if status != 200 || out["state"] != "reuse" || out["order_id"] != "order_fixture" || out["cart_id"] != in.CartID {
		t.Fatalf("refresh: %d %v", status, out)
	}
}

func TestPaymentControlAuthenticationLimitsAndUnknownAttempt(t *testing.T) {
	t.Setenv("RAZORPAY_KEY_SECRET", "fixture-secret")
	db := testDB(t)
	h := routes(&API{db: db})
	in := paymentControl{Operation: "reserve", Account: "buyer@example.test", Fingerprint: strings.Repeat("b", 64), CartID: "cart-fixture", Amount: 100}
	if status, _, _ := controlCall(h, in, "forged"); status != 401 {
		t.Fatalf("forged request %d", status)
	}
	status, out, _ := controlCall(h, in, "fixture-secret")
	if status != 200 || out["state"] != "create" {
		t.Fatalf("initial reservation: %d %v", status, out)
	}
	if _, err := db.sql.Exec(`UPDATE payment_gateway_attempts SET created_at=now()-interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	status, recovered, _ := controlCall(h, in, "fixture-secret")
	if status != 200 || recovered["state"] != "recover" || recovered["receipt"] != out["receipt"] {
		t.Fatalf("ambiguous attempt recreated: %d %v", status, recovered)
	}
	// Definite rejections release the reservation but retain the creation budget.
	for i := 0; i < 5; i++ {
		in.Operation = "release"
		in.Lease = recovered["lease"]
		if status, _, _ := controlCall(h, in, "fixture-secret"); status != 200 {
			t.Fatalf("release: %d", status)
		}
		in.Operation = "reserve"
		var retry string
		status, recovered, retry = controlCall(h, in, "fixture-secret")
		if i == 4 {
			if status != 429 || retry == "" {
				t.Fatalf("creation limit: %d %v %q", status, recovered, retry)
			}
			break
		}
		if status != 200 || recovered["state"] != "create" {
			t.Fatalf("new attempt %d: %d %v", i, status, recovered)
		}
	}
}

func TestPaymentVerificationRateLimitConcurrent(t *testing.T) {
	t.Setenv("RAZORPAY_KEY_SECRET", "fixture-secret")
	h := testAPI(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted, denied := 0, 0
	for i := 0; i < 45; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _, retry := controlCall(h, paymentControl{Operation: "verify", Account: "buyer@example.test"}, "fixture-secret")
			mu.Lock()
			defer mu.Unlock()
			switch status {
			case 200:
				accepted++
			case 429:
				denied++
				if retry == "" {
					t.Error("missing Retry-After")
				}
			default:
				t.Errorf("unexpected %d", status)
			}
		}()
	}
	wg.Wait()
	if accepted != 30 || denied != 15 {
		t.Fatalf("accepted %d denied %d", accepted, denied)
	}
	if _, err := lastTestDB.sql.Exec(`UPDATE payment_request_limits SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := controlCall(h, paymentControl{Operation: "verify", Account: "buyer@example.test"}, "fixture-secret"); status != 200 {
		t.Fatalf("expired budget did not reset: %d", status)
	}
}
