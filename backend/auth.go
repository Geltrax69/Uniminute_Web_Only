package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// A code is short-lived and single-use, and there is a floor between sends so
// the endpoint cannot be used to mail-bomb someone.
const (
	codeLifetime = 10 * time.Minute
	codeCooldown = 60 * time.Second
	maxAttempts  = 5

	// Short access token, long refresh token: a leaked access token stops
	// working within the hour, and the refresh token rotates on every use.
	accessLifetime  = time.Hour
	refreshLifetime = 30 * 24 * time.Hour
)

// Mailer sends the sign-in code. ponytail: Resend's REST API is one POST, so
// no SDK.
type Mailer struct {
	key, from string
	http      *http.Client
	base      string // the tests point this at a local stub
}

// mailerFromEnv returns nil when unconfigured, which is how local development
// runs without sending anything: the code goes to the log instead.
func mailerFromEnv() *Mailer {
	m := &Mailer{
		key:  os.Getenv("RESEND_API_KEY"),
		from: os.Getenv("EMAIL_SEND"),
		http: &http.Client{Timeout: 15 * time.Second},
		base: "https://api.resend.com",
	}
	if m.key == "" || m.from == "" {
		return nil
	}
	return m
}

func (m *Mailer) sendCode(ctx context.Context, to, code string) error {
	return m.send(ctx, to, code+" is your Uniminute sign-in code",
		"Your Uniminute sign-in code is "+code+
			"\n\nIt expires in 10 minutes. If you did not ask to sign in, ignore this email.",
		codeHTML(code))
}

// send is one email. The plain-text part is what some clients show and what
// every client can fall back to, so it is never skipped; the HTML is the
// version most people actually see. Pass an empty html for text-only.
func (m *Mailer) send(ctx context.Context, to, subject, text string, html ...string) error {
	payload := map[string]any{
		"from":    m.from,
		"to":      []string{to},
		"subject": subject,
		"text":    text,
	}
	if len(html) > 0 && html[0] != "" {
		payload["html"] = html[0]
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+"/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.key)
	req.Header.Set("Content-Type", "application/json")

	res, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	resBody, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode >= 300 {
		// Resend explains itself in the body — a wrong from-domain is the
		// usual cause and worth passing through.
		return fmt.Errorf("resend %s: %s", res.Status, strings.TrimSpace(string(resBody)))
	}
	return nil
}

// sixDigits is crypto/rand, not math/rand: a guessable code is no code.
func sixDigits() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n), nil
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// POST /api/login — mails a fresh code to the address.
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !emailPattern.MatchString(email) {
		writeError(w, http.StatusBadRequest, "enter a valid email address")
		return
	}

	// Development shortcut: hand back a session on the spot, no code and no
	// email. Needs the flag *and* a request that came from this machine.
	//
	// The flag alone is not enough. The Cloudflare tunnel forwards to the very
	// port dev.sh runs on, so "only dev.sh sets it" and "only reachable
	// locally" are not the same sentence — with the tunnel up, a flag meant
	// for a laptop was letting anyone on the internet sign in as anybody.
	if skipLoginCode() && isLoopback(r) {
		log.Printf("SKIP_LOGIN_CODE: signing %s in without a code", email)
		a.issueSession(w, r, email)
		return
	}

	// An address with a password is asked for it instead. That is the whole
	// difference: no code is minted, nothing is emailed, and the app shows a
	// password field because the answer said to.
	var hasPassword bool
	a.db.sql.QueryRowContext(r.Context(),
		`SELECT pass_hash <> '' FROM users WHERE email = $1`, email).
		Scan(&hasPassword)
	if hasPassword {
		writeJSON(w, http.StatusOK, map[string]any{
			"email":         email,
			"needsPassword": true,
		})
		return
	}

	code, err := sixDigits()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// One row per address. The WHERE clause is the cooldown: an update that
	// changes nothing means a code went out moments ago.
	var expires time.Time
	err = a.db.sql.QueryRowContext(r.Context(), `
		INSERT INTO login_codes (email, code_hash, expires_at, sent_at)
		VALUES ($1, $2, now() + $3::interval, now())
		ON CONFLICT (email) DO UPDATE SET
			code_hash = EXCLUDED.code_hash, expires_at = EXCLUDED.expires_at,
			sent_at = now(), attempts = 0
		WHERE login_codes.sent_at < now() - $4::interval
		RETURNING expires_at`,
		email, hashCode(code),
		fmt.Sprintf("%d seconds", int(codeLifetime.Seconds())),
		fmt.Sprintf("%d seconds", int(codeCooldown.Seconds()))).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusTooManyRequests,
			"a code was just sent — check your inbox, or try again in a minute")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if a.mail == nil {
		// Unconfigured is a working local setup, not an error.
		log.Printf("no mailer: sign-in code for %s is %s", email, code)
	} else if err := a.mail.sendCode(r.Context(), email, code); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email":     email,
		"expiresAt": expires,
	})
}

