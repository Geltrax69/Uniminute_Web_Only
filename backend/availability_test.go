package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestStoreDailyAvailabilityScopesCheckoutAndResetsInIST(t *testing.T) {
	h := testAPI(t)
	db := lastTestDB
	addRider(t, h, adminSignIn(t, h), "9876543210")
	openApprovedStore(t, h, map[string]any{"name": "Daily Store", "location": "Block 32", "city": "LPU", "categories": []string{"Food"}})
	somewhereToDeliver(t, h)
	_, item := call(t, h, "POST", "/api/seller/items", map[string]any{"title": "Burger", "price": 69, "stock": 10})
	id := item["id"].(string)
	_, store := call(t, h, "GET", "/api/seller/store", nil)
	if store["closedToday"] != false {
		t.Fatal("new store must default open")
	}
	other := signIn(t, db, "other-seller@example.test")
	if status, _ := callAs(t, h, other, "PUT", "/api/seller/availability", map[string]any{"available": false}); status != 403 {
		t.Fatal("another seller changed this store")
	}
	if status, _ := call(t, h, "PUT", "/api/seller/availability", map[string]any{}); status != 400 {
		t.Fatal("missing availability accepted")
	}
	for range 2 {
		if status, _ := call(t, h, "PUT", "/api/seller/availability", map[string]any{"available": false}); status != 200 {
			t.Fatal("could not close store idempotently")
		}
	}
	if status, state := call(t, h, "GET", "/api/seller/availability", nil); status != 200 || state["closedToday"] != true {
		t.Fatal("authoritative store availability read failed")
	}
	var until, timeNow time.Time
	if err := db.sql.QueryRow(`SELECT closed_until,now() FROM seller_stores WHERE owner=$1`, DefaultOwner).Scan(&until, &timeNow); err != nil {
		t.Fatal(err)
	}
	ist := time.FixedZone("IST", 19800)
	n := timeNow.In(ist)
	expected := time.Date(n.Year(), n.Month(), n.Day()+1, 0, 0, 0, 0, ist)
	if !until.Equal(expected) {
		t.Fatalf("day off must expire at midnight IST: %v", until)
	}
	if status, p := call(t, h, "GET", "/api/products/"+id, nil); status != 200 || p["storeClosed"] != true {
		t.Fatalf("public product lost closed state: %d %v", status, p)
	}
	found := false
	for _, row := range callList(t, h, "/api/shops") {
		s := row.(map[string]any)
		if s["name"] == "Daily Store" {
			found = s["closedToday"] == true
		}
	}
	if !found {
		t.Fatal("public shop has no closed badge state")
	}
	basket := map[string]any{"lines": []map[string]any{{"itemId": id, "units": 1}}, "expectedTotal": 84, "requestId": "availability-checkout-123"}
	if status, _ := call(t, h, "POST", "/api/orders/checkout", basket); status != 409 {
		t.Fatal("closed shop accepted a new order")
	}
	var count int
	db.sql.QueryRow(`SELECT count(*) FROM orders`).Scan(&count)
	if count != 0 {
		t.Fatal("closed-shop rejection created an order")
	}
	// No cron needed: an expired timestamp is open everywhere.
	db.sql.Exec(`UPDATE seller_stores SET closed_until=now()-interval '1 second' WHERE owner=$1`, DefaultOwner)
	if _, p := call(t, h, "GET", "/api/products/"+id, nil); p["storeClosed"] != false {
		t.Fatal("store did not reopen after expiry")
	}
	if status, out := call(t, h, "POST", "/api/orders/checkout", basket); status != 201 {
		t.Fatalf("reopened store checkout: %d %v", status, out)
	}
	if status, _ := call(t, h, "PUT", "/api/seller/availability", map[string]any{"available": true}); status != 200 {
		t.Fatal("could not explicitly reopen")
	}
	db.sql.Exec(`UPDATE seller_stores SET status='suspended' WHERE owner=$1`, DefaultOwner)
	if status, _ := call(t, h, "PUT", "/api/seller/availability", map[string]any{"available": true}); status != 403 {
		t.Fatal("availability bypassed admin suspension")
	}
}

