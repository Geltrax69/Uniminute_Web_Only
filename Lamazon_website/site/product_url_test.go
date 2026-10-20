package site

import (
	"context"
	"lamazon/website/backend"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductPublicURLAndLegacyRedirect(t *testing.T) {
	const ref = "55ef4240e2314ef58b1ba1f62b4c8a01"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/products/item-44", "/api/products/" + ref:
			w.Write([]byte(`{"id":"item-44","publicId":"` + ref + `","name":"Test phone","store":"Test Store","price":100}`))
		case "/api/products/item-44/reviews":
			w.Write([]byte(`{"average":4.5,"count":2,"reviews":[]}`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer api.Close()
	handler := New(api.URL)
	legacy := httptest.NewRecorder()
	handler.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/p/item-44", nil))
	if legacy.Code != http.StatusPermanentRedirect || legacy.Header().Get("Location") != "/p/"+ref {
		t.Fatalf("legacy redirect: %d %v", legacy.Code, legacy.Header())
	}
	current := httptest.NewRecorder()
	handler.ServeHTTP(current, httptest.NewRequest(http.MethodGet, "/p/"+ref, nil))
	if current.Code != http.StatusOK || !strings.Contains(current.Body.String(), "Visit Test Store") || !strings.Contains(current.Body.String(), "4.5 out of 5 stars, 2 ratings") {
		t.Fatalf("public URL missing product/store/reviews: %d", current.Code)
	}
}

func TestOpaqueVendorURLKeepsInternalCheckoutID(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"item-44","publicId":"public-ref","name":"Test","price":100,"offers":[{"store":"Other Shop","price":90}]}`))
	}))
	defer api.Close()
	s := &Site{backend: backend.NewBackend(api.URL)}
	p, err := s.cartProduct(context.Background(), "public-ref@Other Shop?Colour=Black")
	if err != nil || p.ID != "item-44@Other Shop?Colour=Black" {
		t.Fatalf("checkout identity: %q, %v", p.ID, err)
	}
}
