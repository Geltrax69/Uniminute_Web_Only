package site

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserOriginChecksRunBeforeCredentialProxying(t *testing.T) {
	calls := 0
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	for _, path := range []string{"/cart/add", "/api/create-order", "/staff-api/admin/security/mfa", "/account/profile"} {
		req := httptest.NewRequest("POST", "https://www.uniminute.in"+path, nil)
		req.Header.Set("Origin", "https://evil.invalid")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Errorf("cross-site write reached %s: %d", path, w.Code)
		}
	}
	if calls != 0 {
		t.Fatal("cross-site request reached application")
	}
	req := httptest.NewRequest("POST", "https://www.uniminute.in/cart/add", nil)
	req.Header.Set("Origin", "https://www.uniminute.in")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 || calls != 1 {
		t.Fatal("same-origin request rejected")
	}
}
func TestProductionCookiesAndSecurityHeaders(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "lw_at", Value: "fixture", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		w.WriteHeader(200)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "https://www.uniminute.in/", nil))
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie flags missing")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
}
