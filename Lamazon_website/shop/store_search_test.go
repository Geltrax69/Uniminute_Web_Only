package shop

import (
	"lamazon/website/backend"
	"testing"
)

func TestStoreSearchMatchesNameAndBothCategoryLabels(t *testing.T) {
	p := backend.Product{Name: "Aloo Cheese Burger", Category: "Quick Bites", Classification: backend.ProductClassification{Group: "Burger"}}
	for _, query := range []string{"", "   ", "CHEESE", "  cheese   aloo ", "quick burger"} {
		if !StoreSearchMatches(p, query) {
			t.Errorf("expected match for %q", query)
		}
	}
	for _, query := range []string{"coffee", "cheese pasta", "<script>"} {
		if StoreSearchMatches(p, query) {
			t.Errorf("unexpected match for %q", query)
		}
	}
}
