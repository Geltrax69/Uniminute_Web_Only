package main

import (
	"context"
	"net/http"
	"testing"
)

func TestClassificationEndpointsAreScopedAndPreviewMatchesCatalogue(t *testing.T) {
	h := testAPI(t)
	if code, _ := callAs(t, h, "", http.MethodPost, "/api/seller/classify", map[string]any{"title": "Tea", "category": "Food"}); code != 401 {
		t.Fatalf("anonymous preview=%d", code)
	}
	code, preview := call(t, h, http.MethodPost, "/api/seller/classify", map[string]any{"title": "Tea", "category": "Food"})
	if code != 200 || preview["group"] != "Hot Beverages" {
		t.Fatalf("preview=%d %+v", code, preview)
	}
	if code, _ := call(t, h, http.MethodPost, "/api/seller/classify", map[string]any{"title": "Tea", "category": "missing-category"}); code != 400 {
		t.Fatalf("invalid category=%d", code)
	}
	if code, _ := call(t, h, http.MethodGet, "/api/admin/catalog-audit", nil); code != 401 && code != 403 {
		t.Fatalf("seller accessed admin audit: %d", code)
	}
	admin := adminSignIn(t, h)
	code, report := callAs(t, h, admin, http.MethodGet, "/api/admin/catalog-audit", nil)
	if code != 200 || report["version"] == "" {
		t.Fatalf("admin audit=%d %+v", code, report)
	}
}

func TestClassificationRecomputesAfterEditingWithoutChangingSourceCategory(t *testing.T) {
	db := testDB(t)
	_, err := db.sql.Exec(`INSERT INTO seller_stores(owner,name,location,city,status) VALUES($1,'Classification Test','Test','Test','approved')`, DefaultOwner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.Exec(`INSERT INTO inventory_items(id,owner,title,category,price,stock) VALUES('classification-tea',$1,'Tea','Food',50,3)`, DefaultOwner)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.product(context.Background(), "classification-tea")
	if err != nil || p.Classification.Group != "Hot Beverages" || p.Category != "Food" {
		t.Fatalf("product=%+v %v", p, err)
	}
	rows, err := db.products(context.Background(), productFilter{ID: "classification-tea", Category: "Tea"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("inferred tea category filter=%+v %v", rows, err)
	}
	if _, err := db.sql.Exec(`UPDATE inventory_items SET title='Cold Coffee' WHERE id='classification-tea'`); err != nil {
		t.Fatal(err)
	}
	p, err = db.product(context.Background(), "classification-tea")
	if err != nil || p.Classification.Group != "Cold Coffee" || p.Category != "Food" {
		t.Fatalf("updated=%+v %v", p, err)
	}
}

func TestCatalogueCategoryFiltersUsePipelineForRelatedProducts(t *testing.T) {
	db := testDB(t)
	_, err := db.sql.Exec(`INSERT INTO seller_stores(owner,name,location,city,status) VALUES($1,'Pipeline Filter','Test','Test','approved')`, DefaultOwner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.Exec(`INSERT INTO catalog_categories(name,parent) VALUES('Mobile','Electronics') ON CONFLICT(name) DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.Exec(`INSERT INTO inventory_items(id,owner,title,category,price,stock) VALUES
 ('pipeline-case',$1,'Pipeline iPhone 17 case','Mobile',100,3),
 ('pipeline-phone',$1,'Pipeline iPhone 17 phone','Mobile',1000,3)`, DefaultOwner)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ category, id string }{{"Mobile", "pipeline-phone"}, {"Mobile Accessories", "pipeline-case"}} {
		rows, err := db.products(context.Background(), productFilter{Category: tc.category, Q: "Pipeline"})
		if err != nil || len(rows) != 1 || rows[0].ID != tc.id {
			t.Fatalf("%s filter: %+v, %v", tc.category, rows, err)
		}
	}
}
