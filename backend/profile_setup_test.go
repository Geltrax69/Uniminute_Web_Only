package main

import (
	"net/http"
	"testing"
)

func setupFixture(key string) map[string]any {
	return map[string]any{"name": "New Buyer", "phone": "9876543210", "avatar": "gym-female", "password": "fixture-password", "setupRequestId": key, "setupAddress": map[string]any{"line": "BH-9, Room 214", "label": "Home", "city": "LPU"}}
}
func TestProfileCardSetupPersistsOnce(t *testing.T) {
	h := testAPI(t)
	in := setupFixture("profile-save-fixture-1234")
	for range 2 {
		status, user := call(t, h, http.MethodPatch, "/api/me", in)
		if status != 200 || user["avatar"] != "gym-female" || user["interest"] != "gym" || user["ready"] != true || user["hasPassword"] != true {
			t.Fatalf("setup save: %d %v", status, user)
		}
	}
	var count int
	if err := lastTestDB.sql.QueryRow(`SELECT count(*) FROM addresses WHERE email=$1`, DefaultOwner).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate address: %d %v", count, err)
	}
	if _, user := call(t, h, "GET", "/api/me", nil); user["avatar"] != "gym-female" {
		t.Fatal("avatar was not retained across reads")
	}
	// Owning a valid setup key never grants another account's saved identity.
	foreign := signIn(t, lastTestDB, "other-avatar@example.com")
	old := testToken
	testToken = foreign
	status, user := call(t, h, "PATCH", "/api/me", map[string]any{"avatar": "coffee-lover"})
	testToken = old
	if status != 200 || user["avatar"] != "coffee-lover" {
		t.Fatal("other account avatar failed")
	}
	if _, user = call(t, h, "GET", "/api/me", nil); user["avatar"] != "gym-female" {
		t.Fatal("another account changed the buyer avatar")
	}
}
func TestProfileCardSetupRejectsUnsafeAvatarAndInvalidAddress(t *testing.T) {
	h := testAPI(t)
	for _, avatar := range []string{"../../secret", "https://example.com/avatar.png", "invented-avatar"} {
		if status, _ := call(t, h, "PATCH", "/api/me", map[string]any{"avatar": avatar}); status != 400 {
			t.Fatal("unsafe avatar accepted")
		}
	}
	in := setupFixture("invalid-profile-fixture")
	in["setupAddress"] = map[string]any{"line": "", "city": "LPU"}
	if status, _ := call(t, h, "PATCH", "/api/me", in); status != 400 {
		t.Fatalf("invalid address: %d", status)
	}
	if _, user := call(t, h, "GET", "/api/me", nil); user["avatar"] != "" || user["name"] != "" || user["hasPassword"] == true {
		t.Fatal("invalid setup partially saved")
	}
}
func TestProfileCardSetupRollsBackIfAddressWriteFails(t *testing.T) {
	h := testAPI(t)
	db := lastTestDB
	if _, err := db.sql.Exec(`ALTER TABLE addresses ADD CONSTRAINT profile_setup_rollback_fixture CHECK(line<>'BH-9, Room 214')`); err != nil {
		t.Fatal(err)
	}
	defer db.sql.Exec(`ALTER TABLE addresses DROP CONSTRAINT profile_setup_rollback_fixture`)
	if status, _ := call(t, h, "PATCH", "/api/me", setupFixture("rollback-profile-fixture")); status != 503 {
		t.Fatalf("expected write failure, got %d", status)
	}
	if _, user := call(t, h, "GET", "/api/me", nil); user["avatar"] != "" || user["name"] != "" || user["hasPassword"] == true || user["ready"] == true {
		t.Fatalf("failed address write saved part of profile: %v", user)
	}
}
