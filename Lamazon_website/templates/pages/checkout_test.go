package pages

import (
	"testing"

	"lamazon/website/backend"
)

func TestPlacedPagePollsOnlyWhileAnOrderIsLive(t *testing.T) {
	live := []backend.Order{{ID: "order-8", Stage: backend.StageAccepted}}
	if got := placedPollTrigger(live); got == "" {
		t.Fatal("accepted order stopped polling before delivery")
	}
	if got := placedRefreshURL(live); got != "/orders/placed?ids=order-8" {
		t.Fatalf("unexpected refresh URL %q", got)
	}

	terminal := []backend.Order{
		{ID: "order-8", Stage: backend.StageDelivered},
		{ID: "order-9", Stage: backend.StageRejected},
	}
	if got := placedPollTrigger(terminal); got != "" {
		t.Fatalf("terminal orders should stop polling, got %q", got)
	}
}

func TestPlacedPageShowsPaymentIssueForUnpaidRazorpay(t *testing.T) {
	unpaid := []backend.Order{
		{ID: "order-1", Stage: backend.StageReceived, PaymentMethod: "razorpay", PaymentStatus: "due"},
	}
	if !placedHasPaymentIssue(unpaid) {
		t.Fatal("unpaid Razorpay order should surface a payment issue on the placed page")
	}

	paid := []backend.Order{
		{ID: "order-2", Stage: backend.StageReceived, PaymentMethod: "razorpay", PaymentStatus: "paid"},
	}
	if placedHasPaymentIssue(paid) {
		t.Fatal("paid Razorpay order must not surface a payment issue")
	}

	cod := []backend.Order{
		{ID: "order-3", Stage: backend.StageReceived, PaymentMethod: "cod", PaymentStatus: "due"},
	}
	if placedHasPaymentIssue(cod) {
		t.Fatal("cash-on-delivery order must not surface a payment issue")
	}

	mixed := append(append([]backend.Order{}, paid...), unpaid...)
	if !placedHasPaymentIssue(mixed) {
		t.Fatal("any unpaid Razorpay order in the set should surface a payment issue")
	}
}
