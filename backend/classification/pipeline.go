// Package classification derives customer-facing groups without overwriting
// merchant categories, inventory IDs, prices, or titles.
package classification

import (
	"strings"
	"unicode"
)

const Version = "2026-10-20.1"

type Result struct {
	Category    string `json:"category"`
	Group       string `json:"group"`
	Order       int    `json:"order"`
	Rule        string `json:"rule,omitempty"`
	Status      string `json:"status"`
	NeedsReview bool   `json:"needsReview"`
	Reason      string `json:"reason"`
	Version     string `json:"version"`
}

type rule struct {
	category        string
	departments     []string
	group           string
	order, priority int
	terms, exclude  []string
}

func makeRule(departments, group string, order, priority int, terms, exclude string) rule {
	return rule{departments: strings.Split(departments, "|"), group: group, order: order, priority: priority, terms: strings.Split(terms, "|"), exclude: strings.Split(exclude, "|")}
}

func (r rule) leaf(category string) rule { r.category = category; return r }

// Specific product types precede related broad types. Boundaries are whole
// words/phrases: tea cannot match steak; a phone case outranks the phone model.
var rules = []rule{
	makeRule("Food", "Combo meals", 90, 200, "combo|with fries|with a drink", ""),
	makeRule("Food", "Burger", 10, 100, "burger|burgers", ""),
	makeRule("Food", "Sandwich", 20, 100, "sandwich|sandwiches", ""),
	makeRule("Food", "Wraps", 30, 100, "wrap|wraps|shawarma", ""),
	makeRule("Food", "Street Food", 40, 140, "pav bhaji|vada pav|chaat|gol gappe|samosa|kachori", ""),
	makeRule("Food", "Pasta", 50, 100, "pasta|spaghetti|penne", ""),
	makeRule("Food", "Fries", 60, 100, "fries|french fries|loaded fries", ""),
	makeRule("Food", "Pizza", 65, 100, "pizza", ""),
	makeRule("Food", "Breakfast", 70, 100, "breakfast|omelette|pancake|poori|paratha", ""),
	makeRule("Food", "Main Course", 75, 100, "curry|dal makhani|paneer butter masala|kadhi", ""),
	makeRule("Food", "Rice", 80, 110, "rice|biryani|pulao", ""),
	makeRule("Food", "Hot Beverages", 100, 80, "tea", "iced tea|ice tea").leaf("Tea"),
	makeRule("Food", "Hot Beverages", 100, 80, "coffee", "").leaf("Coffee"),
	makeRule("Food", "Hot Beverages", 100, 120, "hot chocolate", "").leaf("Hot Chocolate"),
	makeRule("Food", "Hot Beverages", 100, 120, "herbal tea|chamomile|lavender|butterfly blue pea", "").leaf("Herbal Tea"),
	makeRule("Food", "Cold Coffee", 110, 160, "cold coffee|iced coffee|cold brew", ""),
	makeRule("Food", "Frappes", 120, 170, "frappe|frappes|frappuccino", ""),
	makeRule("Food", "Milk Shakes", 130, 160, "milkshake|milkshakes|milk shake|milk shakes", ""),
	makeRule("Food", "Mocktails", 140, 160, "mocktail|mocktails|mojito", ""),
	makeRule("Food", "Lassi", 145, 140, "lassi", ""),
	makeRule("Food", "Cold Drinks", 150, 130, "iced tea|ice tea|lemonade|soda|soft drink|soft drinks|juice", ""),
	makeRule("Food", "Desserts", 160, 140, "ice cream|brownie|gulab jamun|rasmalai|halwa|cheesecake", ""),
	makeRule("Electronics", "Mobile Accessories", 30, 200, "phone case|iphone case|phone cover|mobile cover|back cover|screen protector|tempered glass|phone stand|iphone & case|iphone & cover|phone & case|mobile & cover|galaxy & case", ""),
	makeRule("Electronics", "Computer Accessories", 40, 200, "laptop bag|laptop sleeve|laptop stand|keyboard cover|mouse pad|mousepad|laptop skin|laptop & bag|laptop & sleeve|macbook & sleeve|macbook & case", ""),
	makeRule("Electronics", "Chargers & Cables", 50, 180, "charger|charging cable|usb cable|hdmi cable|power adapter|power bank", ""),
	makeRule("Electronics", "Musical Instruments", 60, 190, "midi|musical keyboard|piano|guitar", ""),
	makeRule("Electronics", "Earphones", 70, 160, "earphones|earbuds|headphones|headset", ""),
	makeRule("Electronics", "Computer Accessories", 40, 130, "mouse|keyboard|webcam", ""),
	makeRule("Electronics", "Laptops", 20, 100, "laptop|macbook|notebook computer", ""),
	makeRule("Electronics", "Mobile", 10, 100, "iphone|smartphone|mobile phone|pixel phone|samsung galaxy", ""),
	makeRule("Electronics", "Batteries", 80, 110, "battery|batteries", ""),
	makeRule("Grocery & Kitchen|Grocery", "Kitchenware & Appliances", 60, 180, "rice cooker|coffee maker|coffee machine|tea kettle|electric kettle|mixer grinder", ""),
	makeRule("Grocery & Kitchen|Grocery", "Atta, Rice & Dal", 20, 100, "rice|atta|flour|dal|lentils", ""),
	makeRule("Grocery & Kitchen|Grocery", "Dairy, Bread & Eggs", 30, 100, "milk|bread|eggs|butter|paneer", ""),
	makeRule("Grocery & Kitchen|Grocery", "Oil, Ghee & Masala", 40, 100, "oil|ghee|masala|spice", ""),
	makeRule("Snacks & Drinks", "Tea, Coffee & Milk Drinks", 30, 100, "tea|coffee|milk drink", ""),
	makeRule("Snacks & Drinks", "Chips & Namkeen", 10, 100, "chips|namkeen|crisps", ""),
	makeRule("Snacks & Drinks", "Sweets & Chocolates", 20, 100, "chocolate|chocolates|candy|sweets", ""),
	makeRule("Snacks & Drinks", "Ice Creams & More", 40, 160, "ice cream|ice creams", ""),
	makeRule("Beauty", "Hair", 20, 170, "hair serum|hair oil|shampoo|conditioner|hair mask|hair colour", ""),
	makeRule("Beauty", "Skin & Face", 10, 160, "face serum|face wash|face cream|sunscreen|moisturizer|moisturiser", ""),
	makeRule("Beauty", "Bath & Body", 30, 120, "body wash|body lotion|soap|shower gel", ""),
	makeRule("Beauty", "Beauty & Cosmetics", 40, 120, "lipstick|mascara|foundation|eyeliner", ""),
	makeRule("Household Essentials", "Furniture", 10, 180, "coffee table|dining table|office chair|sofa|bookshelf", ""),
	makeRule("Stationery & Games", "Notebooks & Diaries", 10, 120, "notebook|notebooks|diary|diaries", ""),
	makeRule("Stationery & Games", "Pens & Pencils", 20, 120, "pen|pens|pencil|pencils|marker", ""),
	makeRule("Stationery & Games", "Toys & Games", 30, 120, "board game|puzzle|toy|toys", ""),
	makeRule("Stationery & Games", "Bags & School Needs", 40, 150, "school bag|backpack|pencil case", ""),
}

