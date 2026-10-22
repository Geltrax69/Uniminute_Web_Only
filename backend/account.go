package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
)

// User is one person, recognised by the address they sign in with.
//
// Roles are derived, never stored: someone is a seller exactly when they have
// a store, so the two can never disagree. Opening a store makes the answer
// change on the next read with nothing to keep in sync.
type User struct {
	Email    string   `json:"email"`
	Avatar   string   `json:"avatar"`
	Interest string   `json:"interest"`
	PublicID string   `json:"id"` // LMZ-1001, the one a person quotes
	Name     string   `json:"name"`
	Phone    string   `json:"phone"`
	Roles    []string `json:"roles"` // ["buyer"] or ["buyer","seller"]
	HasStore bool     `json:"hasStore"`

	// Whether they have a password, so the sign-in screen knows to ask for
	// one instead of mailing a code. Never the hash itself.
	HasPassword bool `json:"hasPassword"`
	HasAddress  bool `json:"hasAddress"`

	// True once we know who they are, how to reach them and where to
	// deliver. False sends them to the details screen rather than into a
	// shop that cannot deliver to them.
	Ready bool `json:"ready"`
}

// SetUp reports whether we have what an order needs: who they are, a number
// to call, and somewhere to deliver.
func (u User) SetUp() bool {
	return u.Name != "" && u.Phone != "" && u.HasAddress
}

