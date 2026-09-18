package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestAddressValidationAndOwnership(t *testing.T) {
	h := testAPI(t)
	for _, bad := range []map[string]any{
		{"line": "Room 1", "city": "LPU", "phone": "abcdefghijabc12"},
		{"line": "Room 1", "city": "LPU", "name": strings.Repeat("a", 101)},
		{"line": "Room 1", "city": "LPU", "name": "<script>alert(1)</script>"},
		{"line": strings.Repeat("a", 301), "city": "LPU"},
		{"line": "Room 1", "city": "LPU", "pincode": "abc123"},
	} {
		if code, _ := call(t, h, "POST", "/api/addresses", bad); code != 400 {
			t.Fatalf("invalid address accepted: %d", code)
		}
	}
	code, address := call(t, h, "POST", "/api/addresses", map[string]any{"line": "Room 1", "city": "LPU", "phone": "+91 9876543210", "name": "Lalit"})
	if code != 201 || address["phone"] != "9876543210" {
		t.Fatalf("valid address: %d %v", code, address)
	}
	id := address["id"].(string)
	stranger := signIn(t, lastTestDB, "stranger@example.com")
	for _, path := range []string{"/api/addresses/" + id, "/api/addresses/" + id + "/default"} {
		code, _ = callAs(t, h, stranger, "PATCH", path, map[string]any{"line": "Other room", "city": "LPU"})
		if code != 404 {
			t.Fatalf("cross-account change: %d", code)
		}
	}
	code, address = call(t, h, "PATCH", "/api/addresses/"+id, map[string]any{"line": "Room 2", "city": "LPU", "name": "Recipient", "phone": "9999999999"})
	if code != 200 || address["line"] != "Room 2" || address["isDefault"] != true {
		t.Fatalf("edit: %d %v", code, address)
	}
}

func TestConcurrentAddressDefaultsAndSelection(t *testing.T) {
	h := testAPI(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := call(t, h, "POST", "/api/addresses", map[string]any{"line": "Room", "city": "LPU", "isDefault": true})
			if code != 201 {
				t.Errorf("save %d", code)
			}
		}()
	}
	wg.Wait()
	rows := callList(t, h, "/api/addresses")
	defaults := 0
	for _, r := range rows {
		if r.(map[string]any)["isDefault"] == true {
			defaults++
		}
	}
	if len(rows) != 8 || defaults != 1 {
		t.Fatalf("rows=%d defaults=%d", len(rows), defaults)
	}
	id := rows[len(rows)-1].(map[string]any)["id"].(string)
	if code, _ := call(t, h, "PATCH", "/api/addresses/"+id+"/default", nil); code != 204 {
		t.Fatal(code)
	}
	if callList(t, h, "/api/addresses")[0].(map[string]any)["id"] != id {
		t.Fatal("default selection did not persist")
	}
	call(t, h, "DELETE", "/api/addresses/"+id, nil)
	if callList(t, h, "/api/addresses")[0].(map[string]any)["isDefault"] != true {
		t.Fatal("default not replaced after deletion")
	}
}

func TestPreferencesPersistAndSuppressNotifications(t *testing.T) {
	h := testAPI(t)
	code, prefs := call(t, h, "PATCH", "/api/preferences", map[string]bool{"orderUpdates": false, "emailOffers": true})
	if code != 200 || prefs["orderUpdates"] != false || prefs["push"] != true {
		t.Fatalf("preferences: %d %v", code, prefs)
	}
	stored, err := (&API{db: lastTestDB}).preferences(context.Background(), DefaultOwner)
	if err != nil || stored.OrderUpdates || !stored.EmailOffers {
		t.Fatalf("stored: %v %v", stored, err)
	}
	call(t, h, "PATCH", "/api/preferences", map[string]bool{"push": false})
	_, prefs = call(t, h, "GET", "/api/preferences", nil)
	if prefs["orderUpdates"] != false || prefs["push"] != false || prefs["emailOffers"] != true {
		t.Fatal("patch overwrote other preferences")
	}
	if code, _ := callAs(t, h, "", "GET", "/api/preferences", nil); code != http.StatusUnauthorized {
		t.Fatal("guest preferences exposed")
	}
}

func TestProductLimitsReturnValidationErrors(t *testing.T) {
	h := testAPI(t)
	openApprovedStore(t, h, map[string]any{"name": "Store", "location": "Block 1", "city": "LPU", "categories": []string{"Food"}})
	for _, bad := range []map[string]any{
		{"title": strings.Repeat("x", maxItemTitle+1), "price": 20, "stock": 1},
		{"title": "Bad", "price": 1000000, "stock": 1},
		{"title": "Bad", "price": 1.001, "stock": 1},
		{"title": "Bad", "price": 20, "stock": 1, "category": "NotARealCategory"},
		{"title": "Bad", "price": 20, "stock": 1, "mrp": 1000000},
	} {
		code, body := call(t, h, "POST", "/api/seller/items", bad)
		if code != 400 || strings.Contains(body["error"].(string), "SQLSTATE") {
			t.Fatalf("bad product: %d %v", code, body)
		}
	}
}

