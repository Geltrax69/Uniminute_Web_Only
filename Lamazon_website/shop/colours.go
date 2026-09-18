package shop

import "strings"

// Swatch is one preset colour in the seller's colour picker.
type Swatch struct {
	Name string `json:"name"`
	Hex  string `json:"hex"`
}

// Swatches are the one-tap colours. Anything else is saved as "Name|#hex".
var Swatches = []Swatch{
	{"Black", "#1A1A1A"}, {"White", "#FFFFFF"}, {"Grey", "#9E9E9E"}, {"Silver", "#C9CCD1"},
	{"Titanium", "#8A8580"}, {"Red", "#D32F2F"}, {"Pink", "#F06292"}, {"Orange", "#FF8A3D"},
	{"Yellow", "#FBC02D"}, {"Green", "#43A047"}, {"Light Blue", "#A7C7E7"}, {"Blue", "#2F6FED"},
	{"Navy", "#1A237E"}, {"Lavender", "#C3B1E1"}, {"Purple", "#9C6ADE"}, {"Light Brown", "#B08D72"},
	{"Brown", "#6D4C41"}, {"Beige", "#D7CCC8"}, {"Golden White", "#F3E9D2"}, {"Gold", "#C9A227"},
}

// ColourParts reads a colour option value: "Light Blue|#A7C7E7" (custom), or a
// bare hex (preset, older listings). Name falls back to the preset's name, then
// to the value itself; hex falls back to black.
func ColourParts(v string) (name, hex string) {
	name, hex, custom := strings.Cut(v, "|")
	if !custom {
		name, hex = "", v
	}
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) != 6 || strings.Trim(strings.ToLower(h), "0123456789abcdef") != "" {
		h = "000000"
	}
	hex = "#" + strings.ToUpper(h)
	if name = strings.TrimSpace(name); name == "" {
		name = v
		for _, s := range Swatches {
			if strings.EqualFold(s.Hex, hex) {
				name = s.Name
			}
		}
	}
	return name, hex
}
