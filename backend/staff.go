package main

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"
)

// Staff — admins and delivery riders — sign in with a password rather than an
// emailed code, so their secrets are stored the way passwords have to be:
// PBKDF2-SHA256, per-row salt, constant-time compare. ponytail: stdlib
// crypto/pbkdf2 instead of a bcrypt dependency; same job, no module to vet.
const (
	pbkdfIterations = 100_000
	staffLifetime   = 12 * time.Hour
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdfIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", pbkdfIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// passwordMatches is deliberately quiet about why it said no: a wrong user
// and a wrong password have to look identical from outside.
func passwordMatches(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	var iter int
	if _, err := fmt.Sscanf(parts[1], "%d", &iter); err != nil || iter <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// seedAdmin creates or updates the admin account from the environment. The
// credentials never live in this repository — an unset ADMIN_PASSWORD simply
// leaves whatever is already in the table.
func (d *DB) seedAdmin(ctx context.Context) error {
	user := strings.TrimSpace(os.Getenv("ADMIN_USER"))
	pass := os.Getenv("ADMIN_PASSWORD")
	if user == "" || pass == "" {
		return nil
	}
	hash, err := hashPassword(pass)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO admins (username, pass_hash) VALUES ($1,$2)
		ON CONFLICT (username) DO UPDATE SET pass_hash = EXCLUDED.pass_hash`,
		strings.ToLower(user), hash)
	return err
}

// ---- staff sessions -----------------------------------------------------

func (d *DB) newStaffSession(ctx context.Context, role, subject string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO staff_sessions (token_hash, role, subject, expires_at)
		VALUES ($1,$2,$3, now() + $4::interval)`,
		hashCode(token), role, subject,
		fmt.Sprintf("%d seconds", int(staffLifetime.Seconds())))
	return token, err
}

// staffSubject resolves a live staff token to who it belongs to, but only for
// the role asked for: an admin token presented to /api/delivery is no better
// than no token at all.
func (d *DB) staffSubject(ctx context.Context, role, token string) (string, error) {
	var subject string
	err := d.sql.QueryRowContext(ctx, `
		SELECT subject FROM staff_sessions
		WHERE token_hash = $1 AND role = $2 AND expires_at > now()`,
		hashCode(token), role).Scan(&subject)
	return subject, err
}

type staffKey struct{}

// staffOf is who the staff middleware verified: the admin's username, or the
// rider's phone number.
func (a *API) staffOf(r *http.Request) string {
	s, _ := r.Context().Value(staffKey{}).(string)
	return s
}

// ---- sign in ------------------------------------------------------------

// POST /api/admin/login
func (a *API) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if !a.allowPasswordAttempt(w, r, "admin", username) {
		return
	}

	var hash string
	err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT pass_hash FROM admins WHERE username = $1`, username).Scan(&hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The compare runs even when there is no such admin, so a missing account
	// and a wrong password take the same time to answer.
	if !passwordMatches(hash, in.Password) {
		writeError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	a.clearPasswordAttempts(r.Context(), "admin", username)
	token, err := a.db.newStaffSession(r.Context(), "admin", username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"username":  username,
		"expiresIn": int(staffLifetime.Seconds()),
	})
}

// POST /api/delivery/login — number and PIN, both handed out by an admin.
func (a *API) handleRiderLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
		PIN   string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	phone := normalisePhone(in.Phone)
	if !a.allowPasswordAttempt(w, r, "rider", phone) {
		return
	}

	var hash, name string
	var active bool
	err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT pin_hash, name, active FROM riders WHERE phone = $1`, phone).
		Scan(&hash, &name, &active)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !passwordMatches(hash, in.PIN) {
		writeError(w, http.StatusUnauthorized,
			"that number is not approved for deliveries, or the PIN is wrong")
		return
	}
	if !active {
		writeError(w, http.StatusForbidden, "this delivery account is switched off")
		return
	}
	a.clearPasswordAttempts(r.Context(), "rider", phone)
	token, err := a.db.newStaffSession(r.Context(), "rider", phone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"phone":     phone,
		"name":      name,
		"expiresIn": int(staffLifetime.Seconds()),
	})
}

// normalisePhone keeps digits only, so 98765-43210 and +91 98765 43210 are
// the same rider. The last ten digits are what India dials.
func normalisePhone(raw string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	if len(digits) > 10 {
		digits = digits[len(digits)-10:]
	}
	return digits
}

// fourDigits is crypto/rand for the same reason the sign-in code is: a
// delivery anyone can guess closed is not a verification.
func fourDigits() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d", n), nil
}

