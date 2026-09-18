package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schema string

// DB is the Postgres-backed store. Same handlers, real storage.
type DB struct {
	sql *sql.DB
}

// OpenDB connects, applies the schema and seeds the catalog if it is empty.
// ponytail: schema on boot instead of a migration tool — the file is
// idempotent, and there is no live data to migrate yet.
func OpenDB(dsn string) (*DB, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	// Pool sized for concurrency: idle conns match open ones so a burst does
	// not pay a fresh handshake per request. ponytail: env override instead
	// of a config file — it is the only knob that varies per deploy.
	max := 25
	if n, err := strconv.Atoi(os.Getenv("DB_MAX_CONNS")); err == nil && n > 0 {
		max = n
	}
	conn.SetMaxOpenConns(max)
	conn.SetMaxIdleConns(max)
	conn.SetConnMaxIdleTime(5 * time.Minute)
	conn.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}

	db := &DB{sql: conn}
	if err := db.seedIfEmpty(ctx); err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}
	return db, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// seedIfEmpty loads the sample catalog the first time only, so restarts do
// not duplicate rows or undo edits.
//
// SKIP_SEED turns it off entirely. "Empty" and "deliberately emptied" look
// identical from here, so without it a wipe lasts exactly until the next
// boot puts the samples back.
func (d *DB) seedIfEmpty(ctx context.Context) error {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("SKIP_SEED"))); v != "" &&
		v != "0" && v != "false" {
		return nil
	}

	var n int
	if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM products`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	for _, p := range seedProducts() {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO products (id, name, category, tab, price, image_url, store, description)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			p.ID, p.Name, p.Category, p.Tab, p.Price, p.ImageURL, p.Store, p.Description); err != nil {
			return err
		}
		for _, o := range p.Offers {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO offers (product_id, store, price) VALUES ($1,$2,$3)`,
				p.ID, o.Store, o.Price); err != nil {
				return err
			}
		}
	}
	for _, s := range seedShops() {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO shops (name, tagline, image_url, tab) VALUES ($1,$2,$3,$4)`,
			s.Name, s.Tagline, s.ImageURL, s.Tab); err != nil {
			return err
		}
	}

	// The two campus restaurants go in as ordinary seller stores, through the
	// same tables a student's own store uses. They are proof the seller side
	// reaches shoppers, not a special case beside it.
	for _, store := range foodStores() {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO users (email, name) VALUES ($1,$2)
			ON CONFLICT (email) DO NOTHING`, store.Owner, store.Name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO seller_stores (owner, name, location, city, categories, status)
			VALUES ($1,$2,$3,$4,$5,'approved')`,
			store.Owner, store.Name, store.Location,
			ServiceableCities[0], store.Categories); err != nil {
			return err
		}
		for _, item := range store.Items {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO inventory_items (owner, title, description, category, price, stock)
				VALUES ($1,$2,$3,$4,$5,$6)`,
				store.Owner, item.Title, item.Section, item.Category,
				item.Price, 50); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// productFilter narrows the catalog. Empty fields are ignored, so the zero
// value means "everything".
type productFilter struct {
	Tab      string
	Category string
	Q        string // substring of name, category or store
	Store    string // lists the store owns, plus lists it stocks as an offer
	ID       string
}

