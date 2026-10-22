package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// Day-off timestamps expire at midnight in the service's local time. No cron
// or process-local state is involved, so restarts and multiple servers agree.
const nextISTMidnight = `((now() AT TIME ZONE 'Asia/Kolkata')::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata'`

func readAvailability(w http.ResponseWriter, r *http.Request) (bool, bool) {
	var in struct {
		Available *bool `json:"available"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil || in.Available == nil {
		writeError(w, 400, "Choose whether you are available.")
		return false, false
	}
	return *in.Available, true
}

func (a *API) handleStoreAvailability(w http.ResponseWriter, r *http.Request) {
	available, ok := readAvailability(w, r)
	if !ok {
		return
	}
	var closed bool
	err := a.db.sql.QueryRowContext(r.Context(), `UPDATE seller_stores SET closed_until=CASE WHEN $2 THEN NULL ELSE `+nextISTMidnight+` END WHERE owner=$1 AND status='approved' RETURNING COALESCE(closed_until>now(),false)`, a.owner(r), available).Scan(&closed)
	if err == sql.ErrNoRows {
		writeError(w, 403, "Only an approved store can change opening availability.")
		return
	}
	if err != nil {
		writeError(w, 503, "Could not save store availability. Try again.")
		return
	}
	writeJSON(w, 200, map[string]bool{"closedToday": closed})
}

func (a *API) handleRiderAvailability(w http.ResponseWriter, r *http.Request) {
	available, ok := readAvailability(w, r)
	if !ok {
		return
	}
	phone := a.staffOf(r)
	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 503, "Could not save availability.")
		return
	}
	defer tx.Rollback()
	var off bool
	err = tx.QueryRowContext(r.Context(), `UPDATE riders SET day_off_until=CASE WHEN $2 THEN NULL ELSE `+nextISTMidnight+` END WHERE phone=$1 AND active RETURNING COALESCE(day_off_until>now(),false)`, phone, available).Scan(&off)
	if err == sql.ErrNoRows {
		writeError(w, 403, "This delivery account is switched off. Contact your admin.")
		return
	}
	if err != nil {
		writeError(w, 503, "Could not save availability. Try again.")
		return
	}
	// Hand back uncollected work so another rider can collect it. Bags already
	// picked up stay with this rider and can still be delivered while off duty.
	if !available {
		if _, err = tx.ExecContext(r.Context(), `UPDATE orders SET assigned_to='' WHERE assigned_to=$1 AND rider_phone='' AND stage IN('received','accepted')`, phone); err != nil {
			writeError(w, 503, "Could not release uncollected orders. No availability was changed.")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 503, "Could not finish saving availability.")
		return
	}
	writeJSON(w, 200, map[string]bool{"dayOffToday": off})
}

func (a *API) handleDeliveryAvailability(w http.ResponseWriter, r *http.Request) {
	var available bool
	if err := a.db.sql.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM riders WHERE active AND (day_off_until IS NULL OR day_off_until<=now()))`).Scan(&available); err != nil {
		writeError(w, 503, "Could not check delivery availability.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]bool{"deliveryAvailable": available})
}

func (a *API) handleStoreAvailabilityState(w http.ResponseWriter, r *http.Request) {
	var closed bool
	err := a.db.sql.QueryRowContext(r.Context(), `SELECT COALESCE(closed_until>now(),false) FROM seller_stores WHERE owner=$1`, a.owner(r)).Scan(&closed)
	if err == sql.ErrNoRows {
		writeError(w, 404, "No store found.")
		return
	}
	if err != nil {
		writeError(w, 503, "Could not refresh store availability.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]bool{"closedToday": closed})
}
func (a *API) handleRiderAvailabilityState(w http.ResponseWriter, r *http.Request) {
	var off bool
	if err := a.db.sql.QueryRowContext(r.Context(), `SELECT COALESCE(day_off_until>now(),false) FROM riders WHERE phone=$1`, a.staffOf(r)).Scan(&off); err != nil {
		writeError(w, 503, "Could not refresh rider availability.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]bool{"dayOffToday": off})
}
