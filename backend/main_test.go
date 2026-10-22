package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

// testAPI is the whole handler over a clean database, without photo storage.
func testAPI(t *testing.T) http.Handler {
	t.Helper()
	return routes(&API{db: testDB(t), cloud: fixtureCloud(t)})
}

// testDB connects to the DATABASE_URL Postgres and hands back a clean slate.
// Skips rather than fails when no database is around, so `go test` still works
// on a machine without one.
func testDB(t *testing.T) *DB {
	t.Helper()
	dsn := testDSN(t)
	db, err := OpenDB(dsn)
	if err != nil {
		if os.Getenv("LAMAZON_TEST_URL") != "" {
			t.Fatalf("configured test database unavailable: %v", err)
		}
		t.Skipf("no Postgres at %s: %v", dsn, err)
	}
	t.Cleanup(func() { db.Close() })

	// Seller data is per-test; the catalog is shared read-only seed.
	if _, err := db.sql.Exec(
		`TRUNCATE security_audit_events, payment_request_limits, payment_gateway_attempts, razorpay_webhook_events, storefront_campaigns, storefront_seasons, checkout_attempts, orders, inventory_items, seller_stores, login_codes,
		  auth_sessions, push_subscriptions, addresses, users,
		  admins, riders, staff_sessions, password_attempts CASCADE`); err != nil {
		t.Fatal(err)
	}
	lastTestDB = db
	testToken = signIn(t, db, DefaultOwner)
	return db
}

// testDSN points at a database of its own, created on demand.
//
// These tests TRUNCATE. Pointed at the development database they would delete
// real people, their addresses and their stores — which is exactly what used
// to happen. LAMAZON_TEST_URL overrides for CI.
func testDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("LAMAZON_TEST_URL"); dsn != "" {
		return dsn
	}
	dev := os.Getenv("DATABASE_URL")
	if dev == "" {
		dev = "postgres://lamazon:lamazon@localhost:5433/lamazon?sslmode=disable"
	}
	u, err := url.Parse(dev)
	if err != nil {
		t.Skipf("DATABASE_URL is not a URL: %v", err)
	}
	name := strings.TrimPrefix(u.Path, "/")
	if strings.HasSuffix(name, "_test") {
		return dev // already a test database
	}
	u.Path = "/" + name + "_test"

	// CREATE DATABASE cannot run inside a transaction or against the database
	// being created, so it goes through the server's default one.
	admin := *u
	admin.Path = "/postgres"
	conn, err := sql.Open("pgx", admin.String())
	if err != nil {
		t.Skipf("no Postgres: %v", err)
	}
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		t.Skipf("no Postgres at %s: %v", admin.Host, err)
	}
	// Already-exists is the normal case after the first run.
	conn.Exec(`CREATE DATABASE ` + name + `_test`)
	return u.String()
}

// signIn mints a session straight through the database — the emailed-code
// path has its own tests, and every other test just needs to be somebody.
func signIn(t *testing.T, db *DB, email string) string {
	t.Helper()
	s, err := db.newSession(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	return s.Token
}

// testToken is the session the plain call() helper uses, and lastTestDB the
// database behind it — both set by testDB.
var (
	testToken  string
	lastTestDB *DB
)

// call runs one request against the API and returns status plus decoded body.
func call(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	if method == http.MethodPost && path == "/api/seller/items" {
		return callItemWithPhoto(t, h, body)
	}
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	return callAs(t, h, testToken, method, path, body)
}

// callAs is call() for someone who is not the signed-in shopper: an admin, a
// rider, or a second seller.
func callAs(t *testing.T, h http.Handler, token, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out) // arrays decode as nil, fine
	return rec.Code, out
}

// adminSignIn puts an admin in the table the way boot does, then signs in
// through the real endpoint — so the password path is exercised, not skipped.
func adminSignIn(t *testing.T, h http.Handler) string {
	t.Helper()
	hash, err := hashPassword("Sup3r$ecret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lastTestDB.sql.Exec(
		`INSERT INTO admins (username, pass_hash) VALUES ('boss', $1)
		 ON CONFLICT (username) DO UPDATE SET pass_hash = EXCLUDED.pass_hash`,
		hash); err != nil {
		t.Fatal(err)
	}
	code, body := callAs(t, h, "", http.MethodPost, "/api/admin/login",
		map[string]string{"username": "boss", "password": "Sup3r$ecret"})
	if code != http.StatusOK {
		t.Fatalf("admin login: want 200, got %d (%v)", code, body["error"])
	}
	return body["token"].(string)
}