// productQuery filters and attaches offers in one round trip. Postgres does
// the narrowing, so a search no longer ships the whole catalog through Go.
//
// The catalog is the seeded rows *and* what real sellers have in stock: a
// store's items are products to a shopper, and listing them anywhere else
// would mean a seller can add stock nobody can buy. Sold-out lines are left
// out rather than shown as unavailable.
const productQuery = `
	-- Which department a category belongs to, however deep it sits. A dish is
	-- filed under Chaat; the tab it has to appear on is Food, three levels up.
	WITH RECURSIVE tree AS (
		SELECT name, parent, name AS root
		FROM catalog_categories WHERE parent = ''
		UNION ALL
		SELECT c.name, c.parent, t.root
		FROM catalog_categories c JOIN tree t ON c.parent = t.name
	),
	catalogue AS (
		SELECT p.id, p.name, p.category, p.tab, p.price,
		       -- The seeded catalogue has no discounts, and a zero here is
		       -- what tells the app to show a plain price.
		       0::numeric AS mrp,
		       '[]'::jsonb AS options,
		       p.image_url, p.store,
		       p.description, p.image_url AS photos, NULL::int AS available_stock
		FROM products p
		UNION ALL
		SELECT i.id, i.title, COALESCE(NULLIF(i.category, ''), 'Food'),
		       -- The department, not the category. These used to be the same
		       -- column, which was true only while every category was itself
		       -- a department: the moment a menu had sections, every dish got
		       -- a tab of its own that no tab bar showed.
		       COALESCE(t.root, NULLIF(i.category, ''), 'Food'),
		       i.price, i.mrp, i.options,
		       COALESCE(i.image_urls[1], ''), s.name, i.description,
		       -- every photo, not just the cover: the details gallery shows
		       -- all of them, and dropping them here lost the rest silently.
		       array_to_string(i.image_urls, E'\n'),
               GREATEST(i.stock - COALESCE((SELECT sum(o.units) FROM orders o WHERE o.item_id=i.id AND o.stage NOT IN ('delivered','rejected')),0),0)::int
		FROM inventory_items i
		JOIN seller_stores s ON s.owner = i.owner
		LEFT JOIN tree t ON t.name = i.category
		-- Only approved stores reach shoppers: a store still under review is
		-- real to its owner and to the admin, and to nobody else.
		WHERE i.stock > 0 AND s.status = 'approved' AND NOT i.delisted
	)
	SELECT p.id, p.name, p.category, p.tab, p.price, p.mrp, p.options,
	       p.image_url, p.store, p.description, p.photos,
	       COALESCE((SELECT json_agg(json_build_object('store', o.store, 'price', o.price)
	                                 ORDER BY o.store)
	                 FROM offers o WHERE o.product_id = p.id), '[]'), p.available_stock
	FROM catalogue p
	WHERE ($1::text = '' OR p.id = $1)
	  AND ($2::text = '' OR p.tab ILIKE $2)
	  AND ($3::text = '' OR p.category ILIKE $3)
	  AND ($4::text = '' OR p.name ILIKE '%' || $4 || '%'
	                     OR p.category ILIKE '%' || $4 || '%'
	                     OR p.store ILIKE '%' || $4 || '%')
	  AND ($5::text = '' OR p.store ILIKE $5
	       OR EXISTS (SELECT 1 FROM offers o
	                  WHERE o.product_id = p.id AND o.store ILIKE $5))
	ORDER BY p.id`