// upsertUser creates the row on first sign-in and returns it either way. The
// public id is assigned once by the sequence and never changes.
func (d *DB) upsertUser(ctx context.Context, email string) (User, error) {
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO NOTHING`,
		email); err != nil {
		return User{}, err
	}
	return d.user(ctx, email)
}

func (d *DB) user(ctx context.Context, email string) (User, error) {
	u := User{Email: email}
	err := d.sql.QueryRowContext(ctx, `
		SELECT u.public_id, u.name, u.phone,
		       EXISTS (SELECT 1 FROM seller_stores s WHERE s.owner = u.email),
		       u.pass_hash <> '',
		       EXISTS (SELECT 1 FROM addresses a WHERE a.email = u.email), u.avatar, u.interest
		FROM users u WHERE u.email = $1`, email).
		Scan(&u.PublicID, &u.Name, &u.Phone, &u.HasStore, &u.HasPassword,
			&u.HasAddress, &u.Avatar, &u.Interest)
	if err != nil {
		return u, err
	}
	u.Roles = []string{"buyer"}
	u.Ready = u.SetUp()
	if u.HasStore {
		u.Roles = append(u.Roles, "seller")
	}
	return u, nil
}

// GET /api/me — who is signed in, with their roles.
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	// A session can outlive its user row only if someone deleted it by hand;
	// recreating is friendlier than a 500.
	u, err := a.db.upsertUser(r.Context(), a.owner(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// PATCH /api/me — name, phone, and a password if they want one.
func (a *API) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name            *string  `json:"name"`
		Phone           *string  `json:"phone"`
		Password        *string  `json:"password"`
		CurrentPassword string   `json:"currentPassword"`
		Avatar          *string  `json:"avatar"`
		SetupAddress    *Address `json:"setupAddress"`
		SetupRequestID  string   `json:"setupRequestId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if err := textLimit(*in.Name, "name", 100, true); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if in.Phone != nil {
		*in.Phone = strings.TrimSpace(*in.Phone)
		if !indianPhone.MatchString(*in.Phone) {
			writeError(w, 400, "enter a valid 10-digit Indian mobile number")
			return
		}
		*in.Phone = normalisePhone(*in.Phone)
	}
	var interest *string
	if in.Avatar != nil {
		group, ok := profileAvatars[*in.Avatar]
		if !ok {
			writeError(w, 400, "Choose an avatar from the collection.")
			return
		}
		interest = &group
	}
	if in.SetupAddress != nil {
		if in.Name == nil || in.Phone == nil {
			writeError(w, 400, "Name and mobile number are required.")
			return
		}
		if err := validateAddress(in.SetupAddress); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		city, ok := resolveCity(in.SetupAddress.City)
		if !ok {
			writeError(w, 400, "Choose a supported delivery location.")
			return
		}
		in.SetupAddress.City = city
		in.SetupAddress.Name, in.SetupAddress.Phone = *in.Name, *in.Phone
		if in.SetupAddress.Label == "" {
			in.SetupAddress.Label = "Home"
		}
		if len(in.SetupRequestID) < 16 || len(in.SetupRequestID) > 80 {
			writeError(w, 400, "Invalid profile save. Reload and try again.")
			return
		}
		// A response lost on a slow connection may be retried after its
		// password was committed. Replay the result without changing it again.
		var completed bool
		if err := a.db.sql.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1 AND setup_request_id=$2)`, a.owner(r), in.SetupRequestID).Scan(&completed); err != nil {
			writeError(w, 503, "Could not check profile save.")
			return
		}
		if completed {
			u, err := a.db.user(r.Context(), a.owner(r))
			if err != nil {
				writeError(w, 503, "Could not load profile.")
				return
			}
			writeJSON(w, 200, u)
			return
		}
	}

	if in.SetupAddress == nil {
		in.SetupRequestID = ""
	}
	email := a.owner(r)
	if _, err := a.db.upsertUser(r.Context(), email); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Hashed here, never stored or logged in the clear. Sent as null by
	// anyone who is only changing their name, which leaves it untouched.
	var hash *string
	if in.Password != nil {
		if !a.allowPasswordAttempt(w, r, "profile-password", email) {
			return
		}
		var currentHash string
		if err := a.db.sql.QueryRowContext(r.Context(), `SELECT pass_hash FROM users WHERE email=$1`, email).Scan(&currentHash); err != nil {
			writeError(w, 503, "could not check current password")
			return
		}
		if currentHash == "" {
			var recent bool
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			err := a.db.sql.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE access_hash=$1 AND created_at>now()-interval '5 minutes')`, hashCode(token)).Scan(&recent)
			if err != nil || !recent {
				writeError(w, 401, "sign in again with your email code before setting a password")
				return
			}
		}
		if currentHash != "" && !passwordMatches(currentHash, in.CurrentPassword) {
			writeError(w, 401, "enter your current password, or use the email password-reset flow")
			return
		}

		if len(*in.Password) < minPasswordLength {
			writeError(w, http.StatusBadRequest,
				"a password needs at least 8 characters")
			return
		}
		h, err := hashPassword(*in.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		hash = &h
	}

	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 503, "could not update profile")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET name=COALESCE($2,name),phone=COALESCE($3,phone),pass_hash=COALESCE($4,pass_hash),avatar=COALESCE($5,avatar),interest=COALESCE($6,interest),setup_request_id=CASE WHEN $7<>'' THEN $7 ELSE setup_request_id END WHERE email=$1`, email, in.Name, in.Phone, hash, in.Avatar, interest, in.SetupRequestID); err != nil {
		writeError(w, 503, "could not update profile")
		return
	}
	if address := in.SetupAddress; address != nil {
		// The user UPDATE holds the same owner lock as address-book writes.
		// Repeated saves update the default address rather than duplicating it.
		var id string
		err := tx.QueryRowContext(r.Context(), `SELECT id FROM addresses WHERE email=$1 ORDER BY is_default DESC,created_at,id LIMIT 1`, email).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO addresses(email,label,line,city,name,phone,is_default) VALUES($1,$2,$3,$4,$5,$6,true)`, email, address.Label, address.Line, address.City, address.Name, address.Phone)
		} else if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE addresses SET label=$3,line=$4,city=$5,name=$6,phone=$7 WHERE id=$1 AND email=$2`, id, email, address.Label, address.Line, address.City, address.Name, address.Phone)
		}
		if err != nil {
			writeError(w, 503, "Could not save delivery address. Your profile has not changed.")
			return
		}
	}

	if hash != nil {
		current := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, err = tx.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE email=$1 AND access_hash<>$2`, email, hashCode(current)); err != nil {
			writeError(w, 503, "could not revoke other sessions")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 503, "could not update profile")
		return
	}

	u, err := a.db.user(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// Address is one entry in a person's address book.
type Address struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Line    string `json:"line"`
	City    string `json:"city"`
	Pincode string `json:"pincode"`
	Name    string `json:"name"`  // who receives it
	Phone   string `json:"phone"` // and on what number
	Default bool   `json:"isDefault"`
}

