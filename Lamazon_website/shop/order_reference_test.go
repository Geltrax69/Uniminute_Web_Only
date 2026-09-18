package shop

import (
	"regexp"
	"testing"
)

func TestOrderRefIsStableAndNonSequential(t *testing.T) {
	ref := OrderRef("order-7")
	if ref != OrderRef("order-7") {
		t.Fatal("the same order changed reference")
	}
	if !regexp.MustCompile(`^UM-[2-9A-HJ-NP-Z]{8}$`).MatchString(ref) {
		t.Fatalf("unexpected public order reference %q", ref)
	}
	if ref == OrderRef("order-8") || ref == "UM-00000007" {
		t.Fatal("order references reveal the database sequence")
	}
}