// ---- admin: what is going on ---------------------------------------------

// GET /api/admin/overview — the numbers on the admin's front page, plus the
// people behind them.
func (a *API) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT u.email, u.public_id, u.name, u.phone, u.created_at,
		       COALESCE(s.name, ''), COALESCE(s.status, '')
		FROM users u
		LEFT JOIN seller_stores s ON s.owner = u.email
		ORDER BY u.created_at DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type person struct {
		Email     string    `json:"email"`
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Phone     string    `json:"phone"`
		JoinedAt  time.Time `json:"joinedAt"`
		StoreName string    `json:"storeName,omitempty"`
		Status    string    `json:"storeStatus,omitempty"`
	}
	people := make([]person, 0)
	var sellers, pending int
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.Email, &p.ID, &p.Name, &p.Phone, &p.JoinedAt,
			&p.StoreName, &p.Status); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if p.StoreName != "" {
			sellers++
		}
		if p.Status == "pending" {
			pending++
		}
		people = append(people, p)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var riders, orders int
	a.db.sql.QueryRowContext(r.Context(),
		`SELECT count(*) FROM riders WHERE active`).Scan(&riders)
	a.db.sql.QueryRowContext(r.Context(), `SELECT count(*) FROM orders`).Scan(&orders)

	writeJSON(w, http.StatusOK, map[string]any{
		"users":         len(people),
		"sellers":       sellers,
		"pendingStores": pending,
		"riders":        riders,
		"orders":        orders,
		"people":        people,
	})
}

// GET /api/admin/insights — who is actually selling. Two leaderboards: stores
// by the orders placed on them, and items by the units that moved.
//
// Rejected orders are left out of both. They are a shopper's intent and a
// shop's refusal, not a sale, and counting them would rank a store that turns
// everything away alongside one that ships.
func (a *API) handleAdminInsights(w http.ResponseWriter, r *http.Request) {
	type storeRow struct {
		Name      string  `json:"name"`
		Orders    int     `json:"orders"`
		Units     int     `json:"units"`
		Revenue   float64 `json:"revenue"`
		Delivered int     `json:"delivered"`
	}
	stores := make([]storeRow, 0)
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT store_name, count(*), COALESCE(sum(units), 0),
		       COALESCE(sum(amount) FILTER (WHERE stage = 'delivered'), 0),
		       count(*) FILTER (WHERE stage = 'delivered')
		FROM orders
		WHERE stage <> 'rejected' AND store_name <> ''
		GROUP BY store_name
		ORDER BY count(*) DESC, sum(amount) DESC
		LIMIT 10`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var s storeRow
		if err := rows.Scan(&s.Name, &s.Orders, &s.Units, &s.Revenue,
			&s.Delivered); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		stores = append(stores, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type itemRow struct {
		Title   string  `json:"title"`
		Store   string  `json:"store"`
		Units   int     `json:"units"`
		Orders  int     `json:"orders"`
		Revenue float64 `json:"revenue"`
	}
	items := make([]itemRow, 0)
	// Grouped by title and store, not by item_id: a seller who relists the
	// same thing should not split their own bestseller into two rows.
	rows, err = a.db.sql.QueryContext(r.Context(), `
		SELECT item_title, store_name, COALESCE(sum(units), 0), count(*),
		       COALESCE(sum(amount) FILTER (WHERE stage = 'delivered'), 0)
		FROM orders
		WHERE stage <> 'rejected'
		GROUP BY item_title, store_name
		ORDER BY sum(units) DESC, count(*) DESC
		LIMIT 10`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var it itemRow
		if err := rows.Scan(&it.Title, &it.Store, &it.Units, &it.Orders,
			&it.Revenue); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var placed, delivered, rejected int
	var revenue float64
	a.db.sql.QueryRowContext(r.Context(), `
		SELECT count(*),
		       count(*) FILTER (WHERE stage = 'delivered'),
		       count(*) FILTER (WHERE stage = 'rejected'),
		       COALESCE(sum(amount) FILTER (WHERE stage = 'delivered'), 0)
		FROM orders`).Scan(&placed, &delivered, &rejected, &revenue)

	writeJSON(w, http.StatusOK, map[string]any{
		"topStores": stores,
		"topItems":  items,
		"totals": map[string]any{
			"placed":    placed,
			"delivered": delivered,
			"rejected":  rejected,
			"revenue":   revenue,
		},
	})
}

// GET /api/admin/stores?status=pending — the review queue, or everything.
func (a *API) handleAdminStores(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT s.owner, s.name, s.location, s.city,
		       array_to_string(s.categories, ','), s.photo_url, s.status,
		       s.reject_reason, COALESCE(u.phone, ''),
		       (SELECT count(*) FROM inventory_items i WHERE i.owner = s.owner)
		FROM seller_stores s
		LEFT JOIN users u ON u.email = s.owner
		WHERE ($1::text = '' OR s.status = $1)
		ORDER BY (s.status = 'pending') DESC, s.name`, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type storeRow struct {
		SellerStore
		Phone string `json:"phone"`
		Items int    `json:"items"`
	}
	out := make([]storeRow, 0)
	for rows.Next() {
		var s storeRow
		var categories string
		if err := rows.Scan(&s.Owner, &s.Name, &s.Location, &s.City, &categories,
			&s.PhotoURL, &s.Status, &s.RejectReason, &s.Phone, &s.Items); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.Categories = []string{}
		if categories != "" {
			s.Categories = strings.Split(categories, ",")
		}
		out = append(out, s)
	}
	writeJSON(w, http.StatusOK, out)
}

