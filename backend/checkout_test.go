package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
)

func TestValidPaymentProof(t *testing.T) {
	proof := paymentProof{OrderID: "order_test", PaymentID: "pay_test"}
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte(proof.OrderID + "|" + proof.PaymentID))
	proof.Signature = hex.EncodeToString(mac.Sum(nil))
	if !validPaymentProof(proof, "secret") {
		t.Fatal("valid Razorpay payment proof was rejected")
	}
	proof.PaymentID += "-tampered"
	if validPaymentProof(proof, "secret") {
		t.Fatal("tampered Razorpay payment proof was accepted")
	}
}

func TestBasketCheckoutOneFeeAndMatchingViews(t *testing.T) {
	h := testAPI(t)
	addRider(t, h, adminSignIn(t, h), "9876543210")
	openApprovedStore(t, h, map[string]any{"name": "Basket Store", "location": "Block 32", "city": "LPU", "categories": []string{"Food"}})
	somewhereToDeliver(t, h)
	_, first := call(t, h, http.MethodPost, "/api/seller/items", map[string]any{"title": "Burger", "price": 69, "stock": 10})
	_, second := call(t, h, http.MethodPost, "/api/seller/items", map[string]any{"title": "Drink", "price": 30, "stock": 10})
	code, body := call(t, h, http.MethodPost, "/api/orders/checkout", map[string]any{
		"lines": []map[string]any{{"itemId": first["id"], "units": 1}, {"itemId": second["id"], "units": 2}}, "requestId": "test-basket-123456", "expectedTotal": 144,
	})
	if code != 201 {
		t.Fatalf("checkout: %d %v", code, body)
	}
	if body["amount"] != float64(144) || body["deliveryFee"] != float64(15) {
		t.Fatalf("incorrect totals: %v", body)
	}
	checkTotals := func(rows []any) {
		t.Helper()
		var amount, fee float64
		for _, row := range rows {
			o := row.(map[string]any)
			amount += o["amount"].(float64)
			fee += o["deliveryFee"].(float64)
		}
		if len(rows) != 2 || amount != 144 || fee != 15 {
			t.Fatalf("inconsistent view: count=%d amount=%v fee=%v", len(rows), amount, fee)
		}
	}
	checkTotals(body["orders"].([]any))
	_, mine := call(t, h, http.MethodGet, "/api/orders", nil)
	checkTotals(mine["orders"].([]any))
	_, seller := call(t, h, http.MethodGet, "/api/seller/orders", nil)
	checkTotals(seller["orders"].([]any))
	admin := adminSignIn(t, h)
	pin := addRider(t, h, admin, "9876543210")
	_, login := callAs(t, h, "", http.MethodPost, "/api/delivery/login", map[string]string{"phone": "9876543210", "pin": pin})
	for _, row := range body["orders"].([]any) {
		id := row.(map[string]any)["id"].(string)
		if code, body := call(t, h, http.MethodPost, "/api/seller/orders/"+id+"/accept", nil); code != 200 {
			t.Fatalf("accept: %d %v", code, body)
		}
	}
	rider := callAs2(t, h, login["token"].(string), "/api/delivery/orders")
	checkTotals(rider["orders"].([]any))
}

func TestBasketCheckoutRollsBackEveryLine(t *testing.T) {
	h := testAPI(t)
	addRider(t, h, adminSignIn(t, h), "9876543210")
	openApprovedStore(t, h, map[string]any{"name": "S", "location": "L", "city": "LPU", "categories": []string{"Food"}})
	somewhereToDeliver(t, h)
	_, item := call(t, h, http.MethodPost, "/api/seller/items", map[string]any{"title": "Burger", "price": 69, "stock": 10})
	for _, tc := range []struct {
		name   string
		lines  []map[string]any
		total  float64
		status int
	}{
		{"missing line", []map[string]any{{"itemId": item["id"], "units": 1}, {"itemId": "zz-missing", "units": 1}}, 84, 404},
		{"price mismatch", []map[string]any{{"itemId": item["id"], "units": 1}}, 69, 409},
		{"oversell", []map[string]any{{"itemId": item["id"], "units": 11}}, 774, 409},
		{"duplicate", []map[string]any{{"itemId": item["id"], "units": 1}, {"itemId": item["id"], "units": 1}}, 153, 400},
		{"invalid quantity", []map[string]any{{"itemId": item["id"], "units": -1}}, 84, 400},
		{"empty", []map[string]any{}, 15, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := call(t, h, http.MethodPost, "/api/orders/checkout", map[string]any{"lines": tc.lines, "requestId": "test-basket-123456", "expectedTotal": tc.total})
			if code != tc.status {
				t.Fatalf("got %d %v", code, body)
			}
			_, mine := call(t, h, http.MethodGet, "/api/orders", nil)
			if len(mine["orders"].([]any)) != 0 {
				t.Fatal("failed basket created a partial order")
			}
		})
	}
	// Old builds cannot silently accumulate a fee on every basket line.
	code, _ := call(t, h, http.MethodPost, "/api/orders", map[string]any{"itemId": item["id"], "units": 1})
	if code != 409 {
		t.Fatalf("unconfirmed legacy checkout: %d", code)
	}
}

