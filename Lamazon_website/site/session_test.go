package site

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lamazon/website/shop"
)

func TestLogoutDoesNotRefreshExpiredSession(t *testing.T) {
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/login/logout" {
			t.Error("logout must not refresh the old session")
		}
		calls++
		w.WriteHeader(204)
	}))
	defer api.Close()
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: shop.RefreshCookie, Value: "old-refresh"})
	rec := httptest.NewRecorder()
	New(api.URL).ServeHTTP(rec, req)
	if calls != 1 {
		t.Fatalf("logout should revoke once; made %d backend calls", calls)
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account" {
		t.Fatalf("logout = %d %v", rec.Code, rec.Header())
	}
	cleared := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			cleared[c.Name] = true
		}
	}
	if !cleared[shop.AccessCookie] || !cleared[shop.RefreshCookie] {
		t.Fatal("session cookies were not cleared")
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("logout can be cached")
	}
}

func TestSessionPagesDoNotPrerenderOrCacheGuestSnapshots(t *testing.T) {
	rec := httptest.NewRecorder()
	New("http://127.0.0.1:1").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/account", nil))
	if rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("account page can be cached")
	}
	if strings.Contains(rec.Body.String(), `type="speculationrules"`) {
		t.Fatal("account page can prerender stale guest pages")
	}
}

func TestLoginReplacesOldSessionWithoutRefreshingIt(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/login/password":
			w.Write([]byte(`{"token":"new-access","refreshToken":"new-refresh","expiresIn":3600}`))
		case "/api/me":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				t.Error("profile lookup did not use the new credentials")
			}
			w.Write([]byte(`{"email":"seller@example.com","ready":true}`))
		default:
			t.Errorf("unexpected backend call during sign-in: %s", r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer api.Close()
	req := httptest.NewRequest(http.MethodPost, "/login/password", strings.NewReader("email=seller%40example.com&password=example&next=%2Faccount"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: shop.RefreshCookie, Value: "old-refresh"})
	rec := httptest.NewRecorder()
	New(api.URL).ServeHTTP(rec, req)
	if rec.Header().Get("HX-Redirect") != "/account" {
		t.Fatalf("sign-in redirect = %q", rec.Header().Get("HX-Redirect"))
	}
	cookies := map[string]string{}
	for _, c := range rec.Result().Cookies() {
		cookies[c.Name] = c.Value
	}
	if cookies[shop.AccessCookie] != "new-access" || cookies[shop.RefreshCookie] != "new-refresh" || cookies["lw_sv"] == "" {
		t.Fatalf("new session was not saved: %v", cookies)
	}
}