func TestRiderDayOffReleasesUncollectedWorkAndPreservesCurrentRun(t *testing.T) {
	h := testAPI(t)
	db := lastTestDB
	admin := adminSignIn(t, h)
	openApprovedStore(t, h, map[string]any{"name": "Daily Deliveries", "location": "Block 32", "city": "LPU", "categories": []string{"Food"}})
	somewhereToDeliver(t, h)
	_, item := call(t, h, "POST", "/api/seller/items", map[string]any{"title": "Burger", "price": 69, "stock": 20})
	itemID := item["id"].(string)
	tokens := map[string]string{}
	for _, phone := range []string{"9111111111", "9222222222"} {
		pin := addRider(t, h, admin, phone)
		_, login := callAs(t, h, "", "POST", "/api/delivery/login", map[string]any{"phone": phone, "pin": pin})
		tokens[phone] = login["token"].(string)
	}
	first, second := tokens["9111111111"], tokens["9222222222"]
	if _, p := callAs(t, h, first, "GET", "/api/delivery/orders", nil); p["rider"].(map[string]any)["dayOffToday"] != false {
		t.Fatal("rider must default online")
	}
	for _, id := range []string{"off-pending", "off-accepted", "off-picked", "other-rider"} {
		stage, assigned, rider := "accepted", "9111111111", ""
		if id == "off-pending" {
			stage = "received"
		}
		if id == "off-picked" {
			stage = "picked"
			rider = assigned
		}
		if id == "other-rider" {
			assigned = "9222222222"
		}
		if _, err := db.sql.Exec(`INSERT INTO orders(id,item_id,item_title,units,amount,stage,buyer_email,store_owner,store_name,assigned_to,rider_phone,delivery_code) VALUES($1,$2,'Burger',1,84,$3,$4,$4,'Daily Deliveries',$5,$6,'1234')`, id, itemID, stage, DefaultOwner, assigned, rider); err != nil {
			t.Fatal(err)
		}
	}
	if status, _ := callAs(t, h, first, "PUT", "/api/delivery/availability", map[string]any{"available": false}); status != 200 {
		t.Fatal("could not take day off")
	}
	if status, state := callAs(t, h, first, "GET", "/api/delivery/availability", nil); status != 200 || state["dayOffToday"] != true {
		t.Fatal("authoritative rider availability read failed")
	}
	var assigned string
	db.sql.QueryRow(`SELECT assigned_to FROM orders WHERE id='off-accepted'`).Scan(&assigned)
	if assigned != "" {
		t.Fatal("uncollected assignment stranded")
	}
	_, panel := callAs(t, h, first, "GET", "/api/delivery/orders", nil)
	orders := panel["orders"].([]any)
	if len(orders) != 1 || orders[0].(map[string]any)["id"] != "off-picked" {
		t.Fatalf("off-duty panel must retain current run only: %v", orders)
	}
	if panel["rider"].(map[string]any)["dayOffToday"] != true {
		t.Fatal("day off not reflected")
	}
	if status, _ := callAs(t, h, first, "POST", "/api/delivery/orders/off-accepted/pick", nil); status != 409 {
		t.Fatal("off-duty rider collected a new package")
	}
	if status, _ := callAs(t, h, admin, "POST", "/api/admin/orders/off-accepted/assign", map[string]any{"phone": "9111111111"}); status != 409 {
		t.Fatal("admin assigned new work to off-duty rider")
	}
	if status, out := callAs(t, h, first, "POST", "/api/delivery/orders/off-picked/deliver", map[string]any{"code": "1234"}); status != 200 {
		t.Fatalf("off-duty rider could not finish current run: %d %v", status, out)
	}
	if status, _ := callAs(t, h, second, "POST", "/api/delivery/orders/off-accepted/pick", nil); status != 200 {
		t.Fatal("another rider could not collect released package")
	}
	if status, o := call(t, h, "POST", "/api/seller/orders/off-pending/accept", nil); status != 200 || o["assignedTo"] != "9222222222" {
		t.Fatalf("automatic assignment used off-duty rider: %d %v", status, o)
	}
	callAs(t, h, second, "PUT", "/api/delivery/availability", map[string]any{"available": false})
	if status, out := callAs(t, h, "", "GET", "/api/availability", nil); status != 200 || out["deliveryAvailable"] != false {
		t.Fatal("all riders off must pause delivery")
	}
	if status, out := call(t, h, "POST", "/api/orders", map[string]any{"itemId": itemID, "units": 1, "expectedTotal": 84}); status != 503 {
		t.Fatalf("all-riders-off checkout: %d %v", status, out)
	}
	db.sql.Exec(`UPDATE riders SET day_off_until=now()-interval '1 second' WHERE phone='9111111111'`)
	if _, out := callAs(t, h, "", "GET", "/api/availability", nil); out["deliveryAvailable"] != true {
		t.Fatal("rider did not become available after day-off expiry")
	}
	if status, _ := callAs(t, h, first, "PUT", "/api/delivery/availability", map[string]any{"available": true}); status != 200 {
		t.Fatal("rider could not go online early")
	}
	// Day-off controls cannot reactivate an administratively disabled account.
	callAs(t, h, admin, "DELETE", "/api/admin/riders/"+url.PathEscape("9111111111"), nil)
	if status, _ := callAs(t, h, first, "PUT", "/api/delivery/availability", map[string]any{"available": true}); status != 401 {
		t.Fatal("disabled rider regained access")
	}
}

