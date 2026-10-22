package site

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lamazon/website/backend"
	"lamazon/website/shop"
)

func TestPaymentAvailabilityUsesFreshStoreAndRiderState(t *testing.T) {
	delivery, closed := true, false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/availability":
			json.NewEncoder(w).Encode(map[string]bool{"deliveryAvailable": delivery})
		case "/api/products/item-1":
			json.NewEncoder(w).Encode(backend.Product{ID: "item-1", Store: "Test shop", Price: 100, StoreClosed: closed})
		default:
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	s := &Site{backend: backend.NewBackend(api.URL)}
	req := httptest.NewRequest("POST", "/api/create-order", nil)
	rec := httptest.NewRecorder()
	shop.SaveCart(rec, []shop.CartLine{{ID: "item-1", Qty: 1}})
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	if err := s.checkNewPaymentAvailability(req); err != nil {
		t.Fatal(err)
	}
	closed = true
	if err := s.checkNewPaymentAvailability(req); err == nil || !strings.Contains(err.Error(), "closed today") {
		t.Fatalf("fresh closed state ignored: %v", err)
	}
	closed = false
	delivery = false
	if err := s.checkNewPaymentAvailability(req); err == nil || !strings.Contains(err.Error(), "riders are off") {
		t.Fatalf("off-duty delivery ignored: %v", err)
	}
	delivery = true
	if err := s.checkNewPaymentAvailability(req); err != nil {
		t.Fatal("availability did not recover", err)
	}
}