func TestOrderNotificationHonorsPreferences(t *testing.T) {
	h, sent, push := notifyAPI(t)
	openApprovedStore(t, h, map[string]any{"name": "Store", "location": "Block 1", "city": "LPU", "categories": []string{"Food"}})
	addRider(t, h, adminSignIn(t, h), "9876543210")
	somewhereToDeliver(t, h)
	_, item := call(t, h, "POST", "/api/seller/items", map[string]any{"title": "Food", "price": 20, "stock": 10})
	call(t, h, "POST", "/api/push/subscribe", map[string]string{"token": "device-token"})
	call(t, h, "PATCH", "/api/preferences", map[string]bool{"orderUpdates": false})
	mails, pushes := sent.count, push.count()
	if code, body := call(t, h, "POST", "/api/orders", map[string]any{"itemId": item["id"], "units": 1, "requestId": "test-basket-123456", "expectedTotal": 35}); code != 201 {
		t.Fatalf("order %d %v", code, body)
	}
	if sent.count != mails || push.count() != pushes {
		t.Fatal("order opt-out was ignored")
	}
	call(t, h, "PATCH", "/api/preferences", map[string]bool{"orderUpdates": true, "push": false})
	call(t, h, "POST", "/api/orders", map[string]any{"itemId": item["id"], "units": 1, "requestId": "test-basket-123456", "expectedTotal": 35})
	if sent.count != mails+2 || push.count() != pushes {
		t.Fatal("push opt-out should still allow order email")
	}
}

func TestCancellationBeforeAcceptanceAndOwnership(t *testing.T) {
	h := testAPI(t)
	addRider(t, h, adminSignIn(t, h), "9876543210")
	openApprovedStore(t, h, map[string]any{"name": "Store", "location": "Block 1", "city": "LPU", "categories": []string{"Food"}})
	somewhereToDeliver(t, h)
	_, item := call(t, h, "POST", "/api/seller/items", map[string]any{"title": "Food", "price": 20, "stock": 1})
	_, order := call(t, h, "POST", "/api/orders", map[string]any{"itemId": item["id"], "units": 1, "requestId": "test-basket-123456", "expectedTotal": 35})
	id := order["id"].(string)
	stranger := signIn(t, lastTestDB, "stranger@example.com")
	if code, _ := callAs(t, h, stranger, "POST", "/api/orders/"+id+"/cancel", nil); code != 404 {
		t.Fatalf("cross-buyer cancellation: %d", code)
	}
	if code, body := call(t, h, "POST", "/api/orders/"+id+"/cancel", nil); code != 200 || body["rejectReason"] != "Cancelled by customer" {
		t.Fatalf("cancel: %d %v", code, body)
	}
	if code, _ := call(t, h, "POST", "/api/orders/"+id+"/cancel", nil); code != 409 {
		t.Fatal("duplicate cancellation allowed")
	}
	code, next := call(t, h, "POST", "/api/orders", map[string]any{"itemId": item["id"], "units": 1, "requestId": "test-basket-123456", "expectedTotal": 35})
	if code != 201 {
		t.Fatal("cancellation did not release reservation")
	}
	nextID := next["id"].(string)
	call(t, h, "POST", "/api/seller/orders/"+nextID+"/accept", nil)
	if code, _ := call(t, h, "POST", "/api/orders/"+nextID+"/cancel", nil); code != 409 {
		t.Fatal("accepted order cancelled")
	}
}

func TestCheckoutUnavailableWithoutRiders(t *testing.T) {
	h := testAPI(t)
	code, _ := call(t, h, "POST", "/api/orders/checkout", map[string]any{"lines": []map[string]any{{"itemId": "any", "units": 1}}, "requestId": "test-basket-123456", "expectedTotal": 35})
	if code != 503 {
		t.Fatalf("no rider: %d", code)
	}
	for _, path := range []string{"/api/orders", "/api/seller/orders"} {
		if code, _ := call(t, h, "POST", path, map[string]any{"itemId": "any", "units": 1, "expectedTotal": 35}); code != 503 {
			t.Fatalf("legacy route bypassed availability: %s %d", path, code)
		}
	}
}

