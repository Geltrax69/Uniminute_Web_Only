package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
)

// The life of an order, and who moves it on:
//
//	buyer   places it            → received
//	seller  accepts              → accepted   (a 4-digit code goes to the buyer)
//	seller  rejects, with reason → rejected   (the end)
//	rider   picks it up          → picked
//	rider   types the code       → delivered  (stock drops, count goes up)
//
// Every step is one UPDATE whose WHERE clause carries both the stage it must
// be in and who is allowed to move it. Nothing here trusts an id on its own,
// which is what keeps one seller's panel — or one rider's run — away from
// somebody else's order.

// orderColumns is the shape every handler here scans, in one place so the
// column list and the Scan cannot drift apart.
const orderColumns = `id, item_id, item_title, units, amount, delivery_fee, stage, placed_at,
	store_owner, store_name, receiver_name, receiver_phone, receiver_address,
	reject_reason, rider_phone, assigned_to, options`

func scanOrder(row interface{ Scan(...any) error }) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.ItemID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage,
		&o.PlacedAt, &o.StoreOwner, &o.StoreName, &o.ReceiverName,
		&o.ReceiverPhone, &o.ReceiverAddress, &o.RejectReason, &o.RiderPhone,
		&o.AssignedTo, &o.Options)
	return o, err
}

type checkoutLine struct {
	ItemID  string  `json:"itemId"`
	Units   int     `json:"units"`
	Options Choices `json:"options,omitempty"`
}

// A legacy single-line request is still an order and includes its delivery fee.
func (a *API) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemID        string   `json:"itemId"`
		Units         int      `json:"units"`
		AddressID     string   `json:"addressId"`
		ExpectedTotal *float64 `json:"expectedTotal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if in.Units == 0 {
		in.Units = 1
	}
	if in.ExpectedTotal == nil {
		writeError(w, http.StatusConflict, "please update Uniminute or reload the website before ordering")
		return
	}
	a.placeBasket(w, r, []checkoutLine{{ItemID: in.ItemID, Units: in.Units}}, in.AddressID, in.ExpectedTotal, true, "")
}

// POST /api/orders/checkout commits every line or none, with one fee per basket.
func (a *API) handleCheckout(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	var in struct {
		Lines         []checkoutLine `json:"lines"`
		RequestID     string         `json:"requestId"`
		AddressID     string         `json:"addressId"`
		ExpectedTotal *float64       `json:"expectedTotal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if in.ExpectedTotal == nil {
		writeError(w, 400, "expectedTotal is required")
		return
	}
	if len(in.RequestID) < 8 || len(in.RequestID) > 100 {
		writeError(w, 400, "a checkout request ID is required")
		return
	}
	a.placeBasket(w, r, in.Lines, in.AddressID, in.ExpectedTotal, false, in.RequestID)
}

