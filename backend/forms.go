package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// The create endpoints take either plain JSON or multipart with photos
// attached, so the app makes one call instead of "create, then upload, then
// hope both landed". isMultipart picks the branch.
func isMultipart(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/")
}

// decodeStore reads a store from either shape. The photos come back
// separately because they go to Cloudinary, not Postgres.
func decodeStore(r *http.Request) (SellerStore, [][]byte, error) {
	var in SellerStore
	if !isMultipart(r) {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return in, nil, errors.New("invalid JSON body")
		}
		return in, nil, nil
	}
	if err := r.ParseMultipartForm(maxPhoto); err != nil {
		return in, nil, err
	}
	in.Name = r.FormValue("name")
	in.Location = r.FormValue("location")
	in.City = r.FormValue("city")
	in.Categories = splitList(r.MultipartForm.Value["categories"])
	photos, err := formPhotos(r)
	return in, photos, err
}

// decodeItem reads an inventory line from either shape.
func decodeItem(r *http.Request) (InventoryItem, [][]byte, error) {
	var in InventoryItem
	if !isMultipart(r) {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return in, nil, errors.New("invalid JSON body")
		}
		return in, nil, nil
	}
	if err := r.ParseMultipartForm(maxPhoto); err != nil {
		return in, nil, err
	}
	in.Title = r.FormValue("title")
	in.Description = r.FormValue("description")
	in.Category = r.FormValue("category")
	// A form carries everything as text, so the numbers are parsed here and a
	// typo becomes a 400 rather than a silent zero.
	price, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("price")), 64)
	if err != nil {
		return in, nil, errors.New("price must be a number")
	}
	in.Price = price
	// Optional, unlike price: an item with no discount simply omits it.
	if s := strings.TrimSpace(r.FormValue("mrp")); s != "" {
		mrp, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return in, nil, errors.New("MRP must be a number")
		}
		in.MRP = mrp
	}
	// A multipart form carries the option groups as one JSON field — they are
	// a nested shape, and flattening them into repeated keys would need a
	// parser on both sides to say the same thing.
	if s := strings.TrimSpace(r.FormValue("options")); s != "" {
		if err := json.Unmarshal([]byte(s), &in.Options); err != nil {
			return in, nil, errors.New("options must be valid JSON")
		}
	}
	if s := strings.TrimSpace(r.FormValue("variantPrices")); s != "" {
		if err := json.Unmarshal([]byte(s), &in.VariantPrices); err != nil {
			return in, nil, errors.New("combination prices must be valid JSON")
		}
	}
	in.CompareGroup = strings.TrimSpace(r.FormValue("compareGroup"))
	if s := strings.TrimSpace(r.FormValue("attributes")); s != "" {
		if err := json.Unmarshal([]byte(s), &in.Attributes); err != nil {
			return in, nil, errors.New("attributes must be valid JSON")
		}
	}
	if s := strings.TrimSpace(r.FormValue("stock")); s != "" {
		stock, err := strconv.Atoi(s)
		if err != nil {
			return in, nil, errors.New("stock must be a whole number")
		}
		in.Stock = stock
	}
	photos, err := formPhotos(r)
	return in, photos, err
}

// splitList accepts either shape of a repeated form field: several
// "categories" parts, or one comma-separated value — which is all a Dart
// MultipartRequest can send, since its fields are a map.
func splitList(values []string) []string {
	out := []string{}
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// formPhotos reads the optional file parts. Absent is fine; present and
// broken is not.
func formPhotos(r *http.Request) ([][]byte, error) {
	if len(r.MultipartForm.File["file"]) == 0 {
		return nil, nil
	}
	return photoBytes(r, "file")
}

// photoBytes pulls every uploaded file out of an already-parsed multipart
// body, checking each one is really an image.
func photoBytes(r *http.Request, field string) ([][]byte, error) {
	return mediaBytes(r, field, photoTypes, maxPhoto)
}

// mediaBytes reads the attached files and rejects anything outside [allowed].
// The allow-list is a parameter because a banner may be a clip and a product
// photo may not, and that difference should live at the route that knows it.
func mediaBytes(r *http.Request, field string, allowed map[string]bool, limit int64) ([][]byte, error) {
	files := r.MultipartForm.File[field]
	if len(files) == 0 {
		return nil, errors.New("attach at least one file as " + field)
	}
	out := make([][]byte, 0, len(files))
	for _, fh := range files {
		if fh.Size > limit {
			return nil, errors.New(fh.Filename + " is larger than " +
				strconv.FormatInt(limit>>20, 10) + " MB")
		}
		f, err := fh.Open()
		if err != nil {
			return nil, err
		}
		buf := make([]byte, fh.Size)
		_, err = io.ReadFull(f, buf)
		f.Close()
		if err != nil {
			return nil, err
		}
		if kind := http.DetectContentType(buf); !allowed[strings.Split(kind, ";")[0]] {
			return nil, errors.New(fh.Filename + " is not a supported file (" + kind + ")")
		}
		out = append(out, buf)
	}
	return out, nil
}
