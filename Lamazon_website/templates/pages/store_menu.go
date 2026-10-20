package pages

import (
	"lamazon/website/backend"
	"lamazon/website/shop"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type StoreSection struct {
	Order    int
	Name     string
	ID       string
	Products []backend.Product
}

// Use the API classification pipeline for menu groups and ordering. Older
// APIs fall back to merchant categories. Every listing remains visible.
func GroupStoreProducts(products []backend.Product) []StoreSection {
	var sections []StoreSection
	index := map[string]int{}
	for _, p := range products {
		name := strings.TrimSpace(p.Category)
		order := 500
		if p.Classification.Version != "" && p.Classification.Group != "" {
			name = p.Classification.Group
			order = p.Classification.Order
		}
		if name == "" {
			name = "Other items"
		}
		key := strings.ToLower(name)
		i, ok := index[key]
		if !ok {
			i = len(sections)
			index[key] = i
			sections = append(sections, StoreSection{Name: name, Order: order})
		}
		sections[i].Products = append(sections[i].Products, p)
	}
	sort.SliceStable(sections, func(i, j int) bool { return sections[i].Order < sections[j].Order })
	for i := range sections {
		sections[i].ID = "store-category-" + strconv.Itoa(i+1)
	}
	return sections
}

func MatchStoreCategory(sections []StoreSection, category string) string {
	category = strings.TrimSpace(category)
	for _, section := range sections {
		if strings.EqualFold(section.Name, category) {
			return section.Name
		}
	}
	// An old menu URL may name a merchant category now grouped by the pipeline.
	// Resolve it only when every matching listing belongs to one new group.
	replacement := ""
	for _, section := range sections {
		for _, p := range section.Products {
			if category != "" && strings.EqualFold(strings.TrimSpace(p.Category), category) {
				if replacement != "" && replacement != section.Name {
					return ""
				}
				replacement = section.Name
			}
		}
	}
	return replacement
}

func storeMenuConfig(d StorePageData) []map[string]any {
	out := make([]map[string]any, 0, len(d.Sections))
	for _, section := range d.Sections {
		items := make([]string, 0, len(section.Products))
		for _, p := range section.Products {
			items = append(items, shop.StoreSearchText(p))
		}
		out = append(out, map[string]any{"name": section.Name, "count": len(section.Products), "items": items})
	}
	return out
}

func storeCategoryURL(name string, queries ...string) string {
	values := url.Values{"category": []string{name}}
	if len(queries) > 0 && strings.TrimSpace(queries[0]) != "" {
		values.Set("q", strings.TrimSpace(queries[0]))
	}
	return "?" + values.Encode() + "#store-menu-results"
}

func storeMenuCount(d StorePageData) int {
	for _, section := range d.Sections {
		if section.Name == d.SelectedCategory {
			return storeSectionCount(section, d.Query)
		}
	}
	return storeAllCount(d)
}

func storeAllCount(d StorePageData) int {
	count := 0
	for _, p := range d.Products {
		if shop.StoreSearchMatches(p, d.Query) {
			count++
		}
	}
	return count
}

func storeSectionCount(section StoreSection, query string) int {
	count := 0
	for _, p := range section.Products {
		if shop.StoreSearchMatches(p, query) {
			count++
		}
	}
	return count
}
