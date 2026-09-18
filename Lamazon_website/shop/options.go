package shop

import (
	"net/url"
	"regexp"
	"strings"

	"lamazon/website/backend"
)

// A cart line's id carries the buyer's choices after the product id, so the
// same phone in Black and in White are two lines: "item-44?Colour=%231A1A1A".

// LineID is the product id with the chosen options appended.
func LineID(productID string, picked []backend.Choice) string {
	if len(picked) == 0 {
		return productID
	}
	q := url.Values{}
	for _, c := range picked {
		q.Set(c.Name, c.Value)
	}
	return productID + "?" + q.Encode()
}

// SplitLine is the product id and the choices a line id carries, sorted by name.
func SplitLine(lineID string) (string, []backend.Choice) {
	base, query, ok := strings.Cut(lineID, "?")
	if !ok {
		return lineID, nil
	}
	q, _ := url.ParseQuery(query)
	var out []backend.Choice
	for name := range q {
		out = append(out, backend.Choice{Name: name, Value: q.Get(name)})
	}
	sortChoices(out)
	return base, out
}

func sortChoices(cs []backend.Choice) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].Name < cs[j-1].Name; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

var colourValue = regexp.MustCompile(`^(.+\|)?#[0-9A-Fa-f]{6}$`)

// ChoiceLabel is how a picked value reads: a colour by its name, anything else as is.
func ChoiceLabel(v string) string {
	if colourValue.MatchString(v) {
		name, _ := ColourParts(v)
		return name
	}
	return v
}

// ChoicesText is "Colour: Black · Storage: 256GB", or "" with no choices.
func ChoicesText(cs []backend.Choice) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.Name+": "+ChoiceLabel(c.Value))
	}
	return strings.Join(parts, " · ")
}
