package main

import "testing"

func TestCapturedPaymentMatchesOrder(t *testing.T) {
	valid := map[string]interface{}{
		"order_id": "order_test", "status": "captured", "amount": float64(812000), "currency": "INR",
	}
	if !capturedPaymentMatches(valid, "order_test", 812000) {
		t.Fatal("captured payment for the Razorpay order was rejected")
	}

	for name, payment := range map[string]map[string]interface{}{
		"failed":       {"order_id": "order_test", "status": "failed", "amount": float64(812000), "currency": "INR"},
		"authorized":   {"order_id": "order_test", "status": "authorized", "amount": float64(812000), "currency": "INR"},
		"wrong order":  {"order_id": "order_other", "status": "captured", "amount": float64(812000), "currency": "INR"},
		"wrong amount": {"order_id": "order_test", "status": "captured", "amount": float64(811900), "currency": "INR"},
	} {
		t.Run(name, func(t *testing.T) {
			if capturedPaymentMatches(payment, "order_test", 812000) {
				t.Fatal("non-matching payment was accepted")
			}
		})
	}
}
