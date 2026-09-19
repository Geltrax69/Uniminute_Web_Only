package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// POST /api/seller/store — opens (or updates) the seller's store.
func (a *API) handleCreateStore(w http.ResponseWriter, r *http.Request) {
	in, photos, err := decodeStore(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Owner = a.owner(r)
	in.Name = strings.TrimSpace(in.Name)
	in.Location = strings.TrimSpace(in.Location)
	if in.Name == "" || in.Location == "" || len(in.Categories) == 0 {
		writeError(w, http.StatusBadRequest,
			"name, location and at least one category are required")
		return
	}
	city, ok := resolveCity(in.City)
	if !ok {
		writeError(w, http.StatusBadRequest,
			"we only deliver around "+ServiceableCities[0])
		return
	}
	in.City = city

	// Photo first: if Cloudinary refuses, nothing has been written yet, so
	// the seller retries one call instead of owning a store with no picture.
	if len(photos) > 0 {
		if a.cloud == nil {
			writeError(w, http.StatusServiceUnavailable, "photo storage is not configured")
			return
		}
		url, err := a.cloud.upload(r.Context(), storeFolder(in.Name), storePhotoName, photos[0])
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		in.PhotoURL = url
	}

	// COALESCE keeps an existing photo when this call did not carry one.
	//
	// Editing a store that was rejected sends it back for review; editing an
	// approved one does not, so a live shop is not taken down by a typo fix.
	if err := a.db.sql.QueryRowContext(r.Context(), `
		INSERT INTO seller_stores (owner, name, location, city, categories, photo_url)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (owner) DO UPDATE SET
			name = EXCLUDED.name, location = EXCLUDED.location,
			city = EXCLUDED.city, categories = EXCLUDED.categories,
			photo_url = COALESCE(NULLIF(EXCLUDED.photo_url, ''), seller_stores.photo_url),
			status = CASE WHEN seller_stores.status = 'rejected'
			              THEN 'pending' ELSE seller_stores.status END,
			reject_reason = CASE WHEN seller_stores.status = 'rejected'
			                     THEN '' ELSE seller_stores.reject_reason END
		RETURNING status, reject_reason`,
		in.Owner, in.Name, in.Location, in.City, in.Categories, in.PhotoURL).
		Scan(&in.Status, &in.RejectReason); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	if in.Status == "pending" {
		a.notify(r.Context(), in.Owner, in.Name+" has been sent for review",
			"Thanks for opening "+in.Name+" on Uniminute. An admin is looking at it "+
				"now — you can add stock as soon as it is approved.")
	}
	writeJSON(w, http.StatusCreated, in)
}

// GET /api/seller/store
func (a *API) handleGetStore(w http.ResponseWriter, r *http.Request) {
	store, err := a.db.store(r.Context(), a.owner(r))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no store yet")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	writeJSON(w, http.StatusOK, store)
}

// requireApprovedStore is the gate on every write to inventory: a store that
// nobody has approved yet cannot hold stock, because that stock would be
// invisible to shoppers and look broken to the seller.
func (a *API) requireApprovedStore(w http.ResponseWriter, r *http.Request) (SellerStore, bool) {
	store, err := a.db.store(r.Context(), a.owner(r))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "open a store before adding stock")
		return store, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return store, false
	}
	switch store.Status {
	case "pending":
		writeError(w, http.StatusForbidden,
			"your store is still being reviewed — you can add stock once it is approved")
		return store, false
	case "rejected":
		writeError(w, http.StatusForbidden,
			"your store was not approved: "+store.RejectReason)
		return store, false
	}
	return store, true
}