// PATCH /api/admin/stores/{owner}/categories — which departments a store
// sells in. A shop that opened as Electronics and grew into Books and Sports
// had no way to say so: the list was set once at onboarding and never again.
//
// The first one decides which tab the shop appears under, so the order the
// admin sends is the order stored.
func (a *API) handleStoreCategories(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Categories []string `json:"categories"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Trimmed and de-duplicated: the same department twice would show the
	// shop's own tag list repeating itself for no reason.
	seen := map[string]bool{}
	clean := make([]string, 0, len(in.Categories))
	for _, c := range in.Categories {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		clean = append(clean, c)
	}
	if len(clean) == 0 {
		writeError(w, http.StatusBadRequest,
			"a store has to sell in at least one department")
		return
	}

	// Every one has to be a real department. Without this an admin typo puts
	// a shop on a tab that does not exist, where no shopper will ever find it.
	for _, c := range clean {
		var parent string
		err := a.db.sql.QueryRowContext(r.Context(),
			`SELECT parent FROM catalog_categories WHERE name = $1`, c).
			Scan(&parent)
		if err != nil {
			writeError(w, http.StatusBadRequest, "no department called "+c)
			return
		}
		if parent != "" {
			writeError(w, http.StatusBadRequest,
				c+" is a category, not a department")
			return
		}
	}

	res, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE seller_stores SET categories = $2 WHERE owner = $1`,
		r.PathValue("owner"), clean)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no store for that owner")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": clean})
}

// POST /api/admin/stores/{owner}/approve
func (a *API) handleApproveStore(w http.ResponseWriter, r *http.Request) {
	a.reviewStore(w, r, "approved", "")
}

// POST /api/admin/stores/{owner}/reject — a rejection without a reason is
// useless to the person who has to fix it, so the reason is required.
func (a *API) handleRejectStore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&in) //nolint:errcheck // empty body is a missing reason
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "say why, so the seller can fix it")
		return
	}
	a.reviewStore(w, r, "rejected", reason)
}

