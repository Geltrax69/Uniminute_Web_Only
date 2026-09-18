package shop

import "testing"

func TestColourParts(t *testing.T) {
	for in, want := range map[string][2]string{
		"Light Blue|#a7c7e7": {"Light Blue", "#A7C7E7"},
		"#2F6FED":            {"Blue", "#2F6FED"},
		"Sky|nothex":         {"Sky", "#000000"},
		"#123456":            {"#123456", "#123456"},
	} {
		if n, h := ColourParts(in); n != want[0] || h != want[1] {
			t.Errorf("%q -> %q %q, want %q %q", in, n, h, want[0], want[1])
		}
	}
}