func (d *DB) addresses(ctx context.Context, email string) ([]Address, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, label, line, city, pincode, name, phone, is_default
		FROM addresses WHERE email = $1
		ORDER BY is_default DESC, created_at`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Address, 0)
	for rows.Next() {
		var a Address
		if err := rows.Scan(&a.ID, &a.Label, &a.Line, &a.City, &a.Pincode,
			&a.Name, &a.Phone, &a.Default); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GET /api/addresses — the book, default first.
func (a *API) handleAddresses(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.addresses(r.Context(), a.owner(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/addresses — save one. The first one saved becomes the default.
func (a *API) handleAddAddress(w http.ResponseWriter, r *http.Request) {
	var in Address
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateAddress(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	in.Line = strings.TrimSpace(in.Line)
	in.Name = strings.TrimSpace(in.Name)
	in.Phone = strings.TrimSpace(in.Phone)
	if in.Line == "" {
		writeError(w, http.StatusBadRequest, "street address is required")
		return
	}
	city, ok := resolveCity(in.City)
	if !ok {
		writeError(w, http.StatusBadRequest,
			"we only deliver around "+ServiceableCities[0])
		return
	}
	in.City = city
	if in.Label == "" {
		in.Label = "Home"
	}

	if id := r.PathValue("id"); id != "" {
		err := a.db.sql.QueryRowContext(r.Context(), `UPDATE addresses SET label=$3,line=$4,city=$5,pincode=$6,name=$7,phone=$8
   WHERE id=$1 AND email=$2 RETURNING id,is_default`, id, a.owner(r), in.Label, in.Line, in.City, in.Pincode, in.Name, in.Phone).Scan(&in.ID, &in.Default)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 404, "address not found")
			return
		}
		if err != nil {
			writeError(w, 500, "could not save address")
			return
		}
		writeJSON(w, 200, in)
		return
	}
	email := a.owner(r)
	if _, err := a.db.upsertUser(r.Context(), email); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	// Serialize every address mutation for this owner, including the first save.
	if _, err := tx.ExecContext(r.Context(), `SELECT 1 FROM users WHERE email=$1 FOR UPDATE`, email); err != nil {
		writeError(w, 500, "could not save address")
		return
	}
	// Exactly one default: promoting this one demotes the rest in the same
	// transaction, so a reader never sees two or none.
	var makeDefault bool
	if err := tx.QueryRowContext(r.Context(),
		`SELECT count(*) = 0 FROM addresses WHERE email = $1`, email).
		Scan(&makeDefault); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	makeDefault = makeDefault || in.Default
	if makeDefault {
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE addresses SET is_default = false WHERE email = $1`, email); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO addresses (email, label, line, city, pincode, name, phone, is_default)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		email, in.Label, in.Line, in.City, in.Pincode, in.Name, in.Phone, makeDefault).
		Scan(&in.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Saving an address is also the moment we learn the person's name and
	// number, so the profile picks them up rather than asking again later.
	if in.Name != "" || in.Phone != "" {
		a.db.sql.ExecContext(r.Context(), `
			UPDATE users SET
				name = CASE WHEN name = '' THEN $2 ELSE name END,
				phone = CASE WHEN phone = '' THEN $3 ELSE phone END
			WHERE email = $1`, email, in.Name, in.Phone)
	}

	in.Default = makeDefault
	writeJSON(w, http.StatusCreated, in)
}

