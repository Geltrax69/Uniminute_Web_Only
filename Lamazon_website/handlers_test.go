package main

import (
	"net/http/httptest"
	"testing"

	"lamazon/website/shop"
)

// A sign-in link must never be able to send a shopper off the site.
func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"":                    "/",
		"/cart":               "/cart",
		"/search?q=a":         "/search?q=a",
		"//evil.example":      "/",
		"/\\evil.example":     "/",
		"https://evil.example": "/",
		"javascript:alert(1)": "/",
	} {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// The basket survives a round trip through its cookie.
func TestCartCookieRoundTrip(t *testing.T) {
	rec := httptest.NewRecorder()
	shop.SaveCart(rec, []shop.CartLine{{ID: "item-1", Qty: 2}})
	req := httptest.NewRequest("GET", "/cart", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	lines := shop.ReadCart(req)
	if len(lines) != 1 || lines[0].ID != "item-1" || lines[0].Qty != 2 {
		t.Fatalf("ReadCart after SaveCart = %+v", lines)
	}
}