// openApprovedStore is the state most seller tests want to start from: a
// store that exists and has been through review.
func openApprovedStore(t *testing.T, h http.Handler, fields map[string]any) {
	t.Helper()
	if code, body := call(t, h, http.MethodPost, "/api/seller/store", fields); code != http.StatusCreated {
		t.Fatalf("create store: want 201, got %d (%v)", code, body["error"])
	}
	approveStore(t, h, DefaultOwner)
}

// approveStore takes a store through review the way an admin would.
func approveStore(t *testing.T, h http.Handler, owner string) {
	t.Helper()
	admin := adminSignIn(t, h)
	if code, body := callAs(t, h, admin, http.MethodPost,
		"/api/admin/stores/"+owner+"/approve", nil); code != http.StatusOK {
		t.Fatalf("approve store: want 200, got %d (%v)", code, body["error"])
	}
}

// somewhereToDeliver saves the address every order needs.
func somewhereToDeliver(t *testing.T, h http.Handler) {
	t.Helper()
	call(t, h, http.MethodPost, "/api/addresses", map[string]any{
		"line": "Block 32, Room 214", "city": "LPU",
		"name": "Lalit Singh", "phone": "9876543210",
	})
}

func callList(t *testing.T, h http.Handler, path string) []any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out []any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v (%s)", path, err, rec.Body.String())
	}
	return out
}

// callListAs and callAs2 are callList and call for a staff token: one returns
// a bare array, the other an object.
func callListAs(t *testing.T, h http.Handler, token, path string) []any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out []any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v (%s)", path, err, rec.Body.String())
	}
	return out
}

func callAs2(t *testing.T, h http.Handler, token, path string) map[string]any {
	t.Helper()
	code, body := callAs(t, h, token, http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("%s: want 200, got %d (%v)", path, code, body["error"])
	}
	return body
}

func TestCatalogFilters(t *testing.T) {
	h := testAPI(t)

	all := callList(t, h, "/api/products")
	if len(all) == 0 {
		t.Fatal("catalog is empty")
	}
	if got := callList(t, h, "/api/products?tab=Grocery"); len(got) != 1 {
		t.Fatalf("tab filter: want 1 grocery product, got %d", len(got))
	}
	if got := callList(t, h, "/api/products?q=milk"); len(got) != 1 {
		t.Fatalf("search: want 1 match for milk, got %d", len(got))
	}
	// Filters stack rather than replacing each other.
	if got := callList(t, h, "/api/products?tab=Food&q=milk"); len(got) != 0 {
		t.Fatalf("stacked filters: want 0, got %d", len(got))
	}
	if code, _ := call(t, h, http.MethodGet, "/api/products/nope", nil); code != http.StatusNotFound {
		t.Fatalf("unknown product: want 404, got %d", code)
	}
}