func TestPolicyDraftsStayPrivateAndCannotBePublished(t *testing.T) {
	h := testAPI(t)
	if err := lastTestDB.seedPolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	admin := adminSignIn(t, h)
	for _, row := range callList(t, h, "/api/policies") {
		if policyHasBlanks(row.(map[string]any)["body"].(string)) {
			t.Fatal("public endpoint exposed placeholders")
		}
	}
	if code, body := callAs(t, h, admin, "PUT", "/api/admin/policies/contact", map[string]string{"title": "Contact", "body": "Write to [support@email.com]"}); code != 400 {
		t.Fatalf("placeholder published: %d %v", code, body)
	}
	if policyHasBlanks("See [the contact page](https://example.com/contact)") {
		t.Fatal("markdown links are not placeholders")
	}
	if code, _ := callAs(t, h, "", "GET", "/api/admin/policies", nil); code != 401 {
		t.Fatal("drafts accessible without admin")
	}
	code, body := callAs(t, h, admin, "GET", "/api/admin/policies", nil)
	if code != 200 || len(body["policies"].([]any)) != 5 {
		t.Fatalf("admin cannot edit drafts: %d %v", code, body)
	}
}

// Every linked page has to be a page. All five used to serve "This policy is
// not published yet" because the shipped drafts opened with a bracketed date,
// and one blank unpublishes the whole document.
func TestEveryShippedPolicyIsPublished(t *testing.T) {
	h := testAPI(t)
	if err := lastTestDB.seedPolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows := callList(t, h, "/api/policies")
	if len(rows) != 5 {
		t.Fatalf("expected five policies, got %d", len(rows))
	}
	for _, raw := range rows {
		row := raw.(map[string]any)
		slug, _ := row["slug"].(string)
		if row["published"] != true {
			t.Fatalf("%s is not published: %v", slug, row["body"])
		}
		body, _ := row["body"].(string)
		if len(body) < 400 || strings.Contains(body, "not published yet") {
			t.Fatalf("%s serves a stub rather than a document: %q", slug, body)
		}
	}
}

// A rewrite of the shipped text has to reach a database that was seeded before
// it. ON CONFLICT DO NOTHING meant it never could — the pages that shipped
// broken stayed broken on every existing deployment.
func TestSeedReplacesTheUntouchedDraftButNotAnEdit(t *testing.T) {
	h := testAPI(t)
	ctx := context.Background()

	// policies is not in the per-test TRUNCATE — it is seeded state, not
	// per-test data — so this test has to put back what it scribbles on.
	t.Cleanup(func() {
		if _, err := lastTestDB.sql.Exec(`DELETE FROM policies`); err != nil {
			t.Fatal(err)
		}
		if err := lastTestDB.seedPolicies(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	// One row left exactly as the old shipped draft, one an admin has written.
	if _, err := lastTestDB.sql.ExecContext(ctx, `
		INSERT INTO policies (slug, title, body) VALUES
		  ('terms','Terms and Conditions','Last updated: [Date]\n\nold draft'),
		  ('contact','Contact Us','Our own words, written by hand.')
		ON CONFLICT (slug) DO UPDATE SET body = EXCLUDED.body`); err != nil {
		t.Fatal(err)
	}
	if err := lastTestDB.seedPolicies(ctx); err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, raw := range callList(t, h, "/api/policies") {
		row := raw.(map[string]any)
		got[row["slug"].(string)] = row["body"].(string)
	}
	if strings.Contains(got["terms"], "old draft") {
		t.Fatal("the untouched draft was left in place")
	}
	if got["contact"] != "Our own words, written by hand." {
		t.Fatalf("an admin's own text was overwritten: %q", got["contact"])
	}
}

func TestPhotoRequiredForNewProduct(t *testing.T) {
	h := testAPI(t)
	openApprovedStore(t, h, map[string]any{"name": "Store", "location": "Block 1", "city": "LPU", "categories": []string{"Food"}})
	code, body := callAs(t, h, testToken, "POST", "/api/seller/items", map[string]any{"title": "No photo", "price": 20, "stock": 1, "category": "Food"})
	if code != 400 || body["error"] != "attach at least one product photo" {
		t.Fatalf("photo-less listing accepted: %d %v", code, body)
	}
	code, body = call(t, h, "POST", "/api/seller/items", map[string]any{"title": "With photo", "price": 20, "stock": 1, "category": "Food"})
	if code != 201 || len(body["imageUrls"].([]any)) != 1 {
		t.Fatalf("photo upload failed: %d %v", code, body)
	}
}

func TestProductEditRetainsCategoryWhenOmitted(t *testing.T) {
	h := testAPI(t)
	openApprovedStore(t, h, map[string]any{"name": "S", "location": "L", "city": "LPU", "categories": []string{"Food"}})
	_, item := call(t, h, "POST", "/api/seller/items", map[string]any{"title": "Burger", "price": 69, "stock": 10})
	code, edited := call(t, h, "PATCH", "/api/seller/items/"+item["id"].(string), map[string]any{"title": "Fresh Burger", "price": 70, "stock": 10})
	if code != 200 || edited["category"] != "Food" {
		t.Fatalf("edit lost category: %d %v", code, edited)
	}
}
