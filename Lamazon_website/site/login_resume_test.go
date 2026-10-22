package site

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLoginCodeResendStates(t *testing.T) {
	for _, tc := range []struct {
		name, from, mode, body string
		status                 int
		retry                  string
		wantCode               bool
	}{
		{"new signup", "email", "", `{"email":"new@example.com","resendAfter":30}`, 200, "", true},
		{"existing code option", "password", "code", `{"email":"new@example.com","hasPassword":true,"resendAfter":30}`, 200, "", true},
		{"code cooldown", "email", "", `{"error":"Please wait","codePending":true}`, 429, "19", true},
		{"general rate limit", "email", "", `{"error":"Too many requests"}`, 429, "600", false},
		{"delivery failure", "email", "", `{"error":"Could not send your email"}`, 502, "", false},
		{"reset wrong-purpose cooldown", "password", "reset", `{"error":"Wait before requesting another code","codePending":false}`, 429, "17", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.retry != "" {
					w.Header().Set("Retry-After", tc.retry)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer api.Close()
			form := url.Values{"email": {"new@example.com"}, "from": {tc.from}, "mode": {tc.mode}}
			req := httptest.NewRequest("POST", "/login/start", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			New(api.URL).ServeHTTP(rec, req)
			html := rec.Body.String()
			code := strings.Contains(html, `hx-post="/login/verify"`)
			if code != tc.wantCode {
				t.Fatalf("code form=%v expected %v", code, tc.wantCode)
			}
			if tc.wantCode && !strings.Contains(html, "Resend code") {
				t.Fatal("resend missing")
			}
			if tc.name == "existing code option" && !strings.Contains(html, "Use password instead") {
				t.Fatal("existing password option lost")
			}
			if tc.name == "new signup" && strings.Contains(html, "Use password instead") {
				t.Fatal("new account offered nonexistent password")
			}
		})
	}
}

func TestVerificationErrorKeepsResendDeadline(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":"Wrong code"}`)
	}))
	defer api.Close()
	deadline := time.Now().Add(12 * time.Second).UnixMilli()
	form := url.Values{"email": {"new@example.com"}, "code": {"123456"}, "resendAt": {fmt.Sprint(deadline)}}
	req := httptest.NewRequest("POST", "/login/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	New(api.URL).ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), fmt.Sprint(deadline)) {
		t.Fatal("wrong code reset resend countdown")
	}
}

func TestVerifiedIncompleteAccountReturnsToSetup(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/login/verify":
			fmt.Fprint(w, `{"token":"fresh-token","refreshToken":"fresh-refresh","expiresIn":3600}`)
		case "/api/me":
			if r.Header.Get("Authorization") != "Bearer fresh-token" {
				t.Error("setup lookup used wrong session")
			}
			fmt.Fprint(w, `{"ready":false,"hasPassword":false}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	req := httptest.NewRequest("POST", "/login/verify", strings.NewReader("email=new%40example.com&code=123456&next=%2Fcart"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	New(api.URL).ServeHTTP(rec, req)
	if rec.Header().Get("HX-Redirect") != "/profile/setup?next=%2Fcart" {
		t.Fatalf("redirect=%s", rec.Header().Get("HX-Redirect"))
	}
}
