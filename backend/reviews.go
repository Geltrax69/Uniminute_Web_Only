package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Review is a buyer's verdict on one delivered order.
type Review struct {
	OrderID     string    `json:"orderId"`
	ItemID      string    `json:"itemId"`
	ItemTitle   string    `json:"itemTitle,omitempty"`
	StoreName   string    `json:"storeName,omitempty"`
	BuyerEmail  string    `json:"buyerEmail,omitempty"`
	BuyerName   string    `json:"buyerName,omitempty"`
	ItemRating  int       `json:"itemRating,omitempty"`
	ItemText    string    `json:"itemText,omitempty"`
	RiderPhone  string    `json:"riderPhone,omitempty"`
	RiderName   string    `json:"riderName,omitempty"`
	RiderRating int       `json:"riderRating,omitempty"`
	RiderText   string    `json:"riderText,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PUT /api/orders/{id}/review — only the buyer, only once it is delivered.
// Saving again replaces the earlier review.
func (a *API) handleSaveReview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemRating  int    `json:"itemRating"`
		ItemText    string `json:"itemText"`
		RiderRating int    `json:"riderRating"`
		RiderText   string `json:"riderText"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.ItemText, in.RiderText = strings.TrimSpace(plainLines(in.ItemText)), strings.TrimSpace(plainLines(in.RiderText))
	if msg := validReview(in.ItemRating, in.RiderRating); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	for _, t := range []struct{ v, label string }{{in.ItemText, "product review"}, {in.RiderText, "delivery review"}} {
		if err := textLimit(t.v, t.label, 1000, false); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	id, buyer := r.PathValue("id"), a.owner(r)
	var stage, itemID, rider string
	err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT stage, item_id, rider_phone FROM orders WHERE id = $1 AND buyer_email = $2`, id, buyer).
		Scan(&stage, &itemID, &rider)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no order "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stage != "delivered" {
		writeError(w, http.StatusConflict, "you can review an order once it is delivered")
		return
	}
	if rider == "" && in.RiderRating > 0 {
		writeError(w, http.StatusBadRequest, "no rider carried this order")
		return
	}
	_, err = a.db.sql.ExecContext(r.Context(), `
		INSERT INTO order_reviews (order_id, buyer_email, item_id, item_rating, item_text, rider_phone, rider_rating, rider_text)
		VALUES ($1,$2,$3,NULLIF($4,0),$5,$6,NULLIF($7,0),$8)
		ON CONFLICT (order_id) DO UPDATE SET
			item_rating = EXCLUDED.item_rating, item_text = EXCLUDED.item_text,
			rider_rating = EXCLUDED.rider_rating, rider_text = EXCLUDED.rider_text,
			updated_at = now()`,
		id, buyer, itemID, in.ItemRating, in.ItemText, rider, in.RiderRating, in.RiderText)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orderId": id, "itemRating": in.ItemRating, "riderRating": in.RiderRating})
}

func validReview(item, rider int) string {
	if item < 0 || item > 5 || rider < 0 || rider > 5 {
		return "ratings are 1 to 5 stars"
	}
	if item == 0 && rider == 0 {
		return "rate the product or the delivery"
	}
	return ""
}

const reviewSelect = `
	SELECT r.order_id, r.item_id, o.item_title, o.store_name, r.buyer_email,
	       COALESCE(u.name, ''), COALESCE(r.item_rating, 0), r.item_text,
	       r.rider_phone, COALESCE(rd.name, ''), COALESCE(r.rider_rating, 0), r.rider_text, r.updated_at
	FROM order_reviews r
	JOIN orders o ON o.id = r.order_id
	LEFT JOIN users u ON u.email = r.buyer_email
	LEFT JOIN riders rd ON rd.phone = r.rider_phone`

func scanReviews(rows *sql.Rows) ([]Review, error) {
	defer rows.Close()
	out := []Review{}
	for rows.Next() {
		var v Review
		if err := rows.Scan(&v.OrderID, &v.ItemID, &v.ItemTitle, &v.StoreName, &v.BuyerEmail, &v.BuyerName,
			&v.ItemRating, &v.ItemText, &v.RiderPhone, &v.RiderName, &v.RiderRating, &v.RiderText, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GET /api/orders/reviews — the buyer's own reviews, to fill their order pages.
func (a *API) handleMyReviews(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.sql.QueryContext(r.Context(), reviewSelect+` WHERE r.buyer_email = $1`, a.owner(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, err := scanReviews(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/products/{id}/reviews — public: the average and the product
// reviews, newest first. Only a first name is shown, never an address.
func (a *API) handleProductReviews(w http.ResponseWriter, r *http.Request) {
	id := strings.SplitN(r.PathValue("id"), "@", 2)[0]
	rows, err := a.db.sql.QueryContext(r.Context(),
		reviewSelect+` WHERE r.item_id = $1 AND r.item_rating IS NOT NULL ORDER BY r.updated_at DESC LIMIT 50`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, err := scanReviews(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type public struct {
		Rating int       `json:"rating"`
		Text   string    `json:"text,omitempty"`
		Name   string    `json:"name"`
		At     time.Time `json:"at"`
	}
	out := struct {
		Average float64  `json:"average"`
		Count   int      `json:"count"`
		Reviews []public `json:"reviews"`
	}{Reviews: []public{}}
	a.db.sql.QueryRowContext(r.Context(),
		`SELECT COALESCE(avg(item_rating), 0)::float8, count(item_rating) FROM order_reviews WHERE item_id = $1`, id).
		Scan(&out.Average, &out.Count)
	for _, v := range list {
		out.Reviews = append(out.Reviews, public{v.ItemRating, v.ItemText, firstName(v.BuyerName), v.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func firstName(full string) string {
	if f := strings.Fields(full); len(f) > 0 {
		return f[0]
	}
	return "A shopper"
}

// GET /api/admin/reviews — everything, newest first.
func (a *API) handleAdminReviews(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.sql.QueryContext(r.Context(), reviewSelect+` ORDER BY r.updated_at DESC LIMIT 500`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, err := scanReviews(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// reviewAfter is how long after delivery the buyer is asked for a review.
const reviewAfter = 10 * time.Minute

// requestReviews asks, once per order, for a review of anything delivered
// more than reviewAfter ago. Orders delivered before this feature (older than
// two days) are left alone rather than all nudged at once.
func (a *API) requestReviews(ctx context.Context) {
	rows, err := a.db.sql.QueryContext(ctx, `
		UPDATE orders SET review_requested_at = now()
		WHERE stage = 'delivered' AND review_requested_at IS NULL
		  AND delivered_at < now() - $1::interval
		  AND delivered_at > now() - interval '2 days'
		  AND NOT EXISTS (SELECT 1 FROM order_reviews r WHERE r.order_id = orders.id)
		RETURNING id, buyer_email, item_title, rider_phone <> ''`,
		fmt.Sprintf("%d seconds", int(reviewAfter.Seconds())))
	if err != nil {
		log.Printf("review requests: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, buyer, title string
		var carried bool
		if rows.Scan(&id, &buyer, &title, &carried) != nil {
			continue
		}
		body := "How was " + title + "? Rate the product"
		if carried {
			body += " and the delivery"
		}
		body += " — it takes ten seconds and helps the shops and riders on Uniminute."
		a.notifyOrderLater(buyer, "Please review your order", body, "/orders/"+id+"#review")
	}
}

// reviewReminders runs requestReviews every minute until ctx ends.
func (a *API) reviewReminders(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.requestReviews(ctx)
		}
	}
}