// POST /api/login/verify — trades a correct code for a session token.
func (a *API) handleVerifyCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	code := strings.TrimSpace(in.Code)

	// Counting the attempt in the same statement that reads the code is what
	// makes the limit hold when someone scripts the guesses.
	var want string
	var attempts int
	err := a.db.sql.QueryRowContext(r.Context(), `
		UPDATE login_codes SET attempts = attempts + 1
		WHERE email = $1 AND expires_at > now() AND attempts < $2
		RETURNING code_hash, attempts`, email, maxAttempts).Scan(&want, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "that code has expired — ask for a new one")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if subtle.ConstantTimeCompare([]byte(hashCode(code)), []byte(want)) != 1 {
		writeError(w, http.StatusUnauthorized,
			fmt.Sprintf("wrong code — %d attempts left", maxAttempts-attempts))
		return
	}

	// Correct: burn the code so it cannot be replayed, then issue the session.
	if _, err := a.db.sql.ExecContext(r.Context(),
		`DELETE FROM login_codes WHERE email = $1`, email); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.issueSession(w, r, email)
}

// isLoopback reports whether the request came from this machine and was not
// relayed. Any forwarding header means a proxy is in front — the tunnel sets
// them, and a real local browser does not — so their presence alone disproves
// it, whatever the socket says.
func isLoopback(r *http.Request) bool {
	for _, h := range []string{
		"X-Forwarded-For", "CF-Connecting-IP", "X-Real-IP", "Forwarded",
	} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// skipLoginCode reports whether the sign-in code is bypassed. Anything but
// unset or "0"/"false" turns it on, because a flag you have to spell exactly
// right is a flag that silently stays off.
func skipLoginCode() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SKIP_LOGIN_CODE")))
	return v != "" && v != "0" && v != "false"
}

// issueSession mints the tokens and the user row behind them. Signing in is
// the first time a person becomes a row, and where their public id comes
// from; everything else joins on the email.
func (a *API) issueSession(w http.ResponseWriter, r *http.Request, email string) {
	user, err := a.db.upsertUser(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	session, err := a.db.newSession(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	session.User = &user
	writeJSON(w, http.StatusOK, session)
}

// minPasswordLength is short enough to type on a phone and long enough that
// guessing it is not a plan.
const minPasswordLength = 8

// POST /api/login/password — for an address that has one. Same session at the
// end of it as a mailed code produces; only the proof differs.
func (a *API) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !a.allowPasswordAttempt(w, r, "shopper", email) {
		return
	}

	var stored string
	err := a.db.sql.QueryRowContext(r.Context(),
		`SELECT pass_hash FROM users WHERE email = $1`, email).Scan(&stored)

	// One message for "no such address", "no password set" and "wrong
	// password". Telling them apart tells a stranger which addresses exist.
	if err != nil || stored == "" || !passwordMatches(stored, in.Password) {
		writeError(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	a.clearPasswordAttempts(r.Context(), "shopper", email)
	a.issueSession(w, r, email)
}

// POST /api/login/refresh — trades a refresh token for a fresh pair.
func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Deleting and re-issuing in one go is the rotation: the old refresh
	// token stops working the moment it is spent, so a stolen one is only
	// good until the real client next refreshes.
	var email string
	err := a.db.sql.QueryRowContext(r.Context(), `
		DELETE FROM auth_sessions
		WHERE refresh_hash = $1 AND refresh_expires_at > now()
		RETURNING email`, hashCode(in.RefreshToken)).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "session expired — sign in again")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	session, err := a.db.newSession(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, session)
}

// Session is what the app stores: two tokens and when the short one dies, so
// the client can refresh before a call fails rather than after.
type Session struct {
	User         *User  `json:"user,omitempty"` // sent on sign-in, not on refresh
	Email        string `json:"email"`
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"` // seconds
}

func (d *DB) newSession(ctx context.Context, email string) (Session, error) {
	access, err := newToken()
	if err != nil {
		return Session{}, err
	}
	refresh, err := newToken()
	if err != nil {
		return Session{}, err
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO auth_sessions
			(access_hash, refresh_hash, email, expires_at, refresh_expires_at)
		VALUES ($1,$2,$3, now() + $4::interval, now() + $5::interval)`,
		hashCode(access), hashCode(refresh), email,
		fmt.Sprintf("%d seconds", int(accessLifetime.Seconds())),
		fmt.Sprintf("%d seconds", int(refreshLifetime.Seconds())))
	if err != nil {
		return Session{}, err
	}
	return Session{
		Email: email, Token: access, RefreshToken: refresh,
		ExpiresIn: int(accessLifetime.Seconds()),
	}, nil
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// sessionEmail resolves a live access token to the address that verified it.
// An expired token is indistinguishable from a wrong one here; the caller
// turns both into the same 401.
func (d *DB) sessionEmail(ctx context.Context, token string) (string, error) {
	var email string
	err := d.sql.QueryRowContext(ctx,
		`SELECT email FROM auth_sessions WHERE access_hash = $1 AND expires_at > now()`,
		hashCode(token)).Scan(&email)
	return email, err
}
