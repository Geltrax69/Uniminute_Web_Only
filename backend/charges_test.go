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

// A pasted, multi-line description arrives with CRLF line breaks from a
// multipart form, and must be accepted with plain "\n" breaks.
func TestDescriptionLineBreaks(t *testing.T) {
	in := "DESIGNED TO DELIGHT — iPhone 17.\r\nSMOOTHER. BRIGHTER. 15.93 cm (6.3″)\rDone’s"
	got := plainLines(in)
	if got != "DESIGNED TO DELIGHT — iPhone 17.\nSMOOTHER. BRIGHTER. 15.93 cm (6.3″)\nDone’s" {
		t.Fatalf("got %q", got)
	}
	if err := textLimit(got, "description", 5000, false); err != nil {
		t.Fatal(err)
	}
}