func TestCheckoutRetryDoesNotDuplicateOrders(t *testing.T) {
	h := testAPI(t)
	addRider(t, h, adminSignIn(t, h), "9876543210")
	openApprovedStore(t, h, map[string]any{"name": "Retry Store", "location": "L", "city": "LPU", "categories": []string{"Food"}})
	somewhereToDeliver(t, h)
	_, item := call(t, h, http.MethodPost, "/api/seller/items", map[string]any{"title": "Burger", "price": 69, "stock": 1})
	// Checkout must not request a second connection while its transaction holds the only one.
	lastTestDB.sql.SetMaxOpenConns(1)
	payload := map[string]any{"lines": []map[string]any{{"itemId": item["id"], "units": 1}}, "requestId": "retry-basket-123456", "expectedTotal": 84}
	type result struct {
		code int
		body map[string]any
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			code, body := call(t, h, http.MethodPost, "/api/orders/checkout", payload)
			results <- result{code, body}
		}()
	}
	var firstID any
	for range 2 {
		res := <-results
		if res.code != 201 {
			t.Fatalf("concurrent retry: %d %v", res.code, res.body)
		}
		id := res.body["orders"].([]any)[0].(map[string]any)["id"]
		if firstID != nil && id != firstID {
			t.Fatal("retry created a different order")
		}
		firstID = id
	}
	// A committed checkout can still be recovered after delivery closes or the
	// buyer deletes their saved address; it must not run today's validation again.
	if _, err := lastTestDB.sql.Exec(`UPDATE riders SET active=false; DELETE FROM addresses`); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, h, http.MethodPost, "/api/orders/checkout", payload)
	if code != 201 || body["orders"].([]any)[0].(map[string]any)["id"] != firstID {
		t.Fatalf("restart retry: %d %v", code, body)
	}
	_, mine := call(t, h, http.MethodGet, "/api/orders", nil)
	if len(mine["orders"].([]any)) != 1 {
		t.Fatal("duplicate order exists")
	}
	payload["expectedTotal"] = 85
	if code, _ := call(t, h, http.MethodPost, "/api/orders/checkout", payload); code != 409 {
		t.Fatalf("changed payload reused ID: %d", code)
	}
}

func TestCatalogShowsUnreservedStock(t *testing.T) {
	h := testAPI(t)
	addRider(t, h, adminSignIn(t, h), "9876543210")
	openApprovedStore(t, h, map[string]any{"name": "Stock Store", "location": "L", "city": "LPU", "categories": []string{"Food"}})
	somewhereToDeliver(t, h)
	_, item := call(t, h, "POST", "/api/seller/items", map[string]any{"title": "Stock Burger", "price": 20, "stock": 3})
	_, order := call(t, h, "POST", "/api/orders", map[string]any{"itemId": item["id"], "units": 2, "expectedTotal": 55})
	found, err := lastTestDB.product(context.Background(), item["id"].(string))
	if err != nil || found.AvailableStock == nil || *found.AvailableStock != 1 {
		t.Fatalf("reserved stock was advertised: %+v %v", found, err)
	}
	call(t, h, "POST", "/api/orders/"+order["id"].(string)+"/cancel", nil)
	found, err = lastTestDB.product(context.Background(), item["id"].(string))
	if err != nil || found.AvailableStock == nil || *found.AvailableStock != 3 {
		t.Fatalf("cancelled stock not restored: %+v %v", found, err)
	}
}
