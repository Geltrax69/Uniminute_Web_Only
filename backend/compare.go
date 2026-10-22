package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// GroupAttribute is one field every product in a comparison group is asked
// for. Unit is what goes after the number — "W", "ml", "months" — so the
// seller types 20 and the shopper reads 20W.
//
// Keeping the unit here rather than in the value is what makes ranking cheap:
// there is no "1.5 L" against "1500ml" to reconcile, because the litres were
// never in the cell.
type GroupAttribute struct {
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"`
	// Mode says what "better" means for this field: higher_better,
	// lower_better, feature, info or equal. Empty means info — shown, never
	// ranked — so every template written before this existed still behaves.
	Mode string `json:"mode,omitempty"`
	// PerUnit marks the field that holds how much you get, which is what the
	// price is divided by for the ₹/100 g row. One per group.
	PerUnit bool `json:"perUnit,omitempty"`
}

// ComparisonGroup is a set of products that can be lined up against each
// other, and the fields to line them up on.
type ComparisonGroup struct {
	Name       string           `json:"name"`
	Attributes []GroupAttribute `json:"attributes"`
	// How many listings are in it. Read-only, and only filled in by the list.
	Items int `json:"items,omitempty"`
}

// GET /api/compare-groups — every group with its template. Public: the seller
// form and the compare screen both draw from it.
func (a *API) handleCompareGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT g.name, g.attributes,
		       (SELECT count(*) FROM inventory_items i
		        WHERE i.compare_group = g.name)
		FROM comparison_groups g ORDER BY g.name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out := make([]ComparisonGroup, 0)
	for rows.Next() {
		var g ComparisonGroup
		var attrs []byte
		if err := rows.Scan(&g.Name, &attrs, &g.Items); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := json.Unmarshal(attrs, &g.Attributes); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/admin/compare-groups — create a group, or replace its template.
//
// Replacing is deliberate: a template is a list the admin edits as a whole,
// and asking them to add fields one request at a time would make reordering
// impossible.
func (a *API) handleSaveCompareGroup(w http.ResponseWriter, r *http.Request) {
	var in ComparisonGroup
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Trimmed and de-duplicated. Two fields with one name would give the
	// seller two identical boxes and the shopper two identical rows.
	seen := map[string]bool{}
	clean := make([]GroupAttribute, 0, len(in.Attributes))
	for _, at := range in.Attributes {
		at.Name = strings.TrimSpace(at.Name)
		at.Unit = strings.TrimSpace(at.Unit)
		if at.Name == "" || seen[strings.ToLower(at.Name)] {
			continue
		}
		seen[strings.ToLower(at.Name)] = true
		clean = append(clean, at)
	}
	in.Attributes = clean

	body, err := json.Marshal(clean)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = a.db.sql.ExecContext(r.Context(), `
		INSERT INTO comparison_groups (name, attributes) VALUES ($1,$2)
		ON CONFLICT (name) DO UPDATE SET attributes = EXCLUDED.attributes`,
		in.Name, body)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		writeError(w, http.StatusBadRequest, pgErr.Message)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, in)
}

// DELETE /api/admin/compare-groups/{name} — only when nothing is in it.
// Deleting one with products would leave those listings pointing at a group
// that no longer exists, and their attributes unreadable.
func (a *API) handleDeleteCompareGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var items int
	a.db.sql.QueryRowContext(r.Context(),
		`SELECT count(*) FROM inventory_items WHERE compare_group = $1`,
		name).Scan(&items)
	if items > 0 {
		writeError(w, http.StatusConflict, plural(items, "item")+
			" still in "+name+". Move them out first.")
		return
	}
	res, err := a.db.sql.ExecContext(r.Context(),
		`DELETE FROM comparison_groups WHERE name = $1`, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no group called "+name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/compare?group=Chargers — everything comparable to each other, with
// the template to compare them on. Cheapest first, because that is the
// question a shopper opened this to answer.
func (a *API) handleCompare(w http.ResponseWriter, r *http.Request) {
	group := strings.TrimSpace(r.URL.Query().Get("group"))
	if group == "" {
		writeError(w, http.StatusBadRequest, "group is required")
		return
	}

	var attrs []byte
	err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT attributes FROM comparison_groups WHERE name = $1`, group).
		Scan(&attrs)
	if err != nil {
		writeError(w, http.StatusNotFound, "no group called "+group)
		return
	}
	template := []GroupAttribute{}
	if err := json.Unmarshal(attrs, &template); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Sold-out lines are left out: comparing against something nobody can buy
	// is how a shopper picks the winner and then finds an empty shelf.
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT i.id, i.title, i.price, i.mrp, i.attributes,
		       COALESCE(i.image_urls[1], ''), s.name
		FROM inventory_items i
		JOIN seller_stores s ON s.owner = i.owner
		WHERE i.compare_group = $1 AND i.stock > 0 AND s.status = 'approved' AND (s.closed_until IS NULL OR s.closed_until<=now())
		ORDER BY i.price`, group)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type row struct {
		ID       string            `json:"id"`
		Title    string            `json:"title"`
		Price    float64           `json:"price"`
		MRP      float64           `json:"mrp,omitempty"`
		Store    string            `json:"store"`
		ImageURL string            `json:"imageUrl"`
		Values   map[string]string `json:"values"`
	}
	out := make([]row, 0)
	for rows.Next() {
		var it row
		var values []byte
		if err := rows.Scan(&it.ID, &it.Title, &it.Price, &it.MRP, &values,
			&it.ImageURL, &it.Store); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := json.Unmarshal(values, &it.Values); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Ranking happens here rather than in the query: it needs every listing at
	// once to know who won, and SQL that could do it would be unreadable and
	// still could not express "nobody won this row".
	items := make([]comparable, 0, len(out))
	for _, it := range out {
		items = append(items, comparable{it.ID, it.Title, it.Price, it.Values})
	}
	ranked := rank(template, items)
	derived := derive(template, items)

	writeJSON(w, http.StatusOK, map[string]any{
		"group":      group,
		"attributes": ranked,
		"derived":    derived,
		"highlights": highlights(ranked, derived, items),
		"products":   out,
	})
}
