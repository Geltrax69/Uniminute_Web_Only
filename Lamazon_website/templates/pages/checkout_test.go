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