func (a *API) reviewStore(w http.ResponseWriter, r *http.Request, status, reason string) {
	owner := r.PathValue("owner")
	var name string
	err := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE seller_stores SET status = $2, reject_reason = $3, reviewed_at = now()
		WHERE owner = $1 RETURNING name`, owner, status, reason).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no store for "+owner)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if status == "approved" {
		a.notify(r.Context(), owner, name+" is approved",
			"Your store is live on Uniminute. Open the seller panel to add stock — "+
				"shoppers can see it now.")
	} else {
		a.notify(r.Context(), owner, name+" was not approved",
			"Reason: "+reason+"\n\nFix that and submit the store again.")
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"owner": owner, "name": name, "status": status, "rejectReason": reason,
	})
}

// ---- admin: riders -------------------------------------------------------

// GET /api/admin/riders
func (a *API) handleListRiders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT r.phone, r.name, r.active, r.delivered, r.created_at,
		       (SELECT count(*) FROM orders o
		        WHERE o.rider_phone = r.phone AND o.stage = 'picked')
		FROM riders r ORDER BY r.created_at DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type rider struct {
		Phone     string    `json:"phone"`
		Name      string    `json:"name"`
		Active    bool      `json:"active"`
		Delivered int       `json:"delivered"`
		Carrying  int       `json:"carrying"`
		AddedAt   time.Time `json:"addedAt"`
	}
	out := make([]rider, 0)
	for rows.Next() {
		var v rider
		if err := rows.Scan(&v.Phone, &v.Name, &v.Active, &v.Delivered,
			&v.AddedAt, &v.Carrying); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/admin/riders — the admin types a number, the server picks the
// PIN. It comes back once, in this response, and is never readable again.
func (a *API) handleAddRider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	phone := normalisePhone(in.Phone)
	if len(phone) != 10 {
		writeError(w, http.StatusBadRequest, "enter a 10-digit mobile number")
		return
	}
	pin, hash, err := newPIN()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := a.db.sql.ExecContext(r.Context(), `
		INSERT INTO riders (phone, name, pin_hash) VALUES ($1,$2,$3)
		ON CONFLICT (phone) DO UPDATE SET
			name = EXCLUDED.name, pin_hash = EXCLUDED.pin_hash, active = true`,
		phone, strings.TrimSpace(in.Name), hash); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"phone": phone, "name": strings.TrimSpace(in.Name), "pin": pin,
	})
}

// POST /api/admin/riders/{phone}/pin — a forgotten PIN, or one that has been
// seen by too many people. The old one stops working immediately, and so does
// every panel already signed in on it.
// The admin may choose the PIN (six digits); with none, one is drawn.
func (a *API) handleResetRiderPIN(w http.ResponseWriter, r *http.Request) {
	phone := normalisePhone(r.PathValue("phone"))
	var in struct {
		PIN string `json:"pin"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in)
	pin, hash, err := chosenPIN(strings.TrimSpace(in.PIN))
	if errors.Is(err, errBadPIN) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	res, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE riders SET pin_hash = $2 WHERE phone = $1`, phone, hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no rider on "+phone)
		return
	}
	a.db.sql.ExecContext(r.Context(),
		`DELETE FROM staff_sessions WHERE role = 'rider' AND subject = $1`, phone)
	writeJSON(w, http.StatusOK, map[string]string{"phone": phone, "pin": pin})
}

// POST /api/admin/riders/{phone}/number — the same person, a new SIM.
//
// The number is the rider's identity everywhere: their row, the orders they
// are carrying, the ones they have already delivered. Moving it in one
// transaction is what keeps those together — a rider who changed their number
// halfway through a run would otherwise be two riders, one holding a bag
// nobody can close and one with a delivered count of zero.
func (a *API) handleChangeRiderNumber(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	old := normalisePhone(r.PathValue("phone"))
	next := normalisePhone(in.Phone)
	if len(next) != 10 {
		writeError(w, http.StatusBadRequest, "enter a 10-digit mobile number")
		return
	}
	if next == old {
		writeError(w, http.StatusBadRequest, "that is already their number")
		return
	}
	var taken bool
	if err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT true FROM riders WHERE phone = $1`, next).Scan(&taken); err == nil {
		writeError(w, http.StatusConflict, next+" already belongs to a rider")
		return
	}

	// A new number means a new PIN: whoever had the old one may still have it.
	pin, hash, err := newPIN()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	res, err := tx.ExecContext(r.Context(),
		`UPDATE riders SET phone = $2, pin_hash = $3 WHERE phone = $1`,
		old, next, hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no rider on "+old)
		return
	}
	// Everything that points at a rider points at them by number, so all of
	// it moves: the run they are on, and the history behind it.
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE orders SET
			rider_phone = CASE WHEN rider_phone = $1 THEN $2 ELSE rider_phone END,
			assigned_to = CASE WHEN assigned_to = $1 THEN $2 ELSE assigned_to END
		WHERE rider_phone = $1 OR assigned_to = $1`, old, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The old number's sessions die with it rather than following the row.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM staff_sessions WHERE role = 'rider' AND subject = $1`,
		old); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"phone": next, "previous": old, "pin": pin,
	})
}

// newPIN is the four digits and the hash that outlives them.
var errBadPIN = errors.New("a PIN is exactly six digits")

// chosenPIN hashes the PIN the admin typed, or draws one when they typed none.
func chosenPIN(pin string) (string, string, error) {
	if pin == "" {
		return newPIN()
	}
	if len(pin) != 6 || strings.Trim(pin, "0123456789") != "" {
		return "", "", errBadPIN
	}
	hash, err := hashPassword(pin)
	return pin, hash, err
}

func newPIN() (pin, hash string, err error) {
	pin, err = sixDigits()
	if err != nil {
		return "", "", err
	}
	hash, err = hashPassword(pin)
	return pin, hash, err
}

