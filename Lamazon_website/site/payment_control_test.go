package site

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"lamazon/website/shop"
)

func TestPaymentBasketStableAcrossRefreshAndLineOrder(t *testing.T) {
	cookie := func(lines []shop.CartLine) []*http.Cookie {
		w := httptest.NewRecorder()
		shop.SaveCart(w, lines)
		return w.Result().Cookies()
	}
	req := httptest.NewRequest("POST", "/", nil)
	for _, c := range cookie([]shop.CartLine{{ID: "a", Qty: 1}, {ID: "b", Qty: 2}}) {
		if c.Name == shop.CartCookie {
			req.AddCookie(c)
		}
	}
	fp, rid := paymentBasket(req, "buyer", "address", "key", 100)
	req.AddCookie(&http.Cookie{Name: shop.CartRIDCookie, Value: rid})
	fp2, _ := paymentBasket(req, "buyer", "address", "key", 100)
	if fp != fp2 {
		t.Fatal("first response cookie changed fingerprint")
	}
	reversed := httptest.NewRequest("POST", "/", nil)
	for _, c := range cookie([]shop.CartLine{{ID: "b", Qty: 2}, {ID: "a", Qty: 1}}) {
		if c.Name == shop.CartCookie {
			reversed.AddCookie(c)
		}
	}
	fp2, _ = paymentBasket(reversed, "buyer", "address", "key", 100)
	if fp != fp2 {
		t.Fatal("line order defeated reuse")
	}
	for _, args := range []struct {
		account, address, key string
		amount                int64
	}{{"other", "address", "key", 100}, {"buyer", "other", "key", 100}, {"buyer", "address", "other", 100}, {"buyer", "address", "key", 200}} {
		changed, _ := paymentBasket(req, args.account, args.address, args.key, args.amount)
		if changed == fp {
			t.Fatal("different checkout reused fingerprint")
		}
	}
}

func TestPaymentControlSignedRPCAndCooldown(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("fixture"))
		mac.Write(body)
		if r.Header.Get("X-Uniminute-Payment-Signature") != hex.EncodeToString(mac.Sum(nil)) {
			t.Error("invalid RPC signature")
		}
		w.Header().Set("Retry-After", "12")
		paymentError(w, 429, "Please wait")
	}))
	defer upstream.Close()
	s := &Site{apiBase: upstream.URL, razorpayKeySecret: "fixture"}
	_, err := s.paymentControl(context.Background(), paymentControlRequest{Operation: "verify", Account: "buyer"})
	var limited *paymentControlError
	if !errors.As(err, &limited) || limited.status != 429 || limited.retry != "12" {
		t.Fatalf("rate limit lost: %v", err)
	}
}

type recoveryOrder struct {
	creates int
	items   []interface{}
}

func (f *recoveryOrder) Create(map[string]interface{}, map[string]string) (map[string]interface{}, error) {
	f.creates++
	return nil, errors.New("timed out")
}
func (f *recoveryOrder) Fetch(id string, _ map[string]interface{}, _ map[string]string) (map[string]interface{}, error) {
	return map[string]interface{}{"id": id}, nil
}
func (f *recoveryOrder) All(map[string]interface{}, map[string]string) (map[string]interface{}, error) {
	return map[string]interface{}{"items": f.items}, nil
}
func (f *recoveryOrder) Payments(string, map[string]interface{}, map[string]string) (map[string]interface{}, error) {
	return map[string]interface{}{"items": f.items}, nil
}

func TestAmbiguousPaymentNeverCreatesAnotherOrder(t *testing.T) {
	provider := &recoveryOrder{}
	s := &Site{razorpay: provider}
	_, err := s.reservedRazorpayOrder(context.Background(), paymentControlRequest{Amount: 100}, paymentReservation{State: "recover", Receipt: "original"})
	if err == nil || provider.creates != 0 {
		t.Fatalf("unknown attempt retried creation: %v %d", err, provider.creates)
	}
	provider.items = []interface{}{map[string]interface{}{"id": "order_wrong", "receipt": "original-extra"}, map[string]interface{}{"id": "order_original", "receipt": "original"}}
	order, err := s.reservedRazorpayOrder(context.Background(), paymentControlRequest{Amount: 100}, paymentReservation{State: "recover", Receipt: "original"})
	if err != nil || order["id"] != "order_original" || provider.creates != 0 {
		t.Fatalf("receipt reconciliation: %v %v", order, err)
	}
}

func TestRecoveredProofRequiresCapturedMatchingPayment(t *testing.T) {
	payment := map[string]interface{}{"id": "pay_fixture", "order_id": "order_fixture", "amount": float64(100), "currency": "INR", "status": "captured"}
	provider := &recoveryOrder{items: []interface{}{payment}}
	s := &Site{razorpay: provider, razorpayKeySecret: "fixture"}
	proof, pending, err := s.existingRazorpayPayment("order_fixture", 100)
	if err != nil || pending || proof == nil || !verifyPaymentSignature(proof.OrderID, proof.PaymentID, proof.Signature, "fixture") {
		t.Fatalf("recovery: %v %v %v", proof, pending, err)
	}
	for _, status := range []string{"authorized", "created", "refunded"} {
		payment["status"] = status
		proof, pending, err = s.existingRazorpayPayment("order_fixture", 100)
		if proof != nil || !pending || err != nil {
			t.Fatalf("unsafe status %s accepted", status)
		}
	}
	payment["status"] = "failed"
	proof, pending, _ = s.existingRazorpayPayment("order_fixture", 100)
	if proof != nil || pending {
		t.Fatal("failed attempt blocked reuse")
	}
}
