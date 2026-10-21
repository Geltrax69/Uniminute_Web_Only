package main

import (
	"context"
	"net/http"
	"testing"
)

// Signing in is what creates the person, and the id they get never changes.
func TestSignInCreatesUserWithStableID(t *testing.T) {
	h, sent := mailAPI(t)
	const email = "buyer@lpu.in"

	call(t, h, http.MethodPost, "/api/login", map[string]string{"email": email})
	_, body := call(t, h, http.MethodPost, "/api/login/verify",
		map[string]string{"email": email, "code": codeFor(t, sent)})

	user := body["user"].(map[string]any)
	id := user["id"].(string)
	if id == "" {
		t.Fatal("no public id issued")
	}
	if roles := user["roles"].([]any); len(roles) != 1 || roles[0] != "buyer" {
		t.Fatalf("a fresh account is a buyer, got %v", roles)
	}

	// Signing in again is the same person, not a new one.
	testDBOf(t).sql.Exec(`DELETE FROM login_codes`)
	call(t, h, http.MethodPost, "/api/login", map[string]string{"email": email})
	_, again := call(t, h, http.MethodPost, "/api/login/verify",
		map[string]string{"email": email, "code": codeFor(t, sent)})
	if got := again["user"].(map[string]any)["id"]; got != id {
		t.Fatalf("id changed between sign-ins: %v then %v", id, got)
	}
}

// Opening a store makes someone a seller without anything being written to
// say so — the role is derived, so it cannot drift.
func TestOpeningAStoreAddsTheSellerRole(t *testing.T) {
	h := testAPI(t)

	_, before := call(t, h, http.MethodGet, "/api/me", nil)
	if roles := before["roles"].([]any); len(roles) != 1 {
		t.Fatalf("want buyer only, got %v", roles)
	}

	call(t, h, http.MethodPost, "/api/seller/store", map[string]any{
		"name": "Lalit Snacks", "location": "Block 32", "city": "LPU",
		"categories": []string{"Food"},
	})

	_, after := call(t, h, http.MethodGet, "/api/me", nil)
	roles := after["roles"].([]any)
	if len(roles) != 2 || roles[0] != "buyer" || roles[1] != "seller" {
		t.Fatalf("after opening a store: want buyer+seller, got %v", roles)
	}
	if after["hasStore"] != true {
		t.Fatal("hasStore should be true")
	}
}

