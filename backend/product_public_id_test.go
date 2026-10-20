package main

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
)

func TestProductPublicIDsAreDurableAndResolvable(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	products, err := db.products(ctx, productFilter{})
	if err != nil || len(products) == 0 {
		t.Fatalf("catalogue: %v, %d rows", err, len(products))
	}
	seen := map[string]bool{}
	shape := regexp.MustCompile(`^[0-9a-f]{32}$`)
	for _, p := range products {
		if !shape.MatchString(p.PublicID) || seen[p.PublicID] {
			t.Fatalf("invalid or repeated public reference %q", p.PublicID)
		}
		seen[p.PublicID] = true
		byPublic, err := db.product(ctx, p.PublicID)
		if err != nil || byPublic.ID != p.ID {
			t.Fatalf("public lookup for %s: %+v, %v", p.ID, byPublic, err)
		}
	}
	first := products[0]
	if _, err := db.sql.ExecContext(ctx, schema); err != nil {
		t.Fatal(err)
	}
	after, err := db.product(ctx, first.ID)
	if err != nil || after.PublicID != first.PublicID {
		t.Fatalf("schema restart changed public reference: %+v, %v", after, err)
	}
	if _, err := db.product(ctx, "00000000000000000000000000000000"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown reference resolved: %v", err)
	}
}

func TestInventoryGetsPublicReferenceWithoutChangingInternalID(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := db.sql.ExecContext(ctx, `INSERT INTO seller_stores(owner,name,location,city,status) VALUES($1,'Opaque Test','Test street','Test city','approved')`, DefaultOwner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO inventory_items(id,owner,title,price,stock) VALUES('item-public-test',$1,'Public reference test',100,3)`, DefaultOwner)
	if err != nil {
		t.Fatal(err)
	}
	product, err := db.product(ctx, "item-public-test")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := db.product(ctx, product.PublicID)
	if err != nil || resolved.ID != "item-public-test" || resolved.PublicID == "" {
		t.Fatalf("inventory public lookup: %+v, %v", resolved, err)
	}
}