func Normalize(value string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)), " ")
}

func contains(text, phrase string) bool {
	phrase = Normalize(phrase)
	return phrase != "" && strings.Contains(" "+text+" ", " "+phrase+" ")
}
func matchesTerm(text, term string) bool {
	for _, part := range strings.Split(term, "&") {
		if !contains(text, part) {
			return false
		}
	}
	return true
}

func inDepartment(r rule, dept string) bool {
	for _, d := range r.departments {
		if Normalize(d) == dept {
			return true
		}
	}
	return false
}

func categoryGroup(department, category string) (string, int, bool) {
	dept, cat := Normalize(department), Normalize(category)
	if dept == "food" {
		switch cat {
		case "tea", "coffee", "herbal tea", "hot chocolate", "hot drinks", "hot beverages":
			return "Hot Beverages", 100, true
		case "wrap", "wraps":
			return "Wraps", 30, true
		case "burger", "burgers":
			return "Burger", 10, true
		case "sandwich", "sandwiches":
			return "Sandwich", 20, true
		case "pav bhaji", "street food", "chaat", "samosa":
			return "Street Food", 40, true
		}
	}
	for _, r := range rules {
		if inDepartment(r, dept) && Normalize(r.group) == cat {
			return r.group, r.order, true
		}
	}
	if cat == "" || cat == dept || cat == "all" || cat == "other" || cat == "other items" || cat == "beverages" || cat == "fast food" {
		return "Other items", 900, false
	}
	return strings.TrimSpace(category), 500, true
}

// Classify is pure and reproducible. The merchant category remains the
// fallback for branded/ambiguous names, and missing evidence goes to review.
func Classify(department, category, title string) Result {
	group, order, specific := categoryGroup(department, category)
	out := Result{Category: strings.TrimSpace(category), Group: group, Order: order, Status: "category", Reason: "Using the merchant category", Version: Version}
	dept, text := Normalize(department), Normalize(title)
	score := -1
	matches := map[string]rule{}
	for _, r := range rules {
		if !inDepartment(r, dept) {
			continue
		}
		excluded := false
		for _, term := range r.exclude {
			if contains(text, term) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		hit := false
		for _, term := range r.terms {
			if matchesTerm(text, term) {
				hit = true
				break
			}
		}
		if !hit || r.priority < score {
			continue
		}
		if r.priority > score {
			matches = map[string]rule{}
			score = r.priority
		}
		matches[r.group] = r
	}
	// If a strong non-food product type contradicts the assigned department,
	// send it to review instead of treating a coffee table as a drink.
	for _, r := range rules {
		if inDepartment(r, dept) || r.priority <= score || r.priority < 170 {
			continue
		}
		foreign := Normalize(r.departments[0])
		if foreign == "food" || foreign == "snacks drinks" {
			continue
		}
		excluded := false
		for _, term := range r.exclude {
			if contains(text, term) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		for _, term := range r.terms {
			if matchesTerm(text, term) {
				out.Status, out.NeedsReview, out.Reason = "review", true, "Product type conflicts with its department; check the assigned category"
				return out
			}
		}
	}
	if len(matches) == 1 {
		for _, r := range matches {
			out.Group, out.Order, out.Rule, out.Status = r.group, r.order, Normalize(department)+":"+Normalize(r.group), "matched"
			out.Reason = "Matched the product type within its department"
			out.Category = r.category
			if out.Category == "" {
				out.Category = r.group
			}
		}
	} else if len(matches) > 1 {
		out.Status, out.NeedsReview, out.Reason = "review", true, "Multiple product types matched; kept the merchant category"
	} else if !specific {
		out.Status, out.NeedsReview, out.Reason = "review", true, "Choose a specific product category; the name is not clear enough to classify"
	}
	return out
}
