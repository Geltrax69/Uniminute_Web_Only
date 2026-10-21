package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type isolationFixture struct {
	h    http.Handler
	db   *DB
	a, b string
}

func securityFixture(t *testing.T) isolationFixture {
	t.Helper()
	h := testAPI(t)
	db := lastTestDB
	f := isolationFixture{h: h, db: db, a: testToken, b: signIn(t, db, "seller-b@example.test")}
	for _, s := range []struct{ owner, name, id, customer, phone, address string }{{DefaultOwner, "Store A", "security-a", "Customer Alice", "9876543210", "Private address A"}, {"seller-b@example.test", "Store B", "security-b", "Customer Bob", "9876543211", "Private address B"}} {
		if _, err := db.upsertUser(context.Background(), s.owner); err != nil {
			t.Fatal(err)
		}
		if _, err := db.sql.Exec(`INSERT INTO seller_stores(owner,name,location,city,categories,status) VALUES($1,$2,'Campus','LPU',ARRAY['Food'],'approved')`, s.owner, s.name); err != nil {
			t.Fatal(err)
		}
		if _, err := db.sql.Exec(`INSERT INTO inventory_items(id,owner,title,price,stock,image_urls) VALUES($1,$2,$3,100,10,ARRAY['https://res.cloudinary.com/test/fixture.png'])`, s.id, s.owner, s.name+" item"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.sql.Exec(`INSERT INTO orders(id,item_id,item_title,units,amount,stage,store_owner,store_name,buyer_email,receiver_name,receiver_phone,receiver_address) VALUES($1,$2,$3,1,100,'received',$4,$5,$6,$7,$8,$9)`, s.id+"-order", s.id, s.name+" item", s.owner, s.name, "buyer-"+s.id+"@example.test", s.customer, s.phone, s.address); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func rawAs(h http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestSellerIsolationInventoryOrdersAndCustomerDetails(t *testing.T) {
	f := securityFixture(t)
	for _, direction := range []struct{ token, own, foreign, private string }{{f.a, "security-a", "security-b", "Customer Bob"}, {f.b, "security-b", "security-a", "Customer Alice"}} {
		for _, path := range []string{"/api/seller/items", "/api/seller/orders"} {
			w := rawAs(f.h, direction.token, "GET", path+"?owner=ignored&storeOwner=ignored&email=ignored", "")
			if w.Code != 200 {
				t.Fatalf("own list %s: %d %s", path, w.Code, w.Body)
			}
			if !strings.Contains(w.Body.String(), direction.own) {
				t.Fatalf("own data missing from %s", path)
			}
			if strings.Contains(w.Body.String(), direction.foreign) || strings.Contains(w.Body.String(), direction.private) {
				t.Fatalf("cross-store data leaked from %s", path)
			}
		}
		for _, attack := range []struct{ method, path, body string }{
			{"PATCH", "/api/seller/items/" + direction.foreign, `{"title":"Stolen","price":1,"stock":1}`},
			{"PATCH", "/api/seller/items/" + direction.foreign + "/stock", `{"delta":5}`},
			{"PATCH", "/api/seller/items/" + direction.foreign + "/listing", `{"delisted":true}`},
			{"DELETE", "/api/seller/items/" + direction.foreign, ``},
			{"PUT", "/api/seller/items/" + direction.foreign + "/photos", `{"imageUrls":[]}`},
			{"POST", "/api/seller/items/" + direction.foreign + "/photos", ``},
			{"POST", "/api/seller/orders/" + direction.foreign + "-order/accept", ``},
			{"POST", "/api/seller/orders/" + direction.foreign + "-order/reject", `{"reason":"Attack"}`},
			{"POST", "/api/seller/orders/" + direction.foreign + "-order/deliver", `{"code":"0000"}`},
		} {
			w := rawAs(f.h, direction.token, attack.method, attack.path, attack.body)
			if w.Code != 404 {
				t.Errorf("foreign resource should be hidden: %s %s: %d %s", attack.method, attack.path, w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), direction.private) {
				t.Error("error exposed customer details")
			}
		}
		for _, path := range []string{"/api/admin/orders", "/api/admin/reports?month=2026-10", "/api/admin/reports/export?month=2026-10", "/api/admin/security/audit"} {
			if w := rawAs(f.h, direction.token, "GET", path, ""); w.Code != 401 {
				t.Errorf("seller accessed admin data: %s: %d", path, w.Code)
			}
		}
	}
	var altered int
	if err := f.db.sql.QueryRow(`SELECT count(*) FROM inventory_items WHERE id IN ('security-a','security-b') AND (price<>100 OR stock<>10 OR delisted)`).Scan(&altered); err != nil || altered != 0 {
		t.Fatalf("foreign inventory was changed: %d %v", altered, err)
	}
	if err := f.db.sql.QueryRow(`SELECT count(*) FROM orders WHERE id IN ('security-a-order','security-b-order') AND stage<>'received'`).Scan(&altered); err != nil || altered != 0 {
		t.Fatalf("foreign order was changed: %d %v", altered, err)
	}
}
func TestServerSessionRevocation(t *testing.T) {
	h := testAPI(t)
	session, err := lastTestDB.newSession(context.Background(), DefaultOwner)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"accessToken": session.Token, "refreshToken": session.RefreshToken})
	if w := rawAs(h, "", "POST", "/api/login/logout", string(body)); w.Code != 204 {
		t.Fatalf("logout: %d %s", w.Code, w.Body)
	}
	if w := rawAs(h, session.Token, "GET", "/api/me", ""); w.Code != 401 {
		t.Fatal("logged-out access token still worked")
	}
	if w := rawAs(h, "", "POST", "/api/login/refresh", `{"refreshToken":"`+session.RefreshToken+`"}`); w.Code != 401 {
		t.Fatal("logged-out refresh token still worked")
	}
}

func TestPasswordChangesRequireReauthenticationAndRevokeOtherSessions(t *testing.T) {
	h := testAPI(t)
	db := lastTestDB
	original, err := hashPassword("original-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.upsertUser(context.Background(), DefaultOwner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.Exec(`UPDATE users SET pass_hash=$1 WHERE email=$2`, original, DefaultOwner); err != nil {
		t.Fatal(err)
	}
	other, err := db.newSession(context.Background(), DefaultOwner)
	if err != nil {
		t.Fatal(err)
	}
	if w := rawAs(h, testToken, "PATCH", "/api/me", `{"password":"changed-password"}`); w.Code != 401 {
		t.Fatal("password changed without reauthentication")
	}
	if w := rawAs(h, testToken, "PATCH", "/api/me", `{"password":"changed-password","currentPassword":"original-password"}`); w.Code != 200 {
		t.Fatalf("password change %d %s", w.Code, w.Body)
	}
	if w := rawAs(h, other.Token, "GET", "/api/me", ""); w.Code != 401 {
		t.Fatal("other access session survived password change")
	}
	if w := rawAs(h, testToken, "GET", "/api/me", ""); w.Code != 200 {
		t.Fatal("current session was unnecessarily revoked")
	}
	// An email-verified reset must revoke the current browser as well.
	if _, err = db.sql.Exec(`INSERT INTO login_codes(email,code_hash,purpose,expires_at) VALUES($1,$2,'reset',now()+interval '10 minutes')`, DefaultOwner, hashCode("123456")); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"email": DefaultOwner, "code": "123456", "password": "reset-password"})
	if w := rawAs(h, "", "POST", "/api/login/reset", string(payload)); w.Code != 200 {
		t.Fatalf("reset %d %s", w.Code, w.Body)
	}
	if w := rawAs(h, testToken, "GET", "/api/me", ""); w.Code != 401 {
		t.Fatal("old session survived email password reset")
	}
}
