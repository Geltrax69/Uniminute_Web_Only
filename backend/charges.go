package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"strings"
)

type Charge struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

const deliveryChargeID = "delivery"

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadCharges(ctx context.Context, q querier) ([]Charge, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,name,amount FROM checkout_charges ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Charge{}
	for rows.Next() {
		var c Charge
		if err := rows.Scan(&c.ID, &c.Name, &c.Amount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func chargesTotal(cs []Charge) float64 {
	t := 0.0
	for _, c := range cs {
		t += c.Amount
	}
	return math.Round(t*100) / 100
}

// GET /api/charges — what every basket pays on top of its items.
func (a *API) handleCharges(w http.ResponseWriter, r *http.Request) {
	cs, err := loadCharges(r.Context(), a.db.sql)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load charges")
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

// PUT /api/admin/charges — the whole list at once. Delivery must stay in it.
func (a *API) handleSaveCharges(w http.ResponseWriter, r *http.Request) {
	var in []Charge
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if msg := validCharges(in); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save charges")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM checkout_charges`); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save charges")
		return
	}
	for i, c := range in {
		id := c.ID
		if id != deliveryChargeID {
			id = newChargeID()
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO checkout_charges(id,name,amount,position) VALUES($1,$2,$3,$4)`,
			id, strings.TrimSpace(c.Name), math.Round(c.Amount*100)/100, i); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save charges")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save charges")
		return
	}
	a.handleCharges(w, r)
}

func validCharges(in []Charge) string {
	if len(in) > 10 {
		return "at most 10 charges"
	}
	delivery := 0
	names := map[string]bool{}
	for _, c := range in {
		name := strings.TrimSpace(c.Name)
		if name == "" || len(name) > 40 {
			return "every charge needs a name of up to 40 characters"
		}
		if names[strings.ToLower(name)] {
			return "two charges are both called " + name
		}
		names[strings.ToLower(name)] = true
		if math.IsNaN(c.Amount) || c.Amount < 0 || c.Amount > 10000 {
			return name + ": enter an amount from ₹0 to ₹10,000"
		}
		if c.ID == deliveryChargeID {
			delivery++
		}
	}
	if delivery != 1 {
		return "the delivery charge can be changed, not removed"
	}
	return ""
}

func newChargeID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "c-" + hex.EncodeToString(b)
}
