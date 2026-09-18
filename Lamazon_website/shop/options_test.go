package shop

import (
	"testing"

	"lamazon/website/backend"
)

func TestLineIDRoundTrip(t *testing.T) {
	picked := []backend.Choice{{Name: "Storage", Value: "256GB"}, {Name: "Colour", Value: "Light Blue|#A7C7E7"}}
	id := LineID("item-44", picked)
	base, got := SplitLine(id)
	if base != "item-44" || len(got) != 2 || got[0] != (backend.Choice{Name: "Colour", Value: "Light Blue|#A7C7E7"}) {
		t.Fatalf("%q -> %q %v", id, base, got)
	}
	if ChoicesText(got) != "Colour: Light Blue · Storage: 256GB" {
		t.Fatalf("text %q", ChoicesText(got))
	}
	if ChoiceLabel("#1A1A1A") != "Black" || ChoiceLabel("64 GB") != "64 GB" {
		t.Fatal("labels")
	}
	if b, cs := SplitLine("item-7@Shop"); b != "item-7@Shop" || cs != nil {
		t.Fatal("plain id")
	}
}
