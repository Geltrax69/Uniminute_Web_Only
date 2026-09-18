package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// mailAPI is the handler with a stubbed Resend, plus the address it captured.
func mailAPI(t *testing.T) (http.Handler, *sentMail) {
	t.Helper()
	sent := &sentMail{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("resend auth header: %q", got)
		}
		var body struct {
			From    string   `json:"from"`
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		sent.from, sent.to, sent.text = body.From, body.To[0], body.Text
		sent.count++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"stub"}`))
	}))
	t.Cleanup(srv.Close)

	return routes(&API{db: testDB(t), cloud: fixtureCloud(t), mail: &Mailer{
		key: "test-key", from: "auth@simpedu.in", http: srv.Client(), base: srv.URL,
	}}), sent
}

// testDBOf is the database the current test's handler is using, so a test can
// inspect or age what the handlers wrote.
func testDBOf(t *testing.T) *DB {
	t.Helper()
	if lastTestDB == nil {
		t.Fatal("no database")
	}
	return lastTestDB
}

type sentMail struct {
	from, to, text string
	count          int
}

// codeFor takes the code out of the email, which is the only place it exists
// in the clear — Postgres stores nothing but its sha256.
var sixDigitPattern = regexp.MustCompile(`\b\d{6}\b`)

func codeFor(t *testing.T, sent *sentMail) string {
	t.Helper()
	got := sixDigitPattern.FindString(sent.text)
	if got == "" {
		t.Fatalf("no code in the email body: %q", sent.text)
	}
	// And what was mailed must be what Postgres will accept.
	var stored string
	if err := testDBOf(t).sql.QueryRow(
		`SELECT code_hash FROM login_codes WHERE email=$1`, sent.to).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if hashCode(got) != stored {
		t.Fatal("the mailed code does not match the stored hash")
	}
	return got
}

func TestEmailCodeSignIn(t *testing.T) {
	h, sent := mailAPI(t)
	const email = "lalit@lpu.in"

	code, body := call(t, h, http.MethodPost, "/api/login", map[string]string{"email": email})
	if code != http.StatusOK {
		t.Fatalf("request code: want 200, got %d (%v)", code, body["error"])
	}
	if sent.count != 1 || sent.to != email || sent.from != "auth@simpedu.in" {
		t.Fatalf("mail not sent right: %+v", sent)
	}

	// The response must not leak the code; only the inbox has it.
	if raw, _ := json.Marshal(body); len(raw) > 0 && body["code"] != nil {
		t.Fatal("the API answered with the code in it")
	}

	secret := codeFor(t, sent)
	if len(secret) != 6 {
		t.Fatalf("code should be 6 digits, got %q", secret)
	}
	// The emailed text is where the user actually reads it.
	if want := "code is " + secret; !strings.Contains(sent.text, want) {
		t.Fatalf("email body missing the code: %q", sent.text)
	}

	// A wrong code is refused and says how many tries are left.
	code, body = call(t, h, http.MethodPost, "/api/login/verify",
		map[string]string{"email": email, "code": "000000"})
	if code != http.StatusUnauthorized && secret != "000000" {
		t.Fatalf("wrong code: want 401, got %d", code)
	}

	code, body = call(t, h, http.MethodPost, "/api/login/verify",
		map[string]string{"email": email, "code": secret})
	if code != http.StatusOK {
		t.Fatalf("right code: want 200, got %d (%v)", code, body["error"])
	}
	token := body["token"].(string)
	if token == "" {
		t.Fatal("no session token issued")
	}

	// Single use: the same code cannot be replayed.
	if code, _ := call(t, h, http.MethodPost, "/api/login/verify",
		map[string]string{"email": email, "code": secret}); code != http.StatusUnauthorized {
		t.Fatalf("replayed code: want 401, got %d", code)
	}

	// And the token identifies the seller without an X-User-Email header.
	req := httptest.NewRequest(http.MethodGet, "/api/seller/store", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound { // no store yet, but it looked one up for this email
		t.Fatalf("token lookup: want 404 for a fresh account, got %d", rec.Code)
	}
}

func TestCodeAttemptsAreLimited(t *testing.T) {
	h, sent := mailAPI(t)
	const email = "brute@lpu.in"
	call(t, h, http.MethodPost, "/api/login", map[string]string{"email": email})
	secret := codeFor(t, sent)

	for i := 0; i < maxAttempts; i++ {
		call(t, h, http.MethodPost, "/api/login/verify",
			map[string]string{"email": email, "code": "999999"})
	}
	// Even the right code is refused once the tries are spent.
	code, _ := call(t, h, http.MethodPost, "/api/login/verify",
		map[string]string{"email": email, "code": secret})
	if code != http.StatusUnauthorized {
		t.Fatalf("after %d wrong tries: want 401, got %d", maxAttempts, code)
	}
}

func TestCodeResendIsRateLimited(t *testing.T) {
	h, sent := mailAPI(t)
	const email = "flood@lpu.in"
	call(t, h, http.MethodPost, "/api/login", map[string]string{"email": email})
	code, _ := call(t, h, http.MethodPost, "/api/login", map[string]string{"email": email})
	if code != http.StatusTooManyRequests {
		t.Fatalf("immediate resend: want 429, got %d", code)
	}
	if sent.count != 1 {
		t.Fatalf("rate limit should stop the second mail, sent %d", sent.count)
	}
}

// Seller routes are private now; the app has to sign in first.
func TestSellerRoutesNeedAToken(t *testing.T) {
	h := testAPI(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/seller/store"},
		{http.MethodPost, "/api/seller/store"},
		{http.MethodGet, "/api/seller/items"},
		{http.MethodPost, "/api/seller/items"},
		{http.MethodDelete, "/api/seller/items/item-1"},
		{http.MethodGet, "/api/seller/orders"},
	} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: want 401, got %d",
				route.method, route.path, rec.Code)
		}
	}
	// A forged token is no better than none.
	req := httptest.NewRequest(http.MethodGet, "/api/seller/items", nil)
	req.Header.Set("Authorization", "Bearer made-up")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged token: want 401, got %d", rec.Code)
	}
	// The catalog stays public.
	if code, _ := call(t, h, http.MethodGet, "/api/products", nil); code != http.StatusOK {
		t.Fatalf("catalog should not need a token, got %d", code)
	}
}

func TestRefreshRotatesAndOldOneDies(t *testing.T) {
	h, sent := mailAPI(t)
	const email = "rotate@lpu.in"
	call(t, h, http.MethodPost, "/api/login", map[string]string{"email": email})
	_, body := call(t, h, http.MethodPost, "/api/login/verify",
		map[string]string{"email": email, "code": codeFor(t, sent)})
	first := body["refreshToken"].(string)
	if body["expiresIn"].(float64) != accessLifetime.Seconds() {
		t.Fatalf("expiresIn should tell the app when to refresh, got %v", body["expiresIn"])
	}

	code, refreshed := call(t, h, http.MethodPost, "/api/login/refresh",
		map[string]string{"refreshToken": first})
	if code != http.StatusOK {
		t.Fatalf("refresh: want 200, got %d (%v)", code, refreshed["error"])
	}
	second := refreshed["refreshToken"].(string)
	if second == first {
		t.Fatal("refresh token must rotate, not repeat")
	}
	// Requests racing on the same token inside the grace minute all get a pair.
	if code, _ := call(t, h, http.MethodPost, "/api/login/refresh",
		map[string]string{"refreshToken": first}); code != http.StatusOK {
		t.Fatalf("refresh reused within a minute: want 200, got %d", code)
	}
	// After it, a spent token is dead, so a stolen one is short-lived.
	if _, err := testDBOf(t).sql.Exec(`UPDATE auth_sessions SET rotated_at = now() - interval '2 minutes' WHERE refresh_hash = $1`, hashCode(first)); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, h, http.MethodPost, "/api/login/refresh",
		map[string]string{"refreshToken": first}); code != http.StatusUnauthorized {
		t.Fatalf("reused refresh token after the grace minute: want 401, got %d", code)
	}
	// The new access token works on a private route.
	req := httptest.NewRequest(http.MethodGet, "/api/seller/items", nil)
	req.Header.Set("Authorization", "Bearer "+refreshed["token"].(string))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refreshed token on a private route: want 200, got %d", rec.Code)
	}
}

// An expired access token is refused, and refreshing revives the session.
func TestExpiredAccessTokenIsRefused(t *testing.T) {
	h := testAPI(t)
	db := testDBOf(t)
	s, err := db.newSession(context.Background(), "expiry@lpu.in")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(
		`UPDATE auth_sessions SET expires_at = now() - interval '1 second'
		 WHERE access_hash = $1`, hashCode(s.Token)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/seller/items", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired access token: want 401, got %d", rec.Code)
	}
	// The refresh token outlives it, which is the whole point of the pair.
	if code, _ := call(t, h, http.MethodPost, "/api/login/refresh",
		map[string]string{"refreshToken": s.RefreshToken}); code != http.StatusOK {
		t.Fatalf("refresh after access expiry: want 200, got %d", code)
	}
}
