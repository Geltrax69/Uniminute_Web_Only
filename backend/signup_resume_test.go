package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInterruptedSignupResumesWithoutPassword(t *testing.T) {
	h, sent := mailAPI(t)
	const email = "unfinished@example.com"
	if status, _ := call(t, h, "POST", "/api/login", map[string]any{"email": email}); status != 200 {
		t.Fatal(status)
	}
	first := codeFor(t, sent)
	status, session := call(t, h, "POST", "/api/login/verify", map[string]any{"email": email, "code": first})
	if status != 200 || session["token"] == nil {
		t.Fatal("first verification failed")
	}
	// Throw away the session, exactly as closing a guest browser does. No
	// setup request or password is ever submitted.
	status, out := call(t, h, "POST", "/api/login", map[string]any{"email": email})
	if status != 200 || out["needsPassword"] == true || sent.count != 2 || out["resendAfter"] != float64(30) {
		t.Fatalf("resume: %d %v mails=%d", status, out, sent.count)
	}
	status, session = call(t, h, "POST", "/api/login/verify", map[string]any{"email": email, "code": codeFor(t, sent)})
	if status != 200 {
		t.Fatal("new browser could not verify")
	}
	user := session["user"].(map[string]any)
	if user["ready"] == true || user["hasPassword"] == true {
		t.Fatalf("unfinished signup state: %v", user)
	}
	// Even a partially saved password must not trap an unfinished signup.
	if _, err := lastTestDB.sql.Exec(`UPDATE users SET pass_hash='fixture-partial' WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	status, out = call(t, h, "POST", "/api/login", map[string]any{"email": email})
	if status != 200 || out["needsPassword"] == true || sent.count != 3 {
		t.Fatalf("partial password signup: %d %v", status, out)
	}
}

func TestResendCooldownAndLatestCode(t *testing.T) {
	h, sent := mailAPI(t)
	const email = "resend@example.com"
	call(t, h, "POST", "/api/login", map[string]any{"email": email})
	original := codeFor(t, sent)
	status, out := call(t, h, "POST", "/api/login", map[string]any{"email": email, "code": true})
	if status != 429 || out["codePending"] != true || out["retryAfter"].(float64) > 30 || sent.count != 1 {
		t.Fatalf("cooldown: %d %v", status, out)
	}
	// Do not show a reset-code form when only a login code exists.
	status, out = call(t, h, "POST", "/api/login/forgot", map[string]any{"email": email})
	if status != 429 || out["codePending"] != false {
		t.Fatalf("wrong-purpose cooldown: %d %v", status, out)
	}
	if _, err := lastTestDB.sql.Exec(`UPDATE login_codes SET sent_at=now()-interval '31 seconds' WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	status, _ = call(t, h, "POST", "/api/login", map[string]any{"email": email, "code": true})
	if status != 200 || sent.count != 2 {
		t.Fatalf("resend after 30 seconds: %d", status)
	}
	latest := codeFor(t, sent)
	if latest != original {
		if status, _ = call(t, h, "POST", "/api/login/verify", map[string]any{"email": email, "code": original}); status != 401 {
			t.Fatal("old code still accepted")
		}
	}
	if status, _ = call(t, h, "POST", "/api/login/verify", map[string]any{"email": email, "code": latest}); status != 200 {
		t.Fatal("latest code rejected")
	}
}

func TestFailedMailCanBeRetriedWithoutPhantomCooldown(t *testing.T) {
	db := testDB(t)
	failed := true
	deliveries := 0
	mail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveries++
		if failed {
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"private provider details"}`))
			return
		}
		w.Write([]byte(`{"id":"fixture"}`))
	}))
	defer mail.Close()
	h := routes(&API{db: db, mail: &Mailer{key: "fixture", from: "auth@example.com", http: mail.Client(), base: mail.URL}})
	const email = "delivery@example.com"
	status, out := call(t, h, "POST", "/api/login", map[string]any{"email": email})
	raw, _ := json.Marshal(out)
	if status != 502 || strings.Contains(string(raw), "private provider details") {
		t.Fatalf("delivery failure: %d %v", status, out)
	}
	var rows int
	if err := db.sql.QueryRow(`SELECT count(*) FROM login_codes WHERE email=$1`, email).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("failed delivery left a code/cooldown")
	}
	failed = false
	status, _ = call(t, h, "POST", "/api/login", map[string]any{"email": email})
	if status != 200 || deliveries != 2 {
		t.Fatal("delivery retry blocked")
	}
	h = routes(&API{db: db})
	status, _ = call(t, h, "POST", "/api/login", map[string]any{"email": "unconfigured@example.com"})
	if status != 503 {
		t.Fatal("unconfigured mail falsely reported success")
	}
}
