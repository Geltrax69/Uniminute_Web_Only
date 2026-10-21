package main

import (
	"bytes"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTOTPVectorAndEncryptedStorage(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	if totpCode(secret, 1) != "287082" {
		t.Fatal("TOTP RFC vector failed")
	}
	t.Setenv("ADMIN_MFA_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	encrypted, err := sealMFA("boss", secret)
	if err != nil || strings.Contains(encrypted, secret) {
		t.Fatalf("MFA encryption failed: %v", err)
	}
	if got, err := openMFA("boss", encrypted); err != nil || got != secret {
		t.Fatal("MFA cannot be decrypted")
	}
	if _, err := openMFA("other-admin", encrypted); err == nil {
		t.Fatal("MFA secret not bound to admin")
	}
}
func TestAdminMFAEnrollmentReplayRecoveryAndAccessControl(t *testing.T) {
	t.Setenv("ADMIN_MFA_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	h := testAPI(t)
	admin := adminSignIn(t, h)
	if code, _ := callAs(t, h, testToken, "POST", "/api/admin/security/mfa", map[string]string{"action": "begin", "password": "Sup3r$ecret"}); code != 401 {
		t.Fatal("shopper enrolled admin MFA")
	}
	code, out := callAs(t, h, admin, "POST", "/api/admin/security/mfa", map[string]string{"action": "begin", "password": "Sup3r$ecret"})
	if code != 200 {
		t.Fatalf("begin: %d %v", code, out)
	}
	secret := out["secret"].(string)
	otp := totpCode(secret, time.Now().Unix()/30)
	code, out = callAs(t, h, admin, "POST", "/api/admin/security/mfa", map[string]string{"action": "confirm", "password": "Sup3r$ecret", "code": otp})
	if code != 200 {
		t.Fatalf("confirm: %d %v", code, out)
	}
	recovery := out["recoveryCodes"].([]any)[0].(string)
	for _, otp := range []string{"", otp} {
		if code, _ := callAs(t, h, "", "POST", "/api/admin/login", map[string]string{"username": "boss", "password": "Sup3r$ecret", "code": otp}); code != 401 {
			t.Errorf("missing/replayed OTP accepted: %d", code)
		}
	}
	code, out = callAs(t, h, "", "POST", "/api/admin/login", map[string]string{"username": "boss", "password": "Sup3r$ecret", "code": recovery})
	if code != 200 {
		t.Fatalf("recovery: %d %v", code, out)
	}
	if code, _ := callAs(t, h, "", "POST", "/api/admin/login", map[string]string{"username": "boss", "password": "Sup3r$ecret", "code": recovery}); code != 401 {
		t.Fatal("recovery code was reusable")
	}
	// Settings never return the secret or recovery code hashes.
	w := rawAs(h, admin, "GET", "/api/admin/security/mfa", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), recovery) {
		t.Fatalf("security status leaked a secret: %d", w.Code)
	}
	var status map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if status["enabled"] != true {
		t.Fatal("MFA was not enabled")
	}
	if w := rawAs(h, out["token"].(string), http.MethodPost, "/api/admin/logout", `{}`); w.Code != 204 {
		t.Fatalf("staff signout: %d", w.Code)
	}
}
