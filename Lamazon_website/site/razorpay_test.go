package site

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type unusedOrderCreator struct{}

func (unusedOrderCreator) Create(map[string]interface{}, map[string]string) (map[string]interface{}, error) {
	return nil, nil
}

func TestVerifyPaymentSignature(t *testing.T) {
	const (
		orderID   = "order_test_123"
		paymentID = "pay_test_456"
		secret    = "test-secret"
	)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(orderID + "|" + paymentID))
	signature := hex.EncodeToString(mac.Sum(nil))

	if !verifyPaymentSignature(orderID, paymentID, signature, secret) {
		t.Fatal("valid Razorpay signature was rejected")
	}
	if verifyPaymentSignature(orderID, paymentID+"x", signature, secret) {
		t.Fatal("signature for a different payment was accepted")
	}
	if verifyPaymentSignature(orderID, paymentID, "not-hex", secret) {
		t.Fatal("malformed signature was accepted")
	}
}

func TestPaymentStateRoundTripAndTamper(t *testing.T) {
	s := &Site{razorpayKeySecret: "test-secret"}
	state := paymentState{
		OrderID: "order_test_123", Amount: 12500, CartID: "cart-request-123",
		Address: "addr-1", Expires: time.Now().Add(time.Minute).Unix(),
	}
	request := httptest.NewRequest(http.MethodPost, "https://example.test/api/create-order", nil)
	recorder := httptest.NewRecorder()
	if err := s.setPaymentState(recorder, request, state); err != nil {
		t.Fatalf("setPaymentState: %v", err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("payment cookie was not hardened: %+v", cookies)
	}

	verifyRequest := httptest.NewRequest(http.MethodPost, "/api/verify-payment", nil)
	verifyRequest.AddCookie(cookies[0])
	got, err := s.readPaymentState(verifyRequest)
	if err != nil || got != state {
		t.Fatalf("readPaymentState = %+v, %v", got, err)
	}

	tampered := *cookies[0]
	tampered.Value = strings.Replace(tampered.Value, "a", "b", 1)
	tamperedRequest := httptest.NewRequest(http.MethodPost, "/api/verify-payment", nil)
	tamperedRequest.AddCookie(&tampered)
	if _, err := s.readPaymentState(tamperedRequest); err == nil {
		t.Fatal("tampered payment state was accepted")
	}
}

func TestCreateOrderRejectsSubMinimumAmount(t *testing.T) {
	s := &Site{razorpay: unusedOrderCreator{}, razorpayKeyID: "test-key", razorpayKeySecret: "test-secret"}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/create-order",
		strings.NewReader(`{"amount":99,"currency":"INR","receipt":"cart-1"}`))
	s.handleCreateRazorpayOrder(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "at least") {
		t.Fatalf("create order: got %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestUnconfiguredStorefrontProxiesPaymentEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/create-order" {
			t.Fatalf("proxied path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != `{"amount":100,"currency":"INR","receipt":"cart-1"}` {
			t.Fatalf("proxied body = %q, %v", body, err)
		}
		http.SetCookie(w, &http.Cookie{Name: razorpayStateCookie, Value: "signed-state", Path: "/", HttpOnly: true})
		paymentJSON(w, http.StatusCreated, map[string]string{"order_id": "order_test"})
	}))
	defer upstream.Close()

	s := &Site{paymentProxy: paymentEndpointProxy(upstream.URL)}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/create-order",
		strings.NewReader(`{"amount":100,"currency":"INR","receipt":"cart-1"}`))
	s.handleCreateRazorpayOrder(recorder, request)

	if recorder.Code != http.StatusCreated || !strings.Contains(recorder.Body.String(), "order_test") {
		t.Fatalf("proxy response: got %d %q", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != razorpayStateCookie || cookies[0].Value != "signed-state" {
		t.Fatalf("proxied cookies = %+v", cookies)
	}
}

func TestVerifyPaymentRequiresAllCheckoutFields(t *testing.T) {
	s := &Site{razorpayKeySecret: "test-secret"}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/verify-payment",
		strings.NewReader(`{"razorpay_order_id":"order_1"}`))
	s.handleVerifyRazorpayPayment(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "required") {
		t.Fatalf("verify payment: got %d %q", recorder.Code, recorder.Body.String())
	}
}
