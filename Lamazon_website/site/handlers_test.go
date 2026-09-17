package site

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"lamazon/website/backend"
	"lamazon/website/shop"
)

// A sign-in link must never be able to send a shopper off the site.
func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"":                     "/",
		"/cart":                "/cart",
		"/search?q=a":          "/search?q=a",
		"//evil.example":       "/",
		"/\\evil.example":      "/",
		"https://evil.example": "/",
		"javascript:alert(1)":  "/",
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

// A panicking handler must answer with the error screen, not a dropped connection.
func TestRecovererShowsErrorScreen(t *testing.T) {
	h := recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "Something went wrong") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestBackendDown(t *testing.T) {
	if !backendDown(&url.Error{Op: "Get", URL: "x", Err: errors.New("refused")}) {
		t.Error("unreachable API should count as down")
	}
	if backendDown(&backend.APIError{Status: 404}) || !backendDown(&backend.APIError{Status: 502}) {
		t.Error("only 5xx answers count as down")
	}
	if backendDown(errors.New("no offer from shop")) {
		t.Error("a plain error is not an outage")
	}
}
