package main

import "testing"

func TestChargeRules(t *testing.T) {
	d := Charge{ID: deliveryChargeID, Name: "Delivery", Amount: 20}
	if msg := validCharges([]Charge{d, {Name: "Packaging", Amount: 5}}); msg != "" {
		t.Fatalf("valid list refused: %s", msg)
	}
	for name, list := range map[string][]Charge{
		"no delivery":  {{Name: "Packaging", Amount: 5}},
		"negative":     {d, {Name: "Packaging", Amount: -1}},
		"blank name":   {d, {Name: " ", Amount: 1}},
		"duplicate":    {d, {Name: "delivery", Amount: 1}},
		"two delivery": {d, d},
	} {
		if validCharges(list) == "" {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := chargesTotal([]Charge{d, {Amount: 4.995}}); got != 25 {
		t.Errorf("total %v, want 25", got)
	}
}
