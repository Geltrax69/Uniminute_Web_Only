package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type eraseFixture struct {
	h                                     http.Handler
	sent                                  *sentMail
	db                                    *DB
	admin, buyerToken, refresh, email, id string
}

func buyerEraseFixture(t *testing.T) eraseFixture {
	t.Helper()
	h, sent := mailAPI(t)
	db := lastTestDB
	const email = "erase-buyer@example.test"
	hash, err := hashPassword("Buyer-old-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.Exec(`INSERT INTO users(email,name,phone,pass_hash,avatar,interest) VALUES($1,'Private Buyer','9876543210',$2,'foodie-female','foodie'),('untouched@example.test','Keep me','9876543211','','','')`, email, hash); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO addresses(email,label,line,city) VALUES('erase-buyer@example.test','Home','Private hostel 214','LPU')`,
		`INSERT INTO seller_stores(owner,name,location,city,status) VALUES('unrelated-seller@example.test','Other business','Campus','LPU','approved')`,
		`INSERT INTO inventory_items(id,owner,title,price,stock) VALUES('erase-fixture-item','unrelated-seller@example.test','Existing sale',100,10)`,
		`INSERT INTO orders(id,item_id,item_title,units,amount,stage,buyer_email,store_owner,receiver_name,receiver_phone,receiver_address,delivery_code,reject_reason) VALUES('erase-fixture-order','erase-fixture-item','Existing sale',1,100,'delivered','erase-buyer@example.test','unrelated-seller@example.test','Private Buyer','9876543210','Private hostel 214','1234','Private note')`,
		`INSERT INTO order_reviews(order_id,buyer_email,item_id,item_rating,item_text) VALUES('erase-fixture-order','erase-buyer@example.test','erase-fixture-item',5,'Private review')`,
		`INSERT INTO notifications(email,title,body) VALUES('erase-buyer@example.test','Private message','Private details'),('untouched@example.test','Keep notification','Keep details')`,
		`INSERT INTO push_subscriptions(endpoint,email,p256dh,auth) VALUES('fixture://erase-device','erase-buyer@example.test','','')`,
		`INSERT INTO login_codes(email,code_hash,expires_at,sent_at) VALUES('erase-buyer@example.test','fixture-old-code',now()+interval '10 minutes',now())`,
		`INSERT INTO checkout_attempts(buyer_email,request_id,fingerprint,response) VALUES('erase-buyer@example.test','old-basket-fixture','fixture','{"email":"erase-buyer@example.test"}')`,
	}
	for _, q := range statements {
		if _, err = db.sql.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.sql.Exec(`INSERT INTO payment_gateway_attempts(attempt_key,account_hash,cart_id,receipt,lease,order_id,created_at) VALUES('erase-attempt',$1,'old-cart','erase-receipt','erase-lease','order_old_fixture',now()-interval '2 hours')`, hashCode(email)); err != nil {
		t.Fatal(err)
	}
	session, err := db.newSession(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err = db.sql.QueryRow(`SELECT public_id FROM users WHERE email=$1`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return eraseFixture{h, sent, db, adminSignIn(t, h), session.Token, session.RefreshToken, email, id}
}
func eraseBody(email string) map[string]any {
	return map[string]any{"email": email, "password": "Sup3r$ecret"}
}
func TestAdminBuyerDeletionPurgesIdentityAndForcesFreshSignup(t *testing.T) {
	f := buyerEraseFixture(t)
	path := "/api/admin/people/" + f.id
	if status, _ := callAs(t, f.h, f.buyerToken, "DELETE", path, eraseBody(f.email)); status != 401 {
		t.Fatal("buyer reached admin deletion")
	}
	if status, _ := callAs(t, f.h, f.admin, "DELETE", path, eraseBody("wrong@example.test")); status != 400 {
		t.Fatal("wrong confirmation accepted")
	}
	if status, _ := callAs(t, f.h, f.admin, "DELETE", path, map[string]any{"email": f.email, "password": "wrong"}); status != 401 {
		t.Fatal("wrong admin password accepted")
	}
	if status, out := callAs(t, f.h, f.admin, "DELETE", path, eraseBody(f.email)); status != 200 || out["deleted"] != true {
		t.Fatalf("delete: %d %v", status, out)
	}
	for _, q := range []string{
		`SELECT count(*) FROM users WHERE email=$1`, `SELECT count(*) FROM addresses WHERE email=$1`, `SELECT count(*) FROM auth_sessions WHERE email=$1`, `SELECT count(*) FROM login_codes WHERE email=$1`, `SELECT count(*) FROM push_subscriptions WHERE email=$1`, `SELECT count(*) FROM notifications WHERE email=$1`, `SELECT count(*) FROM checkout_attempts WHERE buyer_email=$1`, `SELECT count(*) FROM order_reviews WHERE buyer_email=$1`,
	} {
		var n int
		if err := f.db.sql.QueryRow(q, f.email).Scan(&n); err != nil || n != 0 {
			t.Fatalf("personal data retained: %s %d %v", q, n, err)
		}
	}
	var buyer, name, phone, address, code, reason string
	var amount float64
	if err := f.db.sql.QueryRow(`SELECT buyer_email,receiver_name,receiver_phone,receiver_address,delivery_code,reject_reason,amount FROM orders WHERE id='erase-fixture-order'`).Scan(&buyer, &name, &phone, &address, &code, &reason, &amount); err != nil {
		t.Fatal(err)
	}
	if buyer == f.email || name != "Deleted buyer" || phone != "" || address != "" || code != "" || reason != "" || amount != 100 {
		t.Fatal("historical order was not anonymized without changing its amount")
	}
	if status, _ := callAs(t, f.h, f.buyerToken, "GET", "/api/me", nil); status != 401 {
		t.Fatal("old access token survived")
	}
	if status, _ := callAs(t, f.h, "", "POST", "/api/login/refresh", map[string]any{"refreshToken": f.refresh}); status != 401 {
		t.Fatal("old refresh token survived")
	}
	if status, _ := callAs(t, f.h, "", "POST", "/api/login/password", map[string]any{"email": f.email, "password": "Buyer-old-password"}); status != 401 {
		t.Fatal("old password survived")
	}
	status, out := callAs(t, f.h, "", "POST", "/api/login", map[string]any{"email": f.email})
	if status != 200 || out["needsPassword"] == true || f.sent.count != 1 {
		t.Fatalf("fresh signup did not request email verification: %d %v", status, out)
	}
	status, out = callAs(t, f.h, "", "POST", "/api/login/verify", map[string]any{"email": f.email, "code": codeFor(t, f.sent)})
	if status != 200 {
		t.Fatalf("fresh verification: %d", status)
	}
	user := out["user"].(map[string]any)
	if user["id"] == f.id || user["ready"] == true || user["hasPassword"] == true || user["name"] != "" || user["phone"] != "" || user["avatar"] != "" || user["hasAddress"] == true {
		t.Fatalf("old profile reattached: %v", user)
	}
	if _, orders := callAs(t, f.h, out["token"].(string), "GET", "/api/orders", nil); len(orders["orders"].([]any)) != 0 {
		t.Fatal("new signup inherited old orders")
	}
	var otherName string
	if err := f.db.sql.QueryRow(`SELECT name FROM users WHERE email='untouched@example.test'`).Scan(&otherName); err != nil || otherName != "Keep me" {
		t.Fatal("another buyer was altered")
	}
}
func TestAdminBuyerDeletionBlocksStoresAndActiveWork(t *testing.T) {
	f := buyerEraseFixture(t)
	path := "/api/admin/people/" + f.id
	if _, err := f.db.sql.Exec(`UPDATE orders SET stage='received' WHERE id='erase-fixture-order'`); err != nil {
		t.Fatal(err)
	}
	if status, _ := callAs(t, f.h, f.admin, "DELETE", path, eraseBody(f.email)); status != 409 {
		t.Fatal("active order did not block deletion")
	}
	f.db.sql.Exec(`UPDATE orders SET stage='delivered' WHERE id='erase-fixture-order';UPDATE payment_gateway_attempts SET created_at=now() WHERE attempt_key='erase-attempt'`)
	if status, _ := callAs(t, f.h, f.admin, "DELETE", path, eraseBody(f.email)); status != 409 {
		t.Fatal("unsettled payment did not block deletion")
	}
	f.db.sql.Exec(`DELETE FROM payment_gateway_attempts;INSERT INTO seller_stores(owner,name,location,city,status) VALUES('erase-buyer@example.test','Protected business','LPU','LPU','approved')`)
	if status, out := callAs(t, f.h, f.admin, "DELETE", path, eraseBody(f.email)); status != 409 || !strings.Contains(out["error"].(string), "Store owners") {
		t.Fatal("store owner could be deleted")
	}
	if status, _ := callAs(t, f.h, f.buyerToken, "GET", "/api/me", nil); status != 200 {
		t.Fatal("refused deletion revoked valid session")
	}
}
func TestAdminBuyerDeletionIsAtomicAndPreventsBootstrapResurrection(t *testing.T) {
	f := buyerEraseFixture(t)
	path := "/api/admin/people/" + f.id
	if _, err := f.db.sql.Exec(`ALTER TABLE orders ADD CONSTRAINT erase_rollback_fixture CHECK(buyer_email<>'deleted-buyer')`); err != nil {
		t.Fatal(err)
	}
	status, _ := callAs(t, f.h, f.admin, "DELETE", path, eraseBody(f.email))
	f.db.sql.Exec(`ALTER TABLE orders DROP CONSTRAINT erase_rollback_fixture`)
	if status != 503 {
		t.Fatalf("expected rollback failure, got %d", status)
	}
	if status, _ := callAs(t, f.h, f.buyerToken, "GET", "/api/me", nil); status != 200 {
		t.Fatal("partial deletion revoked session")
	}
	t.Setenv("SEED_USER", f.email)
	t.Setenv("SEED_USER_PASSWORD", "bootstrap-password")
	t.Cleanup(func() { f.db.sql.Exec(`DELETE FROM storefront_migrations WHERE id='shopper-bootstrap-disabled'`) })
	if status, _ = callAs(t, f.h, f.admin, "DELETE", path, eraseBody(f.email)); status != 200 {
		t.Fatal("delete failed")
	}
	if err := f.db.seedUser(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	f.db.sql.QueryRow(`SELECT count(*) FROM users WHERE email=$1`, f.email).Scan(&n)
	if n != 0 {
		t.Fatal("restart recreated deleted seed account")
	}
}
func TestStoreSuspensionHidesAllCataloguePathsAndBlocksNewProducts(t *testing.T) {
	h := testAPI(t)
	db := lastTestDB
	admin := adminSignIn(t, h)
	var name string
	if err := db.sql.QueryRow(`SELECT store FROM products WHERE store IN(SELECT name FROM shops) LIMIT 1`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`INSERT INTO seller_stores(owner,name,location,city,status,categories) VALUES($1,$2,'LPU','LPU','approved',ARRAY['Food']);`, DefaultOwner, name); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`INSERT INTO inventory_items(id,owner,title,price,stock) VALUES('suspend-item',$1,'Suspension fixture',100,5)`, DefaultOwner); err != nil {
		t.Fatal(err)
	}
	// Seeded offers must not keep a suspended store's direct page alive.
	if _, err := db.sql.Exec(`INSERT INTO offers(product_id,store,price) SELECT id,$1,10 FROM products WHERE store<>$1 LIMIT 1 ON CONFLICT DO NOTHING`, name); err != nil {
		t.Fatal(err)
	}
	defer db.sql.Exec(`DELETE FROM offers WHERE store=$1 AND price=10`, name)
	for _, state := range []string{"suspend", "restore"} {
		if status, _ := call(t, h, "POST", "/api/admin/stores/"+url.PathEscape(DefaultOwner)+"/"+state, map[string]any{"reason": "Review required"}); status != 401 {
			t.Fatal("seller could moderate own store")
		}
	}
	path := "/api/admin/stores/" + url.PathEscape(DefaultOwner)
	if status, _ := callAs(t, h, admin, "POST", path+"/suspend", map[string]any{"reason": ""}); status != 400 {
		t.Fatal("empty suspension reason accepted")
	}
	if status, out := callAs(t, h, admin, "POST", path+"/suspend", map[string]any{"reason": "Verification required"}); status != 200 {
		t.Fatalf("suspend: %d %v", status, out)
	}
	for _, route := range []string{"/api/products", "/api/products?tab=Food", "/api/products?q=" + url.QueryEscape(name), "/api/shops/" + url.PathEscape(name) + "/products"} {
		for _, raw := range callList(t, h, route) {
			p := raw.(map[string]any)
			if p["store"] == name {
				t.Fatalf("suspended store leaked in %s", route)
			}
		}
	}
	if rows := callList(t, h, "/api/shops/"+url.PathEscape(name)+"/products"); len(rows) != 0 {
		t.Fatal("suspended offer kept the direct store page alive")
	}

	for _, raw := range callList(t, h, "/api/shops") {
		if raw.(map[string]any)["name"] == name {
			t.Fatal("seeded shop row bypassed suspension")
		}
	}
	if status, _ := call(t, h, "GET", "/api/products/suspend-item", nil); status != 404 {
		t.Fatal("direct product remained public")
	}
	if status, _ := call(t, h, "POST", "/api/seller/items", map[string]any{"title": "Blocked product", "price": 10, "stock": 1}); status != 403 {
		t.Fatal("suspended seller added a product")
	}
	if status, _ := call(t, h, "POST", "/api/seller/items/suspend-item/photos", nil); status != 403 {
		t.Fatal("suspended seller uploaded product photos")
	}
	if status, out := call(t, h, "POST", "/api/seller/store", map[string]any{"name": name, "location": "New address", "city": "LPU", "categories": []string{"Food"}}); status != 201 || out["status"] != "suspended" {
		t.Fatal("editing store lifted suspension")
	}
	if status, _ := callAs(t, h, admin, "POST", path+"/restore", nil); status != 200 {
		t.Fatal("restore failed")
	}
	if !onSale(callList(t, h, "/api/products"), "Suspension fixture") {
		t.Fatal("restore did not restore original inventory")
	}
}
