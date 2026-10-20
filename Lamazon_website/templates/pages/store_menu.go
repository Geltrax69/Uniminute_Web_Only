package pages

import (
	"lamazon/website/backend"
	"net/url"
	"strconv"
	"strings"
)

type StoreSection struct {
	Name     string
	ID       string
	Products []backend.Product
}

// Keep the catalogue's order within and between categories. Uncategorised
// listings remain discoverable instead of disappearing from the menu.
func GroupStoreProducts(products []backend.Product) []StoreSection {
	var sections []StoreSection
	index := map[string]int{}
	for _, p := range products {
		name := strings.TrimSpace(p.Category)
		if name == "" {
			name = "Other items"
		}
		key := strings.ToLower(name)
		i, ok := index[key]
		if !ok {
			i = len(sections)
			index[key] = i
			sections = append(sections, StoreSection{Name: name, ID: "store-category-" + strconv.Itoa(i+1)})
		}
		sections[i].Products = append(sections[i].Products, p)
	}
	return sections
}

func MatchStoreCategory(sections []StoreSection, category string) string {
	for _, section := range sections {
		if strings.EqualFold(section.Name, strings.TrimSpace(category)) {
			return section.Name
		}
	}
	return ""
}

func storeMenuConfig(d StorePageData) []map[string]any {
	out := make([]map[string]any, 0, len(d.Sections))
	for _, section := range d.Sections {
		out = append(out, map[string]any{"name": section.Name, "count": len(section.Products)})
	}
	return out
}

func storeCategoryURL(name string) string {
	if name == "" {
		return "?category=#store-menu-results"
	}
	return "?category=" + url.QueryEscape(name) + "#store-menu-results"
}

func storeMenuCount(d StorePageData) int {
	for _, section := range d.Sections {
		if section.Name == d.SelectedCategory {
			return len(section.Products)
		}
	}
	return len(d.Products)
}