// The public checkout handler verifies provider proof before reaching
// placeBasket. Recording an already captured payment must still work when a
// shop or rider takes the day off after checkout started.
func TestCapturedPaymentIsRecordedAfterDailyAvailabilityChanges(t *testing.T) {
	h := testAPI(t)
	db := lastTestDB
	openApprovedStore(t, h, map[string]any{"name": "Paid before close", "location": "Block 32", "city": "LPU", "categories": []string{"Food"}})
	somewhereToDeliver(t, h)
	_, item := call(t, h, "POST", "/api/seller/items", map[string]any{"title": "Burger", "price": 69, "stock": 10})
	call(t, h, "PUT", "/api/seller/availability", map[string]any{"available": false})
	api := &API{db: db}
	r := httptest.NewRequest("POST", "/api/orders/checkout", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r = r.WithContext(context.WithValue(r.Context(), ownerKey{}, DefaultOwner))
	total := 84.0
	w := httptest.NewRecorder()
	api.placeBasket(w, r, []checkoutLine{{ItemID: item["id"].(string), Units: 1}}, "", &total, false, "captured-before-closed", &paymentProof{OrderID: "order_paid_before_close", PaymentID: "pay_fixture"})
	if w.Code != 201 {
		t.Fatalf("captured payment stranded after close: %d %s", w.Code, w.Body.String())
	}
	var status string
	db.sql.QueryRow(`SELECT payment_status FROM orders WHERE razorpay_order_id='order_paid_before_close'`).Scan(&status)
	if status != "paid" {
		t.Fatal("captured payment not recorded as paid")
	}
}

func TestDayOffAndPickupCannotStrandAPackage(t *testing.T) {
	h := testAPI(t)
	db := lastTestDB
	phone := "9111111111"
	_, err := db.sql.Exec(`INSERT INTO riders(phone,name,pin_hash) VALUES('9111111111','Concurrent rider','fixture'); INSERT INTO seller_stores(owner,name,location,city,status) VALUES('fixture-owner','Fixture store','Campus','LPU','approved'); INSERT INTO inventory_items(id,owner,title,price,stock) VALUES('availability-race-item','fixture-owner','Package',69,20)`)
	if err != nil {
		t.Fatal(err)
	}
	token, err := db.newStaffSession(context.Background(), "rider", phone)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		callAs(t, h, token, "PUT", "/api/delivery/availability", map[string]any{"available": true})
		id := fmt.Sprintf("availability-race-%d", i)
		_, err = db.sql.Exec(`INSERT INTO orders(id,item_id,item_title,units,amount,stage,store_owner,assigned_to) VALUES($1,'availability-race-item','Package',1,84,'accepted','fixture-owner',$2)`, id, phone)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		picked := make(chan int, 1)
		off := make(chan int, 1)
		go func() {
			<-start
			status, _ := callAs(t, h, token, "POST", "/api/delivery/orders/"+id+"/pick", nil)
			picked <- status
		}()
		go func() {
			<-start
			status, _ := callAs(t, h, token, "PUT", "/api/delivery/availability", map[string]any{"available": false})
			off <- status
		}()
		close(start)
		pickStatus, offStatus := <-picked, <-off
		if offStatus != 200 || (pickStatus != 200 && pickStatus != 409) {
			t.Fatalf("race status: pick %d off %d", pickStatus, offStatus)
		}
		var stage, assigned, rider string
		db.sql.QueryRow(`SELECT stage,assigned_to,rider_phone FROM orders WHERE id=$1`, id).Scan(&stage, &assigned, &rider)
		if pickStatus == 200 {
			if stage != "picked" || rider != phone {
				t.Fatal("picked package lost its rider")
			}
		} else if stage != "accepted" || assigned != "" || rider != "" {
			t.Fatal("uncollected package did not return to pool")
		}
	}
}
