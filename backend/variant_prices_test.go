package main

import "testing"

func TestVariantCombinationPrices(t *testing.T) {
	item := InventoryItem{
		Title: "Phone", Price: 45000, Options: []ItemOption{
			{Name: "Colour", Values: []string{"Pink", "Blue"}},
			{Name: "Storage", Values: []string{"64GB", "128GB"}},
		},
		VariantPrices: []VariantPrice{
			{Choices: Choices{{"Colour", "Pink"}, {"Storage", "64GB"}}, Price: 45000},
			{Choices: Choices{{"Storage", "128GB"}, {"Colour", "Pink"}}, Price: 80000},
			{Choices: Choices{{"Colour", "Blue"}, {"Storage", "64GB"}}, Price: 47000},
			{Choices: Choices{{"Colour", "Blue"}, {"Storage", "128GB"}}, Price: 82000},
		},
	}
	if err := validateVariantPrices(&item); err != nil {
		t.Fatal(err)
	}
	if item.Price != 45000 {
		t.Fatalf("starting price: %v", item.Price)
	}
	for _, tc := range []struct {
		colour, storage string
		want            float64
	}{
		{"Pink", "64GB", 45000}, {"Pink", "128GB", 80000},
		{"Blue", "64GB", 47000}, {"Blue", "128GB", 82000},
	} {
		price, err := priceForChoices(item.Price, item.VariantPrices, Choices{{"Colour", tc.colour}, {"Storage", tc.storage}})
		if err != nil || price != tc.want {
			t.Errorf("%s/%s: got %v, %v", tc.colour, tc.storage, price, err)
		}
	}
	if _, err := priceForChoices(item.Price, item.VariantPrices, Choices{{"Colour", "Green"}, {"Storage", "64GB"}}); err == nil {
		t.Fatal("unknown combination accepted")
	}
	item.VariantPrices = item.VariantPrices[:3]
	if err := validateVariantPrices(&item); err == nil {
		t.Fatal("incomplete matrix accepted")
	}
	item.VariantPrices = append(item.VariantPrices, item.VariantPrices[0])
	if err := validateVariantPrices(&item); err == nil {
		t.Fatal("duplicate combination accepted")
	}
}

func TestThreeOptionCombinationPrices(t *testing.T) {
	item := InventoryItem{Title: "Shirt", Price: 100, Options: []ItemOption{
		{Name: "Colour", Values: []string{"Red", "Blue"}},
		{Name: "Size", Values: []string{"S", "M"}},
		{Name: "Material", Values: []string{"Cotton", "Linen"}},
	}}
	for _, colour := range []string{"Red", "Blue"} {
		for _, size := range []string{"S", "M"} {
			for _, material := range []string{"Cotton", "Linen"} {
				item.VariantPrices = append(item.VariantPrices, VariantPrice{Choices: Choices{{"Colour", colour}, {"Size", size}, {"Material", material}}, Price: 100 + float64(len(item.VariantPrices))})
			}
		}
	}
	if err := validateVariantPrices(&item); err != nil {
		t.Fatal(err)
	}
	if len(item.VariantPrices) != 8 {
		t.Fatal("expected eight combinations")
	}
}

// Each combination carries its own MRP, and the card's MRP follows the
// combination whose price the card shows.
func TestPerCombinationMRP(t *testing.T) {
	item := InventoryItem{
		Title: "Keyboard", Price: 8100, MRP: 14000,
		Options: []ItemOption{{Name: "Model", Values: []string{"N-25", "N-32"}}},
		VariantPrices: []VariantPrice{
			{Choices: Choices{{"Model", "N-25"}}, Price: 6027, MRP: 9000},
			{Choices: Choices{{"Model", "N-32"}}, Price: 11570, MRP: 16000},
		},
	}
	if err := validateVariantPrices(&item); err != nil {
		t.Fatal(err)
	}
	if item.Price != 6027 || item.MRP != 9000 {
		t.Fatalf("card shows %v off %v", item.Price, item.MRP)
	}
	item.VariantPrices[1].MRP = 11000 // below its own price
	if err := validateVariantPrices(&item); err == nil {
		t.Fatal("MRP below the combination price accepted")
	}
}