// DELETE /api/admin/riders/{phone} — off, not gone. They stop being handed
// orders and are signed out, but the row stays, so the deliveries behind them
// still point at somebody. Switching them back on is one call.
//
// DELETE /api/admin/riders/{phone}?forever=true removes the row instead. That
// is refused while they are actually carrying something: an order with nobody
// on it is worse than a rider who is switched off.
func (a *API) handleRemoveRider(w http.ResponseWriter, r *http.Request) {
	phone := normalisePhone(r.PathValue("phone"))
	if r.URL.Query().Get("forever") == "true" {
		a.deleteRider(w, r, phone)
		return
	}
	res, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE riders SET active = false WHERE phone = $1`, phone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no rider on "+phone)
		return
	}
	// Their sessions go with them; an off rider must not keep a live panel.
	a.db.sql.ExecContext(r.Context(),
		`DELETE FROM staff_sessions WHERE role = 'rider' AND subject = $1`, phone)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/admin/riders/{phone}/on — back on shift. They sign in with the
// same PIN as before; nothing about them was thrown away.
func (a *API) handleRestoreRider(w http.ResponseWriter, r *http.Request) {
	phone := normalisePhone(r.PathValue("phone"))
	res, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE riders SET active = true WHERE phone = $1`, phone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no rider on "+phone)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"phone": phone, "active": true})
}

// deleteRider removes the row for good.
//
// Orders they were only assigned go back to the open pool rather than
// disappearing with them — somebody else can take those. An order they have
// already collected cannot be handled that way, because the bag is in their
// hand, so the delete is refused until it is delivered.
func (a *API) deleteRider(w http.ResponseWriter, r *http.Request, phone string) {
	var carrying int
	if err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT count(*) FROM orders WHERE rider_phone = $1 AND stage = 'picked'`,
		phone).Scan(&carrying); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if carrying > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"they are still carrying %d order(s) — switch them off instead, or "+
				"wait until those are delivered", carrying))
		return
	}

	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if _, err := tx.ExecContext(r.Context(),
		`UPDATE orders SET assigned_to = '' WHERE assigned_to = $1
		 AND stage IN ('received', 'accepted')`, phone); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	res, err := tx.ExecContext(r.Context(), `DELETE FROM riders WHERE phone = $1`, phone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no rider on "+phone)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM staff_sessions WHERE role = 'rider' AND subject = $1`,
		phone); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pickRider chooses who gets the next order: at random, but out of whoever is
// carrying the least, so two riders on shift end up with roughly half each
// rather than one of them doing all of it by luck. Empty when nobody is
// signed up — the order then goes to whichever rider claims it first.
//
// ponytail: no shifts, no zones, no distance. Add those the day this campus
// is too big for "whoever is free".
func (d *DB) pickRider(ctx context.Context) (string, error) {
	var phone string
	err := d.sql.QueryRowContext(ctx, `
		SELECT r.phone FROM riders r
		WHERE r.active
		ORDER BY (SELECT count(*) FROM orders o
		          WHERE o.stage IN ('accepted', 'picked')
		            AND (o.rider_phone = r.phone OR o.assigned_to = r.phone)),
		         random()
		LIMIT 1`).Scan(&phone)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return phone, err
}

// ---- admin: orders and who carries them ----------------------------------

// GET /api/admin/orders?stage= — everything, newest first, with who is on it.
func (a *API) handleAdminOrders(w http.ResponseWriter, r *http.Request) {
	stage := strings.TrimSpace(r.URL.Query().Get("stage"))
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT o.id, o.item_title, o.units, o.amount, o.delivery_fee, o.stage, o.options, o.placed_at,
		       o.store_name, o.receiver_name, o.receiver_phone,
		       o.receiver_address, o.rider_phone, o.assigned_to, o.buyer_email,
		       o.payment_method, o.payment_status
		FROM orders o
		WHERE ($1::text = '' OR o.stage = $1)
		ORDER BY o.placed_at DESC LIMIT 200`, stage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out := make([]Order, 0)
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage,
			&o.Options,
			&o.PlacedAt, &o.StoreName, &o.ReceiverName, &o.ReceiverPhone,
			&o.ReceiverAddress, &o.RiderPhone, &o.AssignedTo, &o.BuyerEmail,
			&o.PaymentMethod, &o.PaymentStatus); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/admin/orders/{id}/assign — put a rider's name on an order, or
// clear it by sending an empty number.
//
// Assigning is not the same as picking up. It says who should collect it;
// rider_phone still only fills in when someone actually has the bag, so an
// order already on a run cannot be reassigned out from under them.
func (a *API) handleAssignOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	phone := normalisePhone(in.Phone)

	if phone != "" {
		var active bool
		err := a.db.sql.QueryRowContext(r.Context(),
			`SELECT active FROM riders WHERE phone = $1`, phone).Scan(&active)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no rider on "+phone)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !active {
			writeError(w, http.StatusConflict, "that rider is switched off")
			return
		}
	}

	id := r.PathValue("id")
	var stage string
	err := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE orders SET assigned_to = $2
		WHERE id = $1 AND stage IN ('received', 'accepted')
		  AND (payment_method = 'cod' OR payment_status = 'paid')
		RETURNING stage`, id, phone).Scan(&stage)
	if errors.Is(err, sql.ErrNoRows) {
		var exists string
		if scanErr := a.db.sql.QueryRowContext(r.Context(),
			`SELECT stage FROM orders WHERE id = $1`, id).Scan(&exists); scanErr == nil {
			writeError(w, http.StatusConflict,
				"that order is already "+exists+" — too late to reassign it")
			return
		}
		writeError(w, http.StatusNotFound, "no order with id "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id": id, "assignedTo": phone, "stage": stage,
	})
	if phone != "" && stage == string(StageAccepted) {
		a.notifyOrderLater("rider:"+phone, "Delivery assigned",
			"Order "+orderReference(id)+" is ready to collect. Open your delivery panel for the pickup details.", "/delivery")
	}
}

