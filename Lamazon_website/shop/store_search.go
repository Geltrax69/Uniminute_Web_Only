package shop

import (
	"lamazon/website/backend"
	"strings"
)

// StoreSearchText keeps shop search focused on the item and its categories.
func StoreSearchText(p backend.Product) string {
	return p.Name + " " + p.Category + " " + p.Classification.Group
}

func StoreSearchMatches(p backend.Product, query string) bool {
	text := strings.ToLower(StoreSearchText(p))
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}