func (d *DB) products(ctx context.Context, f productFilter) ([]Product, error) {
	rows, err := d.sql.QueryContext(ctx, productQuery,
		f.ID, f.Tab, f.Category, f.Q, f.Store)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Product, 0)
	for rows.Next() {
		var p Product
		var offers, options []byte
		var photos string
		if err := rows.Scan(&p.ID, &p.Name, &p.Category, &p.Tab, &p.Price,
			&p.MRP, &options, &p.ImageURL, &p.Store, &p.Description, &photos,
			&offers, &p.AvailableStock); err != nil {
			return nil, err
		}
		p.ImageURLs = splitURLs(photos)
		if err := json.Unmarshal(offers, &p.Offers); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(options, &p.Options); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// product reads one listing, or sql.ErrNoRows.
func (d *DB) product(ctx context.Context, id string) (Product, error) {
	found, err := d.products(ctx, productFilter{ID: id})
	if err != nil {
		return Product{}, err
	}
	if len(found) == 0 {
		return Product{}, sql.ErrNoRows
	}
	return found[0], nil
}

// shops lists the seeded storefronts plus every real seller's store, so a
// store someone opens shows up next to the sample ones rather than nowhere.
func (d *DB) shops(ctx context.Context) ([]Shop, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT name, tagline, image_url, tab FROM shops
		UNION ALL
		SELECT s.name,
		       COALESCE(NULLIF(array_to_string(s.categories, ', '), ''), s.location),
		       s.photo_url,
		       COALESCE(s.categories[1], 'All')
		FROM seller_stores s
		WHERE s.status = 'approved'
		  AND NOT EXISTS (SELECT 1 FROM shops sh WHERE sh.name = s.name)
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Shop
	for rows.Next() {
		var s Shop
		if err := rows.Scan(&s.Name, &s.Tagline, &s.ImageURL, &s.Tab); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// store reads one seller's store.
//
// database/sql cannot scan a text[] into []string, so the array is flattened
// in SQL and split here. ponytail: two lines instead of an array-type
// dependency for the one column that needs it.
func (d *DB) store(ctx context.Context, owner string) (SellerStore, error) {
	var s SellerStore
	var categories string
	err := d.sql.QueryRowContext(ctx, `
		SELECT owner, name, location, city, array_to_string(categories, ','),
		       photo_url, status, reject_reason
		FROM seller_stores WHERE owner = $1`, owner).
		Scan(&s.Owner, &s.Name, &s.Location, &s.City, &categories, &s.PhotoURL,
			&s.Status, &s.RejectReason)
	if err != nil {
		return s, err
	}
	s.Categories = []string{}
	if categories != "" {
		s.Categories = strings.Split(categories, ",")
	}
	return s, nil
}

// items reads a seller's inventory, newest first, with status derived here
// rather than stored — one less column that can drift out of sync.
// allItems is every product in the shop, whoever sells it.
//
// The admin listing needs the whole catalogue in one place — which store, how
// much stock, how much of it is already spoken for, and how many orders point
// at the row. That last number is what makes the delete button honest: an item
// with orders against it cannot be deleted (the FK refuses, and so does the
// handler), so the screen says so before the admin tries rather than after.
func (d *DB) allItems(ctx context.Context) ([]InventoryItem, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT i.id, i.title, i.description, i.category, i.price, i.mrp,
		       i.options, i.compare_group, i.attributes, i.stock, i.delisted,
		       COALESCE((SELECT sum(o.units) FROM orders o
		                 WHERE o.item_id = i.id
		                   AND o.stage NOT IN ('delivered','rejected')), 0)::int,
		       (SELECT count(*) FROM orders o WHERE o.item_id = i.id)::int,
		       COALESCE((SELECT sum(o.units) FROM orders o
		                 WHERE o.item_id = i.id AND o.stage = 'delivered'), 0)::int,
		       s.name, i.owner,
		       array_to_string(i.image_urls, E'\n')
		FROM inventory_items i
		JOIN seller_stores s ON s.owner = i.owner
		ORDER BY s.name, i.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]InventoryItem, 0)
	for rows.Next() {
		var i InventoryItem
		var urls string
		var options, attributes []byte
		if err := rows.Scan(&i.ID, &i.Title, &i.Description, &i.Category,
			&i.Price, &i.MRP, &options, &i.CompareGroup, &attributes,
			&i.Stock, &i.Delisted, &i.Reserved, &i.Orders, &i.Sold, &i.StoreName,
			&i.Owner, &urls); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(options, &i.Options); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(attributes, &i.Attributes); err != nil {
			return nil, err
		}
		i.Status = stockStatus(i.Stock)
		i.ImageURLs = splitURLs(urls)
		out = append(out, i)
	}
	return out, rows.Err()
}

func (d *DB) items(ctx context.Context, owner string) ([]InventoryItem, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, title, description, category, price, mrp, options,
		       compare_group, attributes, stock, delisted,
		       -- Units already spoken for by live orders. The shop shows
		       -- stock minus this, so without it a seller reading "1 left"
		       -- had no way to know shoppers were seeing "Out of stock".
		       COALESCE((SELECT sum(o.units) FROM orders o
		                 WHERE o.item_id = inventory_items.id
		                   AND o.stage NOT IN ('delivered','rejected')), 0)::int,
		       COALESCE((SELECT sum(o.units) FROM orders o
		                 WHERE o.item_id = inventory_items.id AND o.stage = 'delivered'), 0)::int,
		       array_to_string(image_urls, E'\n')
		FROM inventory_items WHERE owner = $1 ORDER BY id DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]InventoryItem, 0)
	for rows.Next() {
		var i InventoryItem
		var urls string
		var options, attributes []byte
		if err := rows.Scan(&i.ID, &i.Title, &i.Description, &i.Category,
			&i.Price, &i.MRP, &options, &i.CompareGroup, &attributes,
			&i.Stock, &i.Delisted, &i.Reserved, &i.Sold, &urls); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(options, &i.Options); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(attributes, &i.Attributes); err != nil {
			return nil, err
		}
		i.Status = stockStatus(i.Stock)
		i.ImageURLs = splitURLs(urls)
		out = append(out, i)
	}
	return out, rows.Err()
}

// orders reads every order against this seller's stock, newest first. Scoped
// by store_owner rather than by joining the item, so an item deleted after
// the fact does not take its order history with it.
func (d *DB) orders(ctx context.Context, owner string) ([]Order, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT `+orderColumns+`
		FROM orders WHERE store_owner = $1
		ORDER BY placed_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Order, 0)
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// splitURLs turns the newline-joined text[] back into a list. Same trick as
// the categories column: no array-type dependency for two columns.
func splitURLs(joined string) []string {
	if joined == "" {
		return []string{}
	}
	return strings.Split(joined, "\n")
}