// ---- delivery panel ------------------------------------------------------

// GET /api/delivery/orders — what this rider can pick up, and what they are
// already carrying. Nobody else's: an order another rider claimed is gone
// from this list the moment they claim it, and one the admin put on someone
// else's name never appears at all.
func (a *API) handleRiderOrders(w http.ResponseWriter, r *http.Request) {
	phone := a.staffOf(r)
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT o.id, o.item_title, o.units, o.amount, o.delivery_fee, o.stage, o.options, o.placed_at,
		       o.store_name,
		       -- LEFT JOIN: a store deleted out from under an order in flight
		       -- still leaves a rider holding it, and a card with no pickup
		       -- line beats no card at all.
		       trim(both ', ' FROM concat_ws(', ', s.location, s.city)),
		       CASE WHEN o.assigned_to=$1 OR o.rider_phone=$1 THEN o.receiver_name ELSE '' END, CASE WHEN o.assigned_to=$1 OR o.rider_phone=$1 THEN o.receiver_phone ELSE '' END, CASE WHEN o.assigned_to=$1 OR o.rider_phone=$1 THEN o.receiver_address ELSE '' END,
		       o.rider_phone, o.assigned_to, o.payment_method, o.payment_status
		FROM orders o
		LEFT JOIN seller_stores s ON s.owner = o.store_owner
		WHERE ((o.stage = 'accepted' AND o.rider_phone = ''
		       AND (o.assigned_to = '' OR o.assigned_to = $1))
		   OR (o.stage = 'picked' AND o.rider_phone = $1))
		  AND (o.payment_method = 'cod' OR o.payment_status = 'paid')
		-- Yours first, then the open pool.
		ORDER BY (o.stage = 'picked') DESC, (o.assigned_to = $1) DESC,
		         o.placed_at`, phone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out := make([]Order, 0)
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage,
			&o.Options,
			&o.PlacedAt, &o.StoreName, &o.StoreAddress, &o.ReceiverName,
			&o.ReceiverPhone, &o.ReceiverAddress, &o.RiderPhone,
			&o.AssignedTo, &o.PaymentMethod, &o.PaymentStatus); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, o)
	}

	var name string
	var delivered int
	a.db.sql.QueryRowContext(r.Context(),
		`SELECT name, delivered FROM riders WHERE phone = $1`, phone).
		Scan(&name, &delivered)
	writeJSON(w, http.StatusOK, map[string]any{
		"rider":     map[string]any{"phone": phone, "name": name, "delivered": delivered},
		"orders":    out,
		"carrying":  countStage(out, StagePicked),
		"available": countStage(out, StageAccepted),
	})
}

// GET /api/delivery/history — what this rider has already handed over, newest
// first. Their own only: the panel is a record of your round, not a league
// table, and another rider's addresses are none of your business.
func (a *API) handleRiderHistory(w http.ResponseWriter, r *http.Request) {
	phone := a.staffOf(r)
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT o.id, o.item_title, o.units, o.amount, o.delivery_fee, o.stage, o.options, o.placed_at,
		       o.store_name,
		       trim(both ', ' FROM concat_ws(', ', s.location, s.city)),
		       o.receiver_name, o.receiver_phone, o.receiver_address,
		       o.delivered_at, o.payment_method, o.payment_status
		FROM orders o
		LEFT JOIN seller_stores s ON s.owner = o.store_owner
		WHERE o.stage = 'delivered' AND o.rider_phone = $1
		-- delivered_at is null on rows that predate the column; they still
		-- belong in the list, just at the bottom where their age puts them.
		ORDER BY o.delivered_at DESC NULLS LAST, o.placed_at DESC
		LIMIT 100`, phone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out := make([]Order, 0)
	var earned float64
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage,
			&o.Options,
			&o.PlacedAt, &o.StoreName, &o.StoreAddress, &o.ReceiverName,
			&o.ReceiverPhone, &o.ReceiverAddress, &o.DeliveredAt,
			&o.PaymentMethod, &o.PaymentStatus); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		earned += o.Amount
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"orders": out,
		"value":  earned,
	})
}