func TestShopStocksOthersListings(t *testing.T) {
	h := testAPI(t)
	// Milk is listed by Nature Fresh; FreshMart stocks it cheaper.
	items := callList(t, h, "/api/shops/FreshMart/products")
	if len(items) != 1 {
		t.Fatalf("want 1 stocked item, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if item["price"].(float64) != 66 {
		t.Fatalf("want FreshMart price 66, got %v", item["price"])
	}
	if item["store"] != "FreshMart" {
		t.Fatalf("want store FreshMart, got %v", item["store"])
	}
}

func TestLocationOnlyServesCampus(t *testing.T) {
	h := testAPI(t)
	for _, city := range []string{"LPU", "  lpu, phagwara ", "Lovely Professional University"} {
		_, body := call(t, h, http.MethodGet,
			"/api/locations/check?city="+url.QueryEscape(city), nil)
		if body["serviceable"] != true {
			t.Fatalf("%q should be serviceable", city)
		}
	}
	for _, city := range []string{"Jalandhar", "Mumbai", ""} {
		_, body := call(t, h, http.MethodGet,
			"/api/locations/check?city="+url.QueryEscape(city), nil)
		if body["serviceable"] != false {
			t.Fatalf("%q should not be serviceable", city)
		}
	}
}

func TestLoginValidatesEmail(t *testing.T) {
	h, _ := mailAPI(t)
	if code, _ := call(t, h, http.MethodPost, "/api/login",
		map[string]string{"email": "lalit@lpu.in"}); code != http.StatusOK {
		t.Fatalf("valid email: want 200, got %d", code)
	}
	for _, bad := range []string{"lalit@example", "nope", "@example.com", ""} {
		if code, _ := call(t, h, http.MethodPost, "/api/login",
			map[string]string{"email": bad}); code != http.StatusBadRequest {
			t.Fatalf("%q: want 400, got %d", bad, code)
		}
	}
}

func TestSellerLifecycle(t *testing.T) {
	h := testAPI(t)

	// Stock cannot exist before a store does.
	if code, _ := call(t, h, http.MethodPost, "/api/seller/items",
		map[string]any{"title": "Cold Coffee", "price": 60, "stock": 10}); code != http.StatusConflict {
		t.Fatalf("item before store: want 409, got %d", code)
	}

	// A store outside the delivery area is refused.
	if code, _ := call(t, h, http.MethodPost, "/api/seller/store", map[string]any{
		"name": "Campus Snacks", "location": "Block 32", "city": "Mumbai",
		"categories": []string{"Food"},
	}); code != http.StatusBadRequest {
		t.Fatalf("unserved city: want 400, got %d", code)
	}

	code, created := call(t, h, http.MethodPost, "/api/seller/store", map[string]any{
		"name": "Campus Snacks", "location": "Block 32", "city": "lpu",
		"categories": []string{"Food"},
	})
	if code != http.StatusCreated {
		t.Fatalf("create store: want 201, got %d", code)
	}
	// A new store waits for review, and cannot stock anything until it is
	// through — otherwise the seller fills a shelf nobody can see.
	if created["status"] != "pending" {
		t.Fatalf("a new store starts pending, got %v", created["status"])
	}
	if code, _ := call(t, h, http.MethodPost, "/api/seller/items", map[string]any{
		"title": "Too Soon", "price": 10, "stock": 1,
	}); code != http.StatusForbidden {
		t.Fatalf("stock before approval: want 403, got %d", code)
	}

	admin := adminSignIn(t, h)
	if code, body := callAs(t, h, admin, http.MethodPost,
		"/api/admin/stores/"+DefaultOwner+"/approve", nil); code != http.StatusOK {
		t.Fatalf("approve: want 200, got %d (%v)", code, body["error"])
	}
	addRider(t, h, adminSignIn(t, h), "9876543210")
	somewhereToDeliver(t, h)

	// Reading the store back must round-trip the categories array.
	code, store := call(t, h, http.MethodGet, "/api/seller/store", nil)
	if code != http.StatusOK {
		t.Fatalf("get store: want 200, got %d (%v)", code, store["error"])
	}
	if store["name"] != "Campus Snacks" {
		t.Fatalf("get store: want name Campus Snacks, got %v", store["name"])
	}
	cats := store["categories"].([]any)
	if len(cats) != 1 || cats[0] != "Food" {
		t.Fatalf("categories should round-trip, got %v", cats)
	}

	code, item := call(t, h, http.MethodPost, "/api/seller/items", map[string]any{
		"title": "Cold Coffee 300ml", "category": "Food", "price": 60, "stock": 10,
	})
	if code != http.StatusCreated {
		t.Fatalf("add item: want 201, got %d", code)
	}
	id := item["id"].(string)
	if item["status"] != "in_stock" {
		t.Fatalf("10 units should be in_stock, got %v", item["status"])
	}

	// Price and stock are validated.
	if code, _ := call(t, h, http.MethodPost, "/api/seller/items",
		map[string]any{"title": "Free", "price": 0, "stock": 1}); code != http.StatusBadRequest {
		t.Fatalf("zero price: want 400, got %d", code)
	}

	// Ordering more than exists is refused.
	if code, _ := call(t, h, http.MethodPost, "/api/seller/orders",
		map[string]any{"itemId": id, "units": 99, "expectedTotal": 5955}); code != http.StatusConflict {
		t.Fatalf("oversized order: want 409, got %d", code)
	}

	_, order := call(t, h, http.MethodPost, "/api/seller/orders",
		map[string]any{"itemId": id, "units": 2, "expectedTotal": 135})
	orderID := order["id"].(string)
	if order["amount"].(float64) != 135 {
		t.Fatalf("2 x 60 plus delivery should be 135, got %v", order["amount"])
	}

	// Accepting reserves; delivering is what removes the units.
	call(t, h, http.MethodPost, "/api/seller/orders/"+orderID+"/accept", nil)
	_, body := call(t, h, http.MethodGet, "/api/seller/items", nil)
	items := body["items"].([]any)
	if items[0].(map[string]any)["stock"].(float64) != 10 {
		t.Fatal("accepting must not change stock")
	}

	call(t, h, http.MethodPost, "/api/seller/orders/"+orderID+"/deliver", nil)
	_, body = call(t, h, http.MethodGet, "/api/seller/items", nil)
	items = body["items"].([]any)
	if got := items[0].(map[string]any)["stock"].(float64); got != 8 {
		t.Fatalf("delivering 2 of 10 should leave 8, got %v", got)
	}
	summary := body["summary"].(map[string]any)
	if summary["inventoryValue"].(float64) != 480 {
		t.Fatalf("8 x 60 should be 480, got %v", summary["inventoryValue"])
	}

	// Delivering twice is refused rather than double-decrementing.
	if code, _ := call(t, h, http.MethodPost,
		"/api/seller/orders/"+orderID+"/deliver", nil); code != http.StatusConflict {
		t.Fatalf("second deliver: want 409, got %d", code)
	}
}

func TestStockNeverGoesNegative(t *testing.T) {
	h := testAPI(t)
	openApprovedStore(t, h, map[string]any{
		"name": "S", "location": "L", "city": "LPU", "categories": []string{"Food"},
	})
	_, item := call(t, h, http.MethodPost, "/api/seller/items",
		map[string]any{"title": "Sandwich", "price": 40, "stock": 3})
	id := item["id"].(string)

	_, patched := call(t, h, http.MethodPatch, "/api/seller/items/"+id+"/stock",
		map[string]any{"delta": -10})
	if patched["stock"].(float64) != 0 {
		t.Fatalf("stock floors at 0, got %v", patched["stock"])
	}
	if patched["status"] != "sold_out" {
		t.Fatalf("0 units is sold_out, got %v", patched["status"])
	}
}

// Twenty buyers, five units. Before order placement became a single locked
// statement, the read-then-insert let every one of them through.
func TestConcurrentOrdersCannotOversell(t *testing.T) {
	h := testAPI(t)
	openApprovedStore(t, h, map[string]any{
		"name": "S", "location": "L", "city": "LPU", "categories": []string{"Food"},
	})
	addRider(t, h, adminSignIn(t, h), "9876543210")
	somewhereToDeliver(t, h)
	_, item := call(t, h, http.MethodPost, "/api/seller/items",
		map[string]any{"title": "Samosa", "price": 20, "stock": 5})
	id := item["id"].(string)

	var wg sync.WaitGroup
	created := make(chan bool, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := call(t, h, http.MethodPost, "/api/seller/orders",
				map[string]any{"itemId": id, "units": 1, "expectedTotal": 35})
			created <- code == http.StatusCreated
		}()
	}
	wg.Wait()
	close(created)

	won := 0
	for ok := range created {
		if ok {
			won++
		}
	}
	if won != 5 {
		t.Fatalf("5 units should fill exactly 5 orders, got %d", won)
	}
}

// The web build sends Authorization on every seller call, and a browser will
// not send the real request unless the preflight says that header is allowed.
// Getting this wrong fails silently: the app catches it and shows sample data.
func TestPreflightAllowsTheHeadersWeSend(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:52614")
	h := testAPI(t)
	for _, path := range []string{
		"/api/seller/items", "/api/push/subscribe", "/api/push/test", "/api/login",
	} {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", "http://localhost:52614")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("%s preflight: want 204, got %d", path, rec.Code)
		}
		allowed := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
		for _, header := range []string{"authorization", "content-type"} {
			if !strings.Contains(allowed, header) {
				t.Errorf("%s preflight does not allow %s: %q", path, header, allowed)
			}
		}
	}
}

// The whole point of the field is answering "is the deploy live?" without
// ssh, so an empty or missing commit is the failure worth catching. Under
// `go test` the toolchain stamps vcs.revision like it does for a build.
func TestHealthSaysWhichCommitIsAnswering(t *testing.T) {
	h := testAPI(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("health is not JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status: want ok, got %q", body["status"])
	}
	if body["commit"] == "" {
		t.Error("health carries no commit, so a deploy cannot be checked")
	}
}
