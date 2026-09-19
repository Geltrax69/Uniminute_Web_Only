package main

import (
	"encoding/json"
	"fmt"
)

func variantPricesJSON(in []VariantPrice) []byte {
	if in == nil {
		return []byte("[]")
	}
	b, _ := json.Marshal(in)
	return b
}

// validateVariantPrices requires exactly one price for every offered combination.
// Older listings without a matrix still use their ordinary product price.
func validateVariantPrices(in *InventoryItem) error {
	if len(in.VariantPrices) == 0 {
		return nil
	}
	groups := make([]ItemOption, 0, len(in.Options))
	for _, o := range in.Options {
		if len(o.Values) == 0 {
			continue
		}
		groups = append(groups, o)
	}
	if len(groups) == 0 {
		return fmt.Errorf("add option choices before setting combination prices")
	}
	want := 1
	groupNames := map[string]bool{}
	for _, o := range groups {
		if o.Name == "" {
			return fmt.Errorf("option names cannot be empty")
		}
		if groupNames[o.Name] {
			return fmt.Errorf("option names must be unique")
		}
		groupNames[o.Name] = true
		values := map[string]bool{}
		for _, value := range o.Values {
			if value == "" || values[value] {
				return fmt.Errorf("choices within %s must be unique and nonempty", o.Name)
			}
			values[value] = true
		}
		if want > 100/len(o.Values) {
			return fmt.Errorf("use at most 100 option combinations")
		}
		want *= len(o.Values)
	}
	if len(in.VariantPrices) != want {
		return fmt.Errorf("set a price for all %d option combinations", want)
	}
	seen := map[string]bool{}
	minimum := 999999.99
	for i, v := range in.VariantPrices {
		if !isFinitePrice(v.Price) || v.Price <= 0 {
			return fmt.Errorf("each combination price must be between ₹0.01 and ₹999999.99")
		}
		if in.MRP > 0 && v.Price > in.MRP {
			return fmt.Errorf("MRP cannot be below a combination price")
		}
		choices, err := matchChoices(in.Title, groups, v.Choices)
		if err != nil {
			return err
		}
		key := choices.key()
		if seen[key] {
			return fmt.Errorf("duplicate option combination")
		}
		seen[key] = true
		in.VariantPrices[i].Choices = choices
		if v.Price < minimum {
			minimum = v.Price
		}
	}
	in.Price = minimum // catalogue cards truthfully show the starting price
	return nil
}

func priceForChoices(base float64, variants []VariantPrice, choices Choices) (float64, error) {
	if len(variants) == 0 {
		return base, nil
	}
	for _, v := range variants {
		if v.Choices.key() == choices.key() {
			return v.Price, nil
		}
	}
	return 0, fmt.Errorf("the selected option combination is no longer available")
}
