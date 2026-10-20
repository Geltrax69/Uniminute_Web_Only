package pages

import (
	"lamazon/website/backend"
	"testing"
)

func TestStoreMenuGroupsEveryProductWithoutLosingOrder(t *testing.T) {
	products := []backend.Product{
		{ID: "1", Category: "Burger"}, {ID: "2", Category: "Sandwich"},
		{ID: "3", Category: " burger "}, {ID: "4", Category: ""}, {ID: "5", Category: "  "},
	}
	groups := GroupStoreProducts(products)
	if len(groups) != 3 {
		t.Fatalf("groups=%+v", groups)
	}
	if groups[0].Name != "Burger" || len(groups[0].Products) != 2 || groups[0].Products[1].ID != "3" {
		t.Fatalf("burger group=%+v", groups[0])
	}
	if groups[1].Name != "Sandwich" || groups[2].Name != "Other items" || len(groups[2].Products) != 2 {
		t.Fatalf("groups=%+v", groups)
	}
	total := 0
	seen := map[string]bool{}
	for _, g := range groups {
		total += len(g.Products)
		if seen[g.ID] {
			t.Fatal("duplicate section ID")
		}
		seen[g.ID] = true
	}
	if total != len(products) {
		t.Fatal("products lost from menu")
	}
	if got := MatchStoreCategory(groups, " BURGER "); got != "Burger" {
		t.Fatalf("category=%q", got)
	}
	if got := MatchStoreCategory(groups, "Unknown"); got != "" {
		t.Fatalf("unknown category should fall back to all: %q", got)
	}
	if got := storeMenuCount(StorePageData{Products: products, Sections: groups, SelectedCategory: "Burger"}); got != 2 {
		t.Fatalf("filtered count=%d", got)
	}
	if got := storeMenuCount(StorePageData{Products: products, Sections: groups}); got != 5 {
		t.Fatalf("all count=%d", got)
	}

	if len(GroupStoreProducts(nil)) != 0 {
		t.Fatal("empty catalogue created menu entries")
	}
}

func TestStoreMenuUsesPipelineGroupsAndSemanticOrder(t *testing.T) {
	products := []backend.Product{
		{ID: "coffee", Category: "Food", Classification: backend.ProductClassification{Group: "Hot Beverages", Order: 100, Version: "test"}},
		{ID: "burger", Category: "Burger", Classification: backend.ProductClassification{Group: "Burger", Order: 10, Version: "test"}},
		{ID: "tea", Category: "Food", Classification: backend.ProductClassification{Group: "Hot Beverages", Order: 100, Version: "test"}},
	}
	groups := GroupStoreProducts(products)
	if len(groups) != 2 || groups[0].Name != "Burger" || groups[1].Name != "Hot Beverages" || len(groups[1].Products) != 2 {
		t.Fatalf("pipeline grouping=%+v", groups)
	}
	if products[0].Category != "Food" {
		t.Fatal("grouping changed merchant categories")
	}
	if MatchStoreCategory(groups, "Food") != "Hot Beverages" {
		t.Fatal("old category link lost its replacement group")
	}
}

func TestStoreSearchCountsAndCategoryLinks(t *testing.T) {
	products := []backend.Product{
		{Name: "Aloo Cheese Burger", Category: "Burger"},
		{Name: "Paneer Burger", Category: "Burger"},
		{Name: "Cheese Sandwich", Category: "Sandwich"},
	}
	d := StorePageData{Products: products, Sections: GroupStoreProducts(products), Query: "CHEESE"}
	if got := storeMenuCount(d); got != 2 {
		t.Fatalf("shop matches=%d", got)
	}
	d.SelectedCategory = "Burger"
	if got := storeMenuCount(d); got != 1 {
		t.Fatalf("category matches=%d", got)
	}
	if got := storeAllCount(d); got != 2 {
		t.Fatalf("all-items count inherited category filter: %d", got)
	}
	if got := storeCategoryURL("Burger", "cheese & corn"); got != "?category=Burger&q=cheese+%26+corn#store-menu-results" {
		t.Fatalf("category link dropped or misencoded query: %q", got)
	}
}