func (a *API) placeBasket(w http.ResponseWriter, r *http.Request, lines []checkoutLine, addressID string, expected *float64, single bool, requestID string) {
	if len(lines) == 0 || len(lines) > 100 {
		writeError(w, 400, "order between 1 and 100 items")
		return
	}
	seen := map[string]bool{}
	for _, line := range lines {
		// The same item may appear once per choice of options (Black and White).
		k := line.ItemID + "\x00" + line.Options.key()
		if line.ItemID == "" || line.Units < 1 || line.Units > 10000 || seen[k] {
			writeError(w, 400, "each item must appear once with a quantity between 1 and 10000")
			return
		}
		seen[k] = true
	}
	buyer := a.owner(r)
	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "could not place order")
		return
	}
	defer tx.Rollback()
	// Deterministic locks prevent baskets with reversed item order deadlocking.
	sort.Slice(lines, func(i, j int) bool { return lines[i].ItemID < lines[j].ItemID })
	fingerprintBytes, _ := json.Marshal(struct {
		Lines    []checkoutLine
		Address  string
		Expected *float64
	}{lines, addressID, expected})
	fingerprint := hashCode(string(fingerprintBytes))
	if requestID != "" {
		if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, buyer+":"+requestID); err != nil {
			writeError(w, 500, "could not check checkout status")
			return
		}
		var savedFingerprint string
		var response []byte
		err = tx.QueryRowContext(r.Context(), `SELECT fingerprint,response FROM checkout_attempts WHERE buyer_email=$1 AND request_id=$2`, buyer, requestID).Scan(&savedFingerprint, &response)
		if err == nil {
			if savedFingerprint != fingerprint {
				writeError(w, 409, "this checkout ID belongs to a different basket")
				return
			}
			var saved map[string]any
			if json.Unmarshal(response, &saved) != nil {
				writeError(w, 500, "could not read checkout confirmation")
				return
			}
			writeJSON(w, 201, saved)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			writeError(w, 500, "could not check checkout status")
			return
		}
	}
	{
		var available bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM riders WHERE active)`).Scan(&available); err != nil {
			writeError(w, 503, "could not check delivery availability")
			return
		}
		if !available {
			writeError(w, 503, "delivery is currently unavailable — please try again when riders are available")
			return
		}
	}
	receiver, err := deliveryTarget(r.Context(), tx, buyer, addressID)
	if err != nil {
		writeError(w, 400, "add a delivery address before ordering")
		return
	}
	charges, err := loadCharges(r.Context(), tx)
	if err != nil {
		writeError(w, 500, "could not load charges")
		return
	}
	basketFee := chargesTotal(charges)

	out := make([]Order, 0, len(lines))
	total := 0.0
	for i, line := range lines {
		var title, storeOwner, storeName, status string
		var price float64
		var stock, reserved int
		var offeredRaw []byte
		err = tx.QueryRowContext(r.Context(), `SELECT i.title,i.price,i.stock,s.owner,s.name,s.status,i.options
   FROM inventory_items i JOIN seller_stores s ON s.owner=i.owner
   WHERE i.id=$1 FOR UPDATE OF i`, line.ItemID).Scan(&title, &price, &stock, &storeOwner, &storeName, &status, &offeredRaw)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 404, "an item is no longer available")
			return
		}
		if err != nil {
			writeError(w, 500, "could not check item availability")
			return
		}
		if status != "approved" {
			writeError(w, 409, "that store is not taking orders")
			return
		}
		var offered []ItemOption
		_ = json.Unmarshal(offeredRaw, &offered)
		choices, err := matchChoices(title, offered, line.Options)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(sum(units),0) FROM orders
   WHERE item_id=$1 AND stage NOT IN ('delivered','rejected')`, line.ItemID).Scan(&reserved); err != nil {
			writeError(w, 500, "could not check stock")
			return
		}
		if stock-reserved < line.Units {
			writeError(w, 409, "not enough stock for "+title)
			return
		}
		// Keep the fee in one fulfilment record so all rider collections add up
		// exactly to the basket total, even when different riders carry its lines.
		fee := 0.0
		if i == 0 {
			fee = basketFee
		}
		amount := math.Round((price*float64(line.Units)+fee)*100) / 100
		o, err := scanOrder(tx.QueryRowContext(r.Context(), `INSERT INTO orders
   (item_id,item_title,units,amount,delivery_fee,stage,buyer_email,store_owner,store_name,
    receiver_name,receiver_phone,receiver_address,options)
   VALUES ($1,$2,$3,$4,$5,'received',$6,$7,$8,$9,$10,$11,$12) RETURNING `+orderColumns,
			line.ItemID, title, line.Units, amount, fee, buyer, storeOwner, storeName, receiver.Name, receiver.Phone, receiver.Line, choices.key()))
		if err != nil {
			log.Printf("checkout insert: %v", err)
			writeError(w, 500, "could not place order")
			return
		}
		out = append(out, o)
		total += amount
	}
	total = math.Round(total*100) / 100
	if expected != nil && math.Abs(*expected-total) > 0.005 {
		writeError(w, 409, "prices have changed — remove and re-add the affected items before ordering")
		return
	}
	response := map[string]any{"orders": out, "amount": total, "deliveryFee": basketFee, "charges": charges}
	if requestID != "" {
		encoded, _ := json.Marshal(response)
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO checkout_attempts(buyer_email,request_id,fingerprint,response)
   VALUES($1,$2,$3,$4::jsonb)`, buyer, requestID, fingerprint, string(encoded)); err != nil {
			writeError(w, 500, "could not save checkout confirmation")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "could not confirm order — check your orders before retrying")
		return
	}
	for _, o := range out {
		a.notifyOrder(r.Context(), o.StoreOwner, fmt.Sprintf("New order: %d × %s", o.Units, o.ItemTitle),
			fmt.Sprintf("%s just received an order.\n\n%d × %s\nItems ₹%.2f + charges ₹%.2f = total ₹%.2f\n\nOpen Uniminute to accept it.", o.StoreName, o.Units, o.ItemTitle, o.Amount-o.DeliveryFee, o.DeliveryFee, o.Amount))
	}
	if single {
		writeJSON(w, 201, out[0])
		return
	}
	writeJSON(w, 201, response)
}

// deliveryTarget picks the address this order is for: the one asked for, or
// the default. Scoped to the buyer, so an id from somebody else's book is
// simply not found.
func deliveryTarget(ctx context.Context, tx *sql.Tx, buyer, addressID string) (Address, error) {
	var ad Address
	err := tx.QueryRowContext(ctx, `
		SELECT id, label, line, city, pincode, name, phone
		FROM addresses
		WHERE email = $1 AND ($2::text = '' OR id = $2)
		ORDER BY is_default DESC, created_at LIMIT 1`, buyer, addressID).
		Scan(&ad.ID, &ad.Label, &ad.Line, &ad.City, &ad.Pincode, &ad.Name, &ad.Phone)
	if err != nil {
		return ad, err
	}
	// Whoever placed the order is who to call when nobody answers the door.
	if ad.Name == "" || ad.Phone == "" {
		var name, phone string
		tx.QueryRowContext(ctx, `SELECT name, phone FROM users WHERE email = $1`,
			buyer).Scan(&name, &phone)
		if ad.Name == "" {
			ad.Name = name
		}
		if ad.Phone == "" {
			ad.Phone = phone
		}
	}
	ad.Line = strings.TrimSpace(ad.Line + ", " + ad.City + " " + ad.Pincode)
	return ad, nil
}

// GET /api/orders — the buyer's own orders, newest first. This is the only
// place the delivery code is ever returned: the rider has to be told it by
// the person at the door, which is what makes it proof of delivery.
func (a *API) handleMyOrders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT `+orderColumns+`,
		       CASE WHEN stage IN ('accepted','picked') THEN delivery_code ELSE '' END
		FROM orders WHERE buyer_email = $1 ORDER BY placed_at DESC`, a.owner(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out := make([]Order, 0)
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.ItemID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee,
			&o.Stage, &o.PlacedAt, &o.StoreOwner, &o.StoreName, &o.ReceiverName,
			&o.ReceiverPhone, &o.ReceiverAddress, &o.RejectReason, &o.RiderPhone,
			&o.AssignedTo, &o.Options, &o.DeliveryCode); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		o.StoreOwner = "" // the buyer has no business with the seller's address
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": out})
}

