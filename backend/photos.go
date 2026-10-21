package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Room for a phone photo at full resolution; the picker already downscales to
// 80% quality. The global 1 MiB cap does not apply to these two routes.
const maxPhoto = 10 << 20

// Cloudinary rejects anything that is not an image anyway, but sniffing here
// keeps a bad upload from costing a round trip.
var photoTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true,
}

// A banner sits behind a headline on the home screen, so it may also be an
// animation or a short silent clip. A product photo may not: a video in a
// product grid is a different app, and one seller uploading a 20 MB film
// would slow the grid down for everybody.
const maxBanner = 25 << 20

var bannerTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true,
	"video/mp4": true, "video/webm": true, "video/quicktime": true,
}

// uploadedBanner parses the body and returns the attached banner artwork.
func uploadedBanner(r *http.Request) ([][]byte, error) {
	if err := r.ParseMultipartForm(maxBanner); err != nil {
		return nil, err
	}
	return mediaBytes(r, "file", bannerTypes, maxBanner)
}

// uploadedPhotos parses the body and returns the attached images.
func uploadedPhotos(r *http.Request) ([][]byte, error) {
	if err := r.ParseMultipartForm(maxPhoto); err != nil {
		return nil, err
	}
	return photoBytes(r, "file")
}

// POST /api/seller/store/photo — the store's cover picture, filed under
// Lamazon/<StoreName>/store_image.
func (a *API) handleStorePhoto(w http.ResponseWriter, r *http.Request) {
	if a.cloud == nil {
		writeError(w, http.StatusServiceUnavailable, "photo storage is not configured")
		return
	}
	store, err := a.db.store(r.Context(), a.owner(r))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "open a store before adding its photo")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	imgs, err := uploadedPhotos(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(imgs) != 1 {
		writeError(w, 400, "choose one store cover image")
		return
	}
	url, err := a.cloud.upload(r.Context(), storeFolder(store.Name), storePhotoName, imgs[0])
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if _, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE seller_stores SET photo_url = $2 WHERE owner = $1`,
		store.Owner, url); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"photoUrl": url})
}

// POST /api/seller/items/{id}/photos — one or more product pictures, named
// <store>_<item>_<n> inside that store's folder and numbered from whatever
// the item already has.
func (a *API) handleItemPhotos(w http.ResponseWriter, r *http.Request) {
	if a.cloud == nil {
		writeError(w, http.StatusServiceUnavailable, "photo storage is not configured")
		return
	}
	// A seller has to have an approved store; an admin is already past the
	// admin token check and is not adding to a store of their own.
	if !strings.HasPrefix(r.URL.Path, "/api/admin/") {
		if _, ok := a.requireApprovedStore(w, r); !ok {
			return
		}
	}
	id := r.PathValue("id")
	owner, err := a.itemOwner(r, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// The folder comes from the store, the name from the item, so both have
	// to exist before anything is uploaded — and the item has to be this
	// seller's, or an id guessed from someone else's shop would upload into
	// their listing.
	var storeName, title string
	var existing int
	err = a.db.sql.QueryRowContext(r.Context(), `
		SELECT s.name, i.title, cardinality(i.image_urls)
		FROM inventory_items i JOIN seller_stores s ON s.owner = i.owner
		WHERE i.id = $1 AND i.owner = $2`, id, owner).
		Scan(&storeName, &title, &existing)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	imgs, err := uploadedPhotos(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if existing+len(imgs) > 12 {
		writeError(w, 400, "a product can have at most 12 images")
		return
	}
	urls := make([]string, 0, len(imgs))
	for n, img := range imgs {
		url, err := a.cloud.upload(r.Context(), storeFolder(storeName),
			itemPhotoName(storeName, title, existing+n+1), img)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		urls = append(urls, url)
	}

	// Append rather than replace, and read back what is stored so the client
	// sees the same list a later GET will return.
	var saved string
	if err := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE inventory_items SET image_urls = image_urls || $2::text[]
		WHERE id = $1 AND owner = $3
		RETURNING array_to_string(image_urls, E'\n')`, id, urls, owner).
		Scan(&saved); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"imageUrls": strings.Split(saved, "\n"),
	})
}

// itemOwner resolves whose listing this is, and whether the caller may touch
// it. A seller may only reach their own; an admin reaches any.
//
// The two routes differ by prefix, not by a flag in the body — a request
// cannot claim to be an admin one, it either arrived on /api/admin/ behind
// the admin token check or it did not.
func (a *API) itemOwner(r *http.Request, id string) (string, error) {
	if strings.HasPrefix(r.URL.Path, "/api/admin/") {
		var owner string
		err := a.db.sql.QueryRowContext(r.Context(),
			`SELECT owner FROM inventory_items WHERE id = $1`, id).Scan(&owner)
		return owner, err
	}
	return a.owner(r), nil
}

// orderPhotos works out the new list from the one the item has and the one the
// client asked for.
//
// Only URLs already on the item survive. Without that check the body could
// point a listing at any image on the internet, which is a defacement waiting
// to happen and not something reordering needs. Duplicates are dropped too —
// the same photo twice gives the gallery two identical pages with no way to
// tell them apart.
func orderPhotos(current, requested []string) ([]string, error) {
	have := map[string]bool{}
	for _, u := range current {
		have[u] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(requested))
	for _, u := range requested {
		u = strings.TrimSpace(u)
		if !have[u] {
			return nil, errors.New("that photo is not on this item")
		}
		if seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out, nil
}

// PUT /api/seller/items/{id}/photos and the admin twin — the whole ordered
// list, which is how one call covers reordering and removing at once.
//
// Only URLs the item already has are accepted. Without that check the body
// could point a listing at any image on the internet, which is a defacement
// waiting to happen and not something reordering needs.
func (a *API) handleReorderItemPhotos(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ImageURLs []string `json:"imageUrls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id := r.PathValue("id")
	owner, err := a.itemOwner(r, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var current string
	err = a.db.sql.QueryRowContext(r.Context(), `
		SELECT array_to_string(image_urls, E'\n') FROM inventory_items
		WHERE id = $1 AND owner = $2`, id, owner).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out, err := orderPhotos(splitURLs(current), in.ImageURLs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(out) == 0 {
		writeError(w, 400, "keep at least one product photo")
		return
	}
	var saved string
	if err := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE inventory_items SET image_urls = $2::text[]
		WHERE id = $1 AND owner = $3
		RETURNING array_to_string(image_urls, E'\n')`, id, out, owner).
		Scan(&saved); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imageUrls": splitURLs(saved)})
}