// The address book belongs to the person, so it is there on the next device.
func TestAddressBookPersistsAndKeepsOneDefault(t *testing.T) {
	h := testAPI(t)

	code, first := call(t, h, http.MethodPost, "/api/addresses", map[string]any{
		"label": "Home", "line": "Block 32, Room 214", "city": "LPU",
		"pincode": "144411", "name": "Lalit Singh", "phone": "9876543210",
	})
	if code != http.StatusCreated {
		t.Fatalf("save address: want 201, got %d (%v)", code, first["error"])
	}
	if first["isDefault"] != true {
		t.Fatal("the first address saved should become the default")
	}

	// Saving an address is where we learn the name and number.
	_, me := call(t, h, http.MethodGet, "/api/me", nil)
	if me["name"] != "Lalit Singh" || me["phone"] != "9876543210" {
		t.Fatalf("profile did not pick up name/phone: %v", me)
	}

	call(t, h, http.MethodPost, "/api/addresses", map[string]any{
		"label": "Other", "line": "Block 1", "city": "LPU", "isDefault": true,
	})
	list := callList(t, h, "/api/addresses")
	if len(list) != 2 {
		t.Fatalf("want 2 addresses, got %d", len(list))
	}
	// Exactly one default, and the promoted one is first.
	defaults := 0
	for _, a := range list {
		if a.(map[string]any)["isDefault"] == true {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("want exactly one default, got %d", defaults)
	}

	// An unserviceable city is refused rather than saved and undeliverable.
	if code, _ := call(t, h, http.MethodPost, "/api/addresses", map[string]any{
		"line": "5 MG Road", "city": "Mumbai",
	}); code != http.StatusBadRequest {
		t.Fatalf("address outside the area: want 400, got %d", code)
	}
}

// A seller's stock is what shoppers browse; listing it anywhere else would
// mean a seller can add stock nobody can buy.
func TestSellerStockAppearsInTheCatalog(t *testing.T) {
	h := testAPI(t)
	openApprovedStore(t, h, map[string]any{
		"name": "Campus Snacks", "location": "Block 32", "city": "LPU",
		"categories": []string{"Food"},
	})
	call(t, h, http.MethodPost, "/api/seller/items", map[string]any{
		"title": "Cold Coffee 300ml", "category": "Food", "price": 60, "stock": 10,
	})
	// Sold out is left out rather than shown as unavailable.
	call(t, h, http.MethodPost, "/api/seller/items", map[string]any{
		"title": "Sold Out Samosa", "category": "Food", "price": 20, "stock": 0,
	})

	food := callList(t, h, "/api/products?tab=Food")
	var found, soldOut bool
	for _, p := range food {
		switch p.(map[string]any)["name"] {
		case "Cold Coffee 300ml":
			found = true
			if p.(map[string]any)["store"] != "Campus Snacks" {
				t.Fatalf("wrong store: %v", p)
			}
		case "Sold Out Samosa":
			soldOut = true
		}
	}
	if !found {
		t.Fatal("a seller's in-stock item never reached the Food tab")
	}
	if soldOut {
		t.Fatal("a sold-out item should not be listed")
	}

	// And the store itself shows up beside the seeded ones.
	shops := callList(t, h, "/api/shops")
	for _, s := range shops {
		if s.(map[string]any)["name"] == "Campus Snacks" {
			return
		}
	}
	t.Fatal("the seller's store is missing from /api/shops")
}

// The seeded shopper must be able to sign in with the password, and rotating
// it must not cost them their profile.
func TestSeedUserSetsAPasswordAndKeepsTheProfile(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	t.Setenv("SEED_USER", "Seed@Example.com") // mixed case on purpose
	t.Setenv("SEED_USER_PASSWORD", "first-pass")
	if err := db.seedUser(ctx); err != nil {
		t.Fatal(err)
	}

	// Addresses are stored lowercase, so a seed typed with capitals must not
	// create a second account nobody can sign in to.
	var hash, name string
	if err := db.sql.QueryRowContext(ctx,
		`SELECT pass_hash, name FROM users WHERE email = 'seed@example.com'`).
		Scan(&hash, &name); err != nil {
		t.Fatalf("seeded user not found: %v", err)
	}
	if !passwordMatches(hash, "first-pass") {
		t.Error("the seeded password does not verify")
	}

	// They fill in their profile; later bootstrap changes must not reset it.
	if _, err := db.sql.ExecContext(ctx,
		`UPDATE users SET name = 'Lalit' WHERE email = 'seed@example.com'`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEED_USER_PASSWORD", "second-pass")
	if err := db.seedUser(ctx); err != nil {
		t.Fatal(err)
	}

	if err := db.sql.QueryRowContext(ctx,
		`SELECT pass_hash, name FROM users WHERE email = 'seed@example.com'`).
		Scan(&hash, &name); err != nil {
		t.Fatal(err)
	}
	if !passwordMatches(hash, "first-pass") {
		t.Error("restart overwrote the existing password")
	}
	if passwordMatches(hash, "second-pass") {
		t.Error("bootstrap reset an existing credential")
	}
	if name != "Lalit" {
		t.Errorf("the seed wiped the profile: name = %q", name)
	}
}

// Unset means "leave the database alone", not "create a blank account".
func TestSeedUserWithoutEnvDoesNothing(t *testing.T) {
	db := testDB(t)
	t.Setenv("SEED_USER", "")
	t.Setenv("SEED_USER_PASSWORD", "")
	if err := db.seedUser(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	db.sql.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	if n != 0 {
		t.Errorf("want no users, got %d", n)
	}
}