// GET /api/seller/orders — every order against this seller's stock.
func (a *API) handleOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := a.db.orders(r.Context(), a.owner(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts := map[OrderStage]int{}
	for _, o := range orders {
		counts[o.Stage]++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"orders": orders,
		"counts": map[string]int{
			"received":  counts[StageReceived],
			"accepted":  counts[StageAccepted],
			"rejected":  counts[StageRejected],
			"picked":    counts[StagePicked],
			"delivered": counts[StageDelivered],
		},
	})
}

// POST /api/seller/orders/{id}/accept — the seller takes the order on. Stock
// is untouched; what changes is that a delivery code now exists, the buyer is
// the only one told it, and a rider now has the job.
func (a *API) handleAcceptOrder(w http.ResponseWriter, r *http.Request) {
	code, err := fourDigits()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Accepting is the moment there is something to carry, so this is where a
	// rider gets it. With nobody signed up the order stays open to whoever
	// turns up, which is also what happens if every rider is switched off.
	rider, err := a.db.pickRider(r.Context())
	if err != nil {
		log.Printf("order %s: choosing a rider: %v", r.PathValue("id"), err)
	}

	id := r.PathValue("id")
	var buyer string
	row := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE orders SET stage = 'accepted', accepted_at = now(), delivery_code = $3,
			-- An admin who already put a name on it wins; otherwise take the
			-- one drawn above.
			assigned_to = CASE WHEN assigned_to <> '' THEN assigned_to ELSE $4 END
		WHERE id = $1 AND store_owner = $2 AND stage = 'received'
		RETURNING `+orderColumns+`, buyer_email`,
		id, a.owner(r), code, rider)

	var o Order
	err = row.Scan(&o.ID, &o.ItemID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage,
		&o.PlacedAt, &o.StoreOwner, &o.StoreName, &o.ReceiverName, &o.ReceiverPhone,
		&o.ReceiverAddress, &o.RejectReason, &o.RiderPhone, &o.AssignedTo, &o.Options, &buyer)
	if !a.orderMoved(w, r, id, err) {
		return
	}
	a.notifyOrder(r.Context(), buyer, "Order "+o.ID+" is confirmed",
		fmt.Sprintf("%s accepted your order of %d × %s.\n\n"+
			"Your delivery code is %s. Read it out to the rider when they hand "+
			"the order over — it is what closes the delivery.",
			o.StoreName, o.Units, o.ItemTitle, code))
	writeJSON(w, http.StatusOK, o)
}

// POST /api/seller/orders/{id}/reject — with a reason, which the buyer sees.
func (a *API) handleRejectOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&in) //nolint:errcheck // empty body is a missing reason
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "say why, so the customer knows")
		return
	}
	id := r.PathValue("id")
	var buyer string
	row := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE orders SET stage = 'rejected', reject_reason = $3
		WHERE id = $1 AND store_owner = $2 AND stage = 'received'
		RETURNING `+orderColumns+`, buyer_email`, id, a.owner(r), reason)

	var o Order
	err := row.Scan(&o.ID, &o.ItemID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage,
		&o.PlacedAt, &o.StoreOwner, &o.StoreName, &o.ReceiverName, &o.ReceiverPhone,
		&o.ReceiverAddress, &o.RejectReason, &o.RiderPhone, &o.AssignedTo, &o.Options, &buyer)
	if !a.orderMoved(w, r, id, err) {
		return
	}
	a.notifyOrder(r.Context(), buyer, "Order "+o.ID+" could not be accepted",
		fmt.Sprintf("%s could not take your order of %d × %s.\n\nReason: %s",
			o.StoreName, o.Units, o.ItemTitle, reason))
	writeJSON(w, http.StatusOK, o)
}