// DELETE /api/addresses/{id} — scoped to the owner, so an id from someone
// else's book matches nothing.
func (a *API) handleDeleteAddress(w http.ResponseWriter, r *http.Request) {
	a.mutateAddress(w, r, true)
}

// PATCH /api/addresses/{id}/default — the selected destination is shared across devices.
func (a *API) handleDefaultAddress(w http.ResponseWriter, r *http.Request) {
	a.mutateAddress(w, r, false)
}

func (a *API) mutateAddress(w http.ResponseWriter, r *http.Request, remove bool) {
	email, id := a.owner(r), r.PathValue("id")
	tx, err := a.db.sql.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "could not update address")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `SELECT 1 FROM users WHERE email=$1 FOR UPDATE`, email); err != nil {
		writeError(w, 500, "could not update address")
		return
	}
	var found string
	if err = tx.QueryRowContext(r.Context(), `SELECT id FROM addresses WHERE id=$1 AND email=$2`, id, email).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 404, "address not found")
		} else {
			writeError(w, 500, "could not read address")
		}
		return
	}
	if remove {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM addresses WHERE id=$1 AND email=$2`, id, email)
	} else {
		_, err = tx.ExecContext(r.Context(), `UPDATE addresses SET is_default=(id=$2) WHERE email=$1`, email, id)
	}
	if err != nil {
		writeError(w, 500, "could not update address")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE addresses SET is_default=true WHERE id=(
  SELECT id FROM addresses WHERE email=$1 AND NOT EXISTS(SELECT 1 FROM addresses WHERE email=$1 AND is_default)
  ORDER BY created_at,id LIMIT 1)`, email); err != nil {
		writeError(w, 500, "could not select delivery address")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "could not confirm address change")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// seedUser creates or updates one shopper account from the environment, so
// there is always an address that can sign in with a password rather than
// waiting on an emailed code — the account a demo, a test run or a locked-out
// afternoon needs.
//
// Same rule as seedAdmin: the credentials never live in this repository. This
// one is a public repo, so a password written into a seed file would be
// published to the world and stay in the git history after it was removed.
//
// Runs on boot to initialize missing credentials only. A restart must never
// overwrite a password changed through account recovery. Rotate existing
// passwords through the verified reset flow, not the bootstrap environment.
func (d *DB) seedUser(ctx context.Context) error {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("SEED_USER")))
	pass := os.Getenv("SEED_USER_PASSWORD")
	if email == "" || pass == "" {
		return nil
	}
	hash, err := hashPassword(pass)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(os.Getenv("SEED_USER_NAME"))
	phone := strings.TrimSpace(os.Getenv("SEED_USER_PHONE"))
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO users (email, pass_hash, name, phone) VALUES ($1,$2,$3,$4)
		ON CONFLICT (email) DO UPDATE SET
			pass_hash = CASE WHEN users.pass_hash = '' THEN EXCLUDED.pass_hash ELSE users.pass_hash END,
			-- Only when blank. What this person typed for themselves is
			-- theirs, and a restart is not a reason to overwrite it.
			name = CASE WHEN users.name = '' THEN EXCLUDED.name ELSE users.name END,
			phone = CASE WHEN users.phone = '' THEN EXCLUDED.phone
			             ELSE users.phone END`,
		email, hash, name, phone); err != nil {
		return err
	}

	// An address too, so the account lands in the shop rather than on the
	// details form. Only if they have none — the first one they save is real.
	line := strings.TrimSpace(os.Getenv("SEED_USER_ADDRESS"))
	if line == "" {
		return nil
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO addresses (email, label, line, city, name, phone, is_default)
		SELECT $1, 'Home', $2, $3, $4, $5, true
		WHERE NOT EXISTS (SELECT 1 FROM addresses WHERE email = $1)`,
		email, line, ServiceableCities[0], name, phone)
	return err
}
