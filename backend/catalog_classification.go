package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Geltrax69/Lamazon/backend/classification"
)

func auditCatalogue(products []Product) classification.Report {
	inputs := make([]classification.Input, 0, len(products))
	for _, p := range products {
		inputs = append(inputs, classification.Input{ID: p.ID, Store: p.Store, Title: p.Name, Category: p.Category, Department: p.Tab})
	}
	return classification.Audit(inputs)
}

func (a *API) handleCatalogueAudit(w http.ResponseWriter, r *http.Request) {
	products, err := a.db.products(r.Context(), productFilter{})
	if err != nil {
		writeError(w, 500, "could not check product categories")
		return
	}
	writeJSON(w, 200, auditCatalogue(products))
}

// Sellers can see the resulting customer category before saving. The same
// pure pipeline runs on every catalogue read, including future edits/imports.
func (a *API) handleClassifyItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title    string `json:"title"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, "invalid classification request")
		return
	}
	if err := textLimit(in.Title, "title", maxItemTitle, true); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	root, err := a.db.rootCategory(r, strings.TrimSpace(in.Category))
	if err != nil {
		writeError(w, 400, "choose an existing category")
		return
	}
	result := classification.Classify(root, in.Category, in.Title)
	suggestion := ""
	if result.Status == "matched" {
		if categories, err := a.db.categories(r); err == nil {
			for _, category := range categories {
				if category.Name == result.Category && category.Name != in.Category {
					if candidateRoot, err := a.db.rootCategory(r, category.Name); err == nil && candidateRoot == root {
						suggestion = category.Name
					}
				}
			}
		}
	}
	writeJSON(w, 200, struct {
		classification.Result
		SuggestedCategory string `json:"suggestedCategory,omitempty"`
	}{result, suggestion})
}