// POST /api/seller/orders/{id}/deliver — the seller handing an order over
// themselves, for a store that does its own running. Refused once a rider has
// picked it up: that delivery is closed with the buyer's code, not from here.
func (a *API) handleDeliverOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	o, err := scanOrder(tx.QueryRowContext(r.Context(), `
		UPDATE orders SET stage = 'delivered', delivered_at = now()
		WHERE id = $1 AND store_owner = $2 AND stage = 'accepted' AND rider_phone = ''
		RETURNING `+orderColumns, id, a.owner(r)))
	if !a.orderMoved(w, r, id, err) {
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE inventory_items SET stock = GREATEST(stock - $2, 0) WHERE id = $1`,
		o.ItemID, o.Units); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// orderMoved turns "no row came back" into the reason it did not. Every
// stage change above is one conditional UPDATE, so this is where "not yours",
// "already moved on" and "never existed" are told apart — and they must be,
// because 409 on a double tap and 404 on a bad id mean different things to
// the app showing them.
func (a *API) orderMoved(w http.ResponseWriter, r *http.Request, id string, err error) bool {
	if err == nil {
		return true
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	var stage, owner string
	switch scanErr := a.db.sql.QueryRowContext(r.Context(),
		`SELECT stage, store_owner FROM orders WHERE id = $1`, id).
		Scan(&stage, &owner); {
	case errors.Is(scanErr, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "no order with id "+id)
	case scanErr != nil:
		writeError(w, http.StatusInternalServerError, scanErr.Error())
	case owner != a.owner(r):
		// Not their order, and not their business that it exists either.
		log.Printf("order %s: %s tried to move someone else's order", id, a.owner(r))
		writeError(w, http.StatusNotFound, "no order with id "+id)
	default:
		writeError(w, http.StatusConflict, "that order is already "+stage)
	}
	return false
}

// Cancel only a buyer's own received order. The same conditional update used
// by acceptance ensures a cancellation racing the seller cannot undo acceptance.
func (a *API) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	o, err := scanOrder(a.db.sql.QueryRowContext(r.Context(), `UPDATE orders SET stage='rejected',
  reject_reason='Cancelled by customer' WHERE id=$1 AND buyer_email=$2 AND stage='received'
  RETURNING `+orderColumns, r.PathValue("id"), a.owner(r)))
	if errors.Is(err, sql.ErrNoRows) {
		var stage string
		err = a.db.sql.QueryRowContext(r.Context(), `SELECT stage FROM orders WHERE id=$1 AND buyer_email=$2`, r.PathValue("id"), a.owner(r)).Scan(&stage)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 404, "order not found")
			return
		}
		if err != nil {
			writeError(w, 500, "could not cancel order")
			return
		}
		writeError(w, 409, "only orders waiting for the shop can be cancelled")
		return
	}
	if err != nil {
		writeError(w, 500, "could not cancel order")
		return
	}
	a.notifyOrder(r.Context(), o.StoreOwner, "Order "+o.ID+" was cancelled", "The customer cancelled the order before acceptance.")
	o.StoreOwner = ""
	writeJSON(w, 200, o)
}