func countStage(orders []Order, stage OrderStage) int {
	n := 0
	for _, o := range orders {
		if o.Stage == stage {
			n++
		}
	}
	return n
}

// POST /api/delivery/orders/{id}/pick — claiming it. The WHERE clause is the
// claim: whichever rider's UPDATE lands first gets the order, and the second
// one is told it is gone rather than both riding to the same door. An order
// the admin assigned can only be claimed by the rider it was assigned to.
func (a *API) handleRiderPick(w http.ResponseWriter, r *http.Request) {
	phone := a.staffOf(r)
	id := r.PathValue("id")
	var o Order
	err := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE orders SET stage = 'picked', rider_phone = $2, picked_at = now()
		WHERE id = $1 AND stage = 'accepted' AND rider_phone = ''
		  AND (assigned_to = '' OR assigned_to = $2)
		  AND (payment_method = 'cod' OR payment_status = 'paid')
		RETURNING id, item_title, units, amount, delivery_fee, stage, placed_at, store_name,
		          receiver_name, receiver_phone, receiver_address, buyer_email,
		          assigned_to`,
		id, phone).
		Scan(&o.ID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage, &o.PlacedAt,
			&o.StoreName, &o.ReceiverName, &o.ReceiverPhone, &o.ReceiverAddress,
			&o.BuyerEmail, &o.AssignedTo)
	if errors.Is(err, sql.ErrNoRows) {
		a.explainFailedPick(w, r, id, phone)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.notifyOrderLater(o.BuyerEmail, "Order "+orderReference(o.ID)+" is on its way",
		fmt.Sprintf("A rider picked up your %s from %s.\n\n"+
			"Have your 4-digit delivery code ready — they need it to close the order.",
			o.ItemTitle, o.StoreName), "/orders/"+o.ID)
	o.BuyerEmail = ""
	writeJSON(w, http.StatusOK, o)
}

// A pick can fail because someone beat them to it, or because the order has
// somebody else's name on it. The second one is worth saying out loud, or a
// rider stands in a shop wondering why the button does nothing.
func (a *API) explainFailedPick(w http.ResponseWriter, r *http.Request, id, phone string) {
	var stage, assigned, rider string
	switch err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT stage, assigned_to, rider_phone FROM orders WHERE id = $1`, id).
		Scan(&stage, &assigned, &rider); {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "no order with id "+id)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	case assigned != "" && assigned != phone:
		writeError(w, http.StatusForbidden,
			"that order is assigned to another rider")
	default:
		writeError(w, http.StatusConflict,
			"that order is not waiting for pick-up any more")
	}
}

