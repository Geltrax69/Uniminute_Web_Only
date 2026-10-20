package classification

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDepartmentAndSpecificityPipeline(t *testing.T) {
	for _, tc := range []struct {
		dept, category, title, group string
		review                       bool
	}{
		{"Food", "Food", "Tea", "Hot Beverages", false},
		{"Food", "Food", "Coffee", "Hot Beverages", false},
		{"Food", "Beverages", "Hot Chocolate", "Hot Beverages", false},
		{"Food", "Beverages", "Green Tea", "Hot Beverages", false},
		{"Food", "Herbal Tea", "Chamomile Ta", "Hot Beverages", false},
		{"Food", "Breakfast", "Pav Bhaji", "Street Food", false},
		{"Food", "Food", "Regular Cold Coffee", "Cold Coffee", false},
		{"Food", "Food", "Coffee Milkshake", "Milk Shakes", false},
		{"Food", "Frappes", "Biscoff", "Frappes", false},
		{"Food", "Sandwich", "Mac & Cheese Sandwich", "Sandwich", false},
		{"Food", "Burger", "Burger and Pasta", "Burger", true},
		{"Food", "Burger", "Burger with Fries", "Combo meals", false},
		{"Food", "Food", "Steak special", "Other items", true},
		{"Food", "Food", "Mystery special", "Other items", true},
		{"Food", "Food", "Coffee table", "Other items", true},
		{"Electronics", "Mobile", "iPhone 17 case", "Mobile Accessories", false},
		{"Electronics", "Laptops", "MacBook laptop sleeve", "Computer Accessories", false},
		{"Electronics", "Mobile", "iPhone USB charger", "Chargers & Cables", false},
		{"Electronics", "Electronics", "MIDI Keyboard Controller", "Musical Instruments", false},
		{"Electronics", "Electronics", "Logitech Gaming Mouse USB-C Charging", "Computer Accessories", false},
		{"Electronics", "Electronics", "Apple iPhone 17 256 GB", "Mobile", false},
		{"Electronics", "Electronics", "Dell Thin and Light Laptop 15", "Laptops", false},
		{"Grocery & Kitchen", "Grocery & Kitchen", "Rice Cooker", "Kitchenware & Appliances", false},
		{"Grocery & Kitchen", "Grocery & Kitchen", "Coffee maker", "Kitchenware & Appliances", false},
		{"Grocery & Kitchen", "Grocery & Kitchen", "Basmati Rice 5 kg", "Atta, Rice & Dal", false},
		{"Snacks & Drinks", "Tea, Coffee & Milk Drinks", "Instant coffee 200g", "Tea, Coffee & Milk Drinks", false},
		{"Household Essentials", "Home & Lifestyle", "Coffee Table", "Furniture", false},
		{"Beauty", "Skin & Face", "Hair serum", "Hair", false},
		{"Beauty", "Beauty", "Face serum", "Skin & Face", false},
		{"Stationery & Games", "Stationery & Games", "School backpack", "Bags & School Needs", false},
		{"Stationery & Games", "Notebooks & Diaries", "Notebook", "Notebooks & Diaries", false},
	} {
		t.Run(tc.title+"/"+tc.dept, func(t *testing.T) {
			got := Classify(tc.dept, tc.category, tc.title)
			if got.Group != tc.group || got.NeedsReview != tc.review {
				t.Fatalf("got %+v, want group=%s review=%v", got, tc.group, tc.review)
			}
			if got.Version != Version {
				t.Fatal("unversioned result")
			}
		})
	}
}

func TestPureBitesCatalogueRegression(t *testing.T) {
	raw, err := os.ReadFile("testdata/pure_bites.json")
	if err != nil {
		t.Fatal(err)
	}
	var products []Input
	if err := json.Unmarshal(raw, &products); err != nil {
		t.Fatal(err)
	}
	report := Audit(products)
	expected := map[string]int{"Burger": 4, "Sandwich": 6, "Wraps": 2, "Street Food": 1, "Pasta": 4, "Fries": 4, "Hot Beverages": 7, "Cold Coffee": 5, "Frappes": 7}
	if report.Products != 40 || len(report.Review) != 0 || len(report.Groups) != len(expected) {
		t.Fatalf("unexpected report: %+v", report)
	}
	for name, count := range expected {
		if report.Groups["Food / "+name] != count {
			t.Fatalf("%s=%d, want %d", name, report.Groups["Food / "+name], count)
		}
	}
	if len(report.Duplicates) != 1 || len(report.Duplicates[0]) != 2 || report.Duplicates[0][0].Title != "Red Sauce Pasta" {
		t.Fatalf("duplicates=%+v", report.Duplicates)
	}
	// Auditing must leave the original categories and titles intact.
	for _, p := range products {
		if p.Title == "Pav Bhaji" && p.Category != "Breakfast" {
			t.Fatal("audit changed a source category")
		}
	}
}
