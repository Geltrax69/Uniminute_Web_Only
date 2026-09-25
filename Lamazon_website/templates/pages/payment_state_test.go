package pages

import (
	"testing"

	"lamazon/website/backend"
)

func TestUnpaidRazorpayOrderNeedsPaymentAction(t *testing.T) {
	o := backend.Order{Stage: backend.StageReceived, PaymentMethod: "razorpay", PaymentStatus: "due"}
	if !paymentNeedsAction(o) || statusLabel(o) != "Payment issue" {
		t.Fatalf("unpaid Razorpay order was shown as %q", statusLabel(o))
	}
	if got := paymentModeLabel(o); got != "Razorpay · payment incomplete" {
		t.Fatalf("payment label = %q", got)
	}

	o.PaymentStatus = "paid"
	if paymentNeedsAction(o) || statusLabel(o) != "Waiting for store acceptance" {
		t.Fatalf("captured Razorpay order was shown as %q", statusLabel(o))
	}
	if got := paymentModeLabel(o); got != "Razorpay · paid online" {
		t.Fatalf("payment label = %q", got)
	}
}