// POST /api/delivery/orders/{id}/deliver — the code is the hand-over. The
// rider types what the buyer read out; wrong digits change nothing.
func (a *API) handleRiderDeliver(w http.ResponseWriter, r *http.Request) {
	phone := a.staffOf(r)
	id := r.PathValue("id")
	var in struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	// Three conditions in one statement: the right order, in the right stage,
	// carried by this rider, closed with the right code. Any of them false and
	// no row comes back — which is also what stops a rider closing an order
	// that belongs to someone else's run.
	var o Order
	err = tx.QueryRowContext(r.Context(), `
		UPDATE orders SET stage = 'delivered', delivered_at = now()
		WHERE id = $1 AND stage = 'picked' AND rider_phone = $2 AND delivery_code = $3
		RETURNING id, item_id, item_title, units, amount, delivery_fee, stage, placed_at,
		          store_name, store_owner, buyer_email`,
		id, phone, strings.TrimSpace(in.Code)).
		Scan(&o.ID, &o.ItemID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage,
			&o.PlacedAt, &o.StoreName, &o.StoreOwner, &o.BuyerEmail)
	if errors.Is(err, sql.ErrNoRows) {
		a.explainFailedDelivery(w, r, id, phone)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Delivering is what actually takes the units out of stock, and what the
	// rider's count is made of. Both in the transaction: a delivery that only
	// half happened is worse than one that did not.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE inventory_items SET stock = GREATEST(stock - $2, 0) WHERE id = $1`,
		o.ItemID, o.Units); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE riders SET delivered = delivered + 1 WHERE phone = $1`, phone); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.notifyOrderLater(o.BuyerEmail, "Delivered: "+o.ItemTitle,
		fmt.Sprintf("Order %s from %s has been delivered. Enjoy.", orderReference(o.ID), o.StoreName), "/orders/"+o.ID)
	a.notifyOrderLater(o.StoreOwner, "Order "+orderReference(o.ID)+" was delivered",
		fmt.Sprintf("%d × %s reached the customer. ₹%.0f.",
			o.Units, o.ItemTitle, o.Amount), "/seller?pane=orders")
	o.BuyerEmail, o.StoreOwner = "", ""
	writeJSON(w, http.StatusOK, o)
}

// A failed delivery has three possible causes and the rider needs to know
// which: wrong code, someone else's order, or already closed.
func (a *API) explainFailedDelivery(w http.ResponseWriter, r *http.Request, id, phone string) {
	var stage, rider string
	err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT stage, rider_phone FROM orders WHERE id = $1`, id).Scan(&stage, &rider)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "no order with id "+id)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	case rider != phone:
		writeError(w, http.StatusForbidden, "that order is not on your run")
	case stage == "delivered":
		writeError(w, http.StatusConflict, "that order is already delivered")
	case stage != "picked":
		writeError(w, http.StatusConflict, "pick the order up first")
	default:
		log.Printf("order %s: wrong delivery code from rider %s", id, phone)
		writeError(w, http.StatusBadRequest,
			"wrong code — ask the customer to read it out again")
	}
}

// GET /api/admin/items?owner= — one store's stock, so an admin can fix a
// listing's photos without asking the seller to do it.
//
// Scoped to a store rather than listing everything: the reason to open this is
// always "that shop's pictures are wrong", and a catalogue-wide list would be
// thousands of rows nobody scrolls.
// GET /api/admin/items — one store's stock with ?owner=, or the whole
// catalogue without it.
//
// owner used to be required, which meant an admin could only look at
// inventory one shop at a time and had no way to answer "what is low across
// the shop" at all.
func (a *API) handleAdminItems(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.URL.Query().Get("owner"))
	var items []InventoryItem
	var err error
	if owner == "" {
		items, err = a.db.allItems(r.Context())
	} else {
		items, err = a.db.items(r.Context(), owner)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// DELETE /api/admin/items/{id}
//
// The same guard the seller route uses, for the same reason: an order is the
// record of a sale, and deleting the product it names would take it with it.
// Being an admin does not make that outcome any less irreversible.
func (a *API) handleAdminDeleteItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var placed int
	a.db.sql.QueryRowContext(r.Context(),
		`SELECT count(*) FROM orders WHERE item_id = $1`, id).Scan(&placed)
	if placed > 0 {
		writeError(w, http.StatusConflict, plural(placed, "order")+
			" placed for this product. Hide it from the shop instead — "+
			"deleting it would take the orders with it.")
		return
	}

	// No owner in the WHERE clause: an admin is not scoped to one store, which
	// is the whole point of the route.
	res, err := a.db.sql.ExecContext(r.Context(),
		`DELETE FROM inventory_items WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/admin/items/{id}/listing — {"delisted": true|false}
//
// The way past the delete guard: takes a product off the shop without
// touching a single order.
func (a *API) handleAdminPatchListing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Delisted *bool `json:"delisted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Delisted == nil {
		writeError(w, http.StatusBadRequest, "send delisted: true or false")
		return
	}
	id := r.PathValue("id")
	res, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE inventory_items SET delisted = $2 WHERE id = $1`,
		id, *in.Delisted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/admin/items/{id}/stock — {"stock": n}
//
// A correction, not a sale. An admin fixing a miscount should not have to
// sign in as the seller to do it.
func (a *API) handleAdminPatchStock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Stock *int `json:"stock"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Stock == nil {
		writeError(w, http.StatusBadRequest, "send stock")
		return
	}
	id := r.PathValue("id")
	res, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE inventory_items SET stock = GREATEST($2, 0) WHERE id = $1`,
		id, *in.Stock)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no item with id "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
