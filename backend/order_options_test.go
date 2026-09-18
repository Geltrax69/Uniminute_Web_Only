package main

import "testing"

func TestMatchChoices(t *testing.T) {
	offered := []ItemOption{
		{Name: "Storage", Values: []string{"256GB", "1TB"}},
		{Name: "Colour", Kind: "colour", Values: []string{"#1A1A1A", "Light Blue|#A7C7E7"}},
	}
	got, err := matchChoices("iPhone", offered, Choices{{"Colour", "Light Blue|#A7C7E7"}, {"Storage", "1TB"}})
	if err != nil || len(got) != 2 || got[0] != (Choice{"Storage", "1TB"}) {
		t.Fatalf("valid picks: %v %v", got, err)
	}
	for name, picked := range map[string]Choices{
		"missing colour": {{"Storage", "1TB"}},
		"not offered":    {{"Storage", "2TB"}, {"Colour", "#1A1A1A"}},
		"unknown option": {{"Storage", "1TB"}, {"Colour", "#1A1A1A"}, {"Size", "XL"}},
	} {
		if _, err := matchChoices("iPhone", offered, picked); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got, err := matchChoices("Burger", nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("no options: %v %v", got, err)
	}
}
