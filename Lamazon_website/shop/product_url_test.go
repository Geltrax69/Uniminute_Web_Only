package shop

import (
	"lamazon/website/backend"
	"testing"
)

func TestProductPathPreservesVendorAndChoices(t *testing.T) {
	for _, tc := range []struct {
		p    backend.Product
		want string
	}{
		{backend.Product{ID: "item-44", PublicID: "f8aabc123"}, "/p/f8aabc123"},
		{backend.Product{ID: "item-44"}, "/p/item-44"},
		{backend.Product{ID: "item-44@Other Shop?Colour=Black", PublicID: "f8aabc123"}, "/p/f8aabc123@Other%20Shop?Colour=Black"},
	} {
		if got := ProductPath(tc.p); got != tc.want {
			t.Errorf("ProductPath(%+v)=%q, want %q", tc.p, got, tc.want)
		}
	}
}
