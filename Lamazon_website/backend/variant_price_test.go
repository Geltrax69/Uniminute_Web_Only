package backend

import "testing"

func TestProductPriceForSelections(t *testing.T) {
	p := Product{Price: 45000, VariantPrices: []VariantPrice{
		{Choices: []Choice{{Name: "Colour", Value: "Pink"}, {Name: "Storage", Value: "64GB"}}, Price: 45000},
		{Choices: []Choice{{Name: "Colour", Value: "Pink"}, {Name: "Storage", Value: "128GB"}}, Price: 80000},
		{Choices: []Choice{{Name: "Colour", Value: "Blue"}, {Name: "Storage", Value: "64GB"}}, Price: 47000},
		{Choices: []Choice{{Name: "Colour", Value: "Blue"}, {Name: "Storage", Value: "128GB"}}, Price: 82000},
	}}
	for _, tc := range []struct {
		colour, storage string
		price           float64
	}{
		{"Pink", "64GB", 45000}, {"Pink", "128GB", 80000},
		{"Blue", "64GB", 47000}, {"Blue", "128GB", 82000},
	} {
		got, ok := p.PriceFor([]Choice{{Name: "Storage", Value: tc.storage}, {Name: "Colour", Value: tc.colour}})
		if !ok || got != tc.price {
			t.Errorf("%s %s: got %.2f, ok=%v", tc.colour, tc.storage, got, ok)
		}
	}
	if _, ok := p.PriceFor([]Choice{{Name: "Colour", Value: "Green"}, {Name: "Storage", Value: "64GB"}}); ok {
		t.Fatal("unavailable combination accepted")
	}
	p.VariantPrices = nil
	if got, ok := p.PriceFor(nil); !ok || got != 45000 {
		t.Fatal("legacy single price changed")
	}
}