// GET /api/seller/items — inventory plus the derived summary.
func (a *API) handleItems(w http.ResponseWriter, r *http.Request) {
	items, err := a.db.items(r.Context(), a.owner(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	var units, needsRestock int
	var value float64
	for _, i := range items {
		units += i.Stock
		value += i.Price * float64(i.Stock)
		if i.Status != "in_stock" {
			needsRestock++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"summary": map[string]any{
			"products":       len(items),
			"units":          units,
			"needsRestock":   needsRestock,
			"inventoryValue": value,
		},
	})
}

// POST /api/seller/items — add a line of stock.
func (a *API) handleAddItem(w http.ResponseWriter, r *http.Request) {
	store, ok := a.requireApprovedStore(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 40<<20)
	in, photos, err := decodeItem(r)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.validateItem(r.Context(), &in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	switch {
	case in.Title == "":
		writeError(w, http.StatusBadRequest, "title is required")
		return
	case in.Price <= 0:
		writeError(w, http.StatusBadRequest, "price must be above 0")
		return
	case in.Stock < 0:
		writeError(w, http.StatusBadRequest, "stock cannot be negative")
		return
	// An MRP below the selling price is a markup wearing a discount's badge.
	// Checked here as well as in the schema so the seller gets a sentence
	// instead of a constraint violation.
	case in.MRP != 0 && in.MRP < in.Price:
		writeError(w, http.StatusBadRequest,
			"MRP cannot be below the selling price")
		return
	}

	if len(photos) == 0 {
		writeError(w, 400, "attach at least one product photo")
		return
	}
	if len(photos) > 10 {
		writeError(w, 400, "attach at most 10 product photos")
		return
	}
	if in.Category == "" && len(store.Categories) > 0 {
		in.Category = store.Categories[0]
	}
	if in.Category == "" {
		writeError(w, 400, "choose an existing category")
		return
	}
	if err := a.validateItem(r.Context(), &in); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	// Photos are named after the store, which the gate above already read.
	in.ImageURLs = []string{}
	if len(photos) > 0 {
		if a.cloud == nil {
			writeError(w, http.StatusServiceUnavailable, "photo storage is not configured")
			return
		}
		for n, img := range photos {
			url, err := a.cloud.upload(r.Context(), storeFolder(store.Name),
				itemPhotoName(store.Name, in.Title, n+1), img)
			if err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			in.ImageURLs = append(in.ImageURLs, url)
		}
	}

	// The id comes from the sequence, so concurrent adds cannot collide.
	err = a.db.sql.QueryRowContext(r.Context(), `
		INSERT INTO inventory_items
			(owner, title, description, category, price, mrp, options, variant_prices,
			 compare_group, attributes, stock, image_urls)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::text[]) RETURNING id`,
		a.owner(r), in.Title, in.Description, in.Category, in.Price, in.MRP,
		optionsJSON(in.Options), variantPricesJSON(in.VariantPrices), strings.TrimSpace(in.CompareGroup),
		attributesJSON(in.Attributes), in.Stock, in.ImageURLs).
		Scan(&in.ID)
	// The foreign key is what enforces "no stock without a store".
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		writeError(w, http.StatusConflict, "open a store before adding stock")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	in.Status = stockStatus(in.Stock)
	writeJSON(w, http.StatusCreated, in)
}

type stockPatch struct {
	Delta *int `json:"delta"` // relative change, e.g. -1 for a sale
	Stock *int `json:"stock"` // absolute set, wins over delta
}

// PATCH /api/seller/items/{id}/stock
func (a *API) handlePatchStock(w http.ResponseWriter, r *http.Request) {
	var in stockPatch
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id := r.PathValue("id")

	// GREATEST keeps the floor in SQL, so a racing update cannot slip a
	// negative past the check constraint.
	var query string
	var arg int
	switch {
	case in.Stock != nil:
		query = `UPDATE inventory_items SET stock = GREATEST($2, 0)
		         WHERE id = $1 AND owner = $3
		         RETURNING id, title, description, category, price, mrp,
		         options, stock, delisted, array_to_string(image_urls, E'\n')`
		arg = *in.Stock
	case in.Delta != nil:
		query = `UPDATE inventory_items SET stock = GREATEST(stock + $2, 0)
		         WHERE id = $1 AND owner = $3
		         RETURNING id, title, description, category, price, mrp,
		         options, stock, delisted, array_to_string(image_urls, E'\n')`
		arg = *in.Delta
	default:
		writeError(w, http.StatusBadRequest, "send delta or stock")
		return
	}

	var it InventoryItem
	var urls string
	var options []byte
	err := a.db.sql.QueryRowContext(r.Context(), query, id, arg, a.owner(r)).Scan(
		&it.ID, &it.Title, &it.Description, &it.Category, &it.Price, &it.MRP,
		&options, &it.Stock, &it.Delisted, &urls)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	if err := json.Unmarshal(options, &it.Options); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	it.Status = stockStatus(it.Stock)
	it.ImageURLs = splitURLs(urls)
	writeJSON(w, http.StatusOK, it)
}

// PATCH /api/seller/items/{id} — the details of a listing someone already
// has: title, description, category, price, MRP, stock.
//
// Photos are not here. They go through /photos, which uploads to Cloudinary;
// mixing the two would mean a rename that fails on an image upload leaves the
// price half-changed.
func (a *API) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	var in InventoryItem
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.Category) == "" {
		if err := a.db.sql.QueryRowContext(r.Context(), `SELECT category FROM inventory_items WHERE id=$1 AND owner=$2`, r.PathValue("id"), a.owner(r)).Scan(&in.Category); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, 404, "item not found")
			} else {
				writeError(w, 500, "could not read item")
			}
			return
		}
		if in.Category == "" {
			writeError(w, 400, "choose an existing category")
			return
		}
	}
	if err := a.validateItem(r.Context(), &in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	switch {
	case in.Title == "":
		writeError(w, http.StatusBadRequest, "title is required")
		return
	case in.Price <= 0:
		writeError(w, http.StatusBadRequest, "price must be above 0")
		return
	case in.Stock < 0:
		writeError(w, http.StatusBadRequest, "stock cannot be negative")
		return
	case in.MRP != 0 && in.MRP < in.Price:
		writeError(w, http.StatusBadRequest,
			"MRP cannot be below the selling price")
		return
	}

	var it InventoryItem
	var urls string
	var options, variants, attributes []byte
	// The owner is in the WHERE clause, not just the read: without it a seller
	// could reprice somebody else's stock by guessing an id.
	err := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE inventory_items
		SET title = $2, description = $3, category = $4, price = $5,
		    mrp = $6, options = $7, variant_prices = $8, compare_group = $9, attributes = $10,
		    stock = $11
		WHERE id = $1 AND owner = $12
		RETURNING id, title, description, category, price, mrp, options, variant_prices,
		          compare_group, attributes, stock, delisted,
		          array_to_string(image_urls, E'\n')`,
		r.PathValue("id"), in.Title, in.Description, in.Category, in.Price,
		in.MRP, optionsJSON(in.Options), variantPricesJSON(in.VariantPrices), strings.TrimSpace(in.CompareGroup),
		attributesJSON(in.Attributes), in.Stock, a.owner(r)).
		Scan(&it.ID, &it.Title, &it.Description, &it.Category, &it.Price,
			&it.MRP, &options, &variants, &it.CompareGroup, &attributes, &it.Stock,
			&it.Delisted, &urls)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no item with id "+r.PathValue("id"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	if err := json.Unmarshal(options, &it.Options); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	if err := json.Unmarshal(variants, &it.VariantPrices); err != nil {
		writeError(w, 500, "could not load combination prices")
		return
	}
	if err := json.Unmarshal(attributes, &it.Attributes); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	it.Status = stockStatus(it.Stock)
	it.ImageURLs = splitURLs(urls)
	writeJSON(w, http.StatusOK, it)
}

// attributesJSON mirrors optionsJSON: the column is NOT NULL and every reader
// unmarshals it, so a nil map has to land as {} rather than as SQL NULL.
func attributesJSON(in map[string]string) []byte {
	if in == nil {
		in = map[string]string{}
	}
	out, err := json.Marshal(in)
	if err != nil {
		return []byte("{}")
	}
	return out
}

// optionsJSON is what goes into the jsonb column. A nil slice must land as an
// empty array, not as SQL NULL: the column is NOT NULL, and every reader
// unmarshals it without a nil check.
func optionsJSON(in []ItemOption) []byte {
	if in == nil {
		in = []ItemOption{}
	}
	out, err := json.Marshal(in)
	if err != nil {
		return []byte("[]")
	}
	return out
}

// DELETE /api/seller/items/{id}
//
// Refuses while any order references the item. An order is the record of a
// sale — the buyer's proof of purchase, the seller's history, the admin's
// audit trail — and deleting the product used to take all of it, silently,
// including orders already delivered. handleDeleteCategory has always worked
// this way; this is the same guard, one table over.
func (a *API) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// idx_orders_item makes this a cheap count.
	var placed int
	a.db.sql.QueryRowContext(r.Context(),
		`SELECT count(*) FROM orders WHERE item_id = $1`, id).Scan(&placed)
	if placed > 0 {
		writeError(w, http.StatusConflict, plural(placed, "order")+
			" placed for this product. Hide it from the shop instead — "+
			"deleting it would take the orders with it.")
		return
	}

	res, err := a.db.sql.ExecContext(r.Context(),
		`DELETE FROM inventory_items WHERE id = $1 AND owner = $2`, id, a.owner(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/seller/items/{id}/listing — {"delisted": true|false}
//
// The way out of the delete guard: hiding a product takes it off the shop
// without touching a single order.
func (a *API) handlePatchListing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Delisted *bool `json:"delisted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Delisted == nil {
		writeError(w, http.StatusBadRequest, "send delisted: true or false")
		return
	}
	id := r.PathValue("id")
	res, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE inventory_items SET delisted = $2 WHERE id = $1 AND owner = $3`,
		id, *in.Delisted, a.owner(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save or load store data — try again")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
