package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Push sends browser notifications through Firebase Cloud Messaging.
type Push struct {
	publicKey string
	webConfig map[string]string
	fcm       *FCM
}

func pushFromEnv() *Push {
	fcm := fcmFromEnv()
	webConfig := firebaseWebConfig()
	if webConfig["projectId"] == "" && fcm != nil {
		webConfig["projectId"] = fcm.projectID
	}
	p := &Push{
		publicKey: os.Getenv("FIREBASE_WEB_PUSH_PUBLIC_KEY"),
		webConfig: webConfig,
		fcm:       fcm,
	}
	if p.publicKey == "" && p.fcm == nil && len(p.webConfig) == 0 {
		return nil
	}
	return p
}

// firebaseWebConfig is safe to expose: Firebase's browser configuration
// identifies the project but does not grant server access. Keep it in the
// environment so staging and production can never accidentally share tokens.
func firebaseWebConfig() map[string]string {
	out := map[string]string{}
	if raw := strings.TrimSpace(os.Getenv("FIREBASE_WEB_CONFIG")); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	for key, env := range map[string]string{
		"apiKey": "FIREBASE_API_KEY", "authDomain": "FIREBASE_AUTH_DOMAIN",
		"projectId": "FIREBASE_PROJECT_ID", "storageBucket": "FIREBASE_STORAGE_BUCKET",
		"messagingSenderId": "FIREBASE_MESSAGING_SENDER_ID", "appId": "FIREBASE_APP_ID",
	} {
		if out[key] == "" {
			if value := strings.TrimSpace(os.Getenv(env)); value != "" {
				out[key] = value
			}
		}
	}
	return out
}

// A subscription is the Firebase Messaging token the browser hands us.
type subscription struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

// GET /api/push/key — the app needs the Firebase Web Push certificate public
// key to subscribe, and baking it into the build would mean rebuilding to
// rotate it.
func (a *API) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if a.push == nil || a.push.publicKey == "" {
		writeError(w, http.StatusServiceUnavailable, "FIREBASE_WEB_PUSH_PUBLIC_KEY is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": a.push.publicKey})
}

// GET /api/push/config — the browser needs both its public Firebase app
// identity and the Web Push certificate before it can mint an FCM token.
func (a *API) handlePushConfig(w http.ResponseWriter, r *http.Request) {
	if a.push == nil || a.push.publicKey == "" ||
		a.push.webConfig["apiKey"] == "" || a.push.webConfig["projectId"] == "" ||
		a.push.webConfig["messagingSenderId"] == "" || a.push.webConfig["appId"] == "" {
		writeError(w, http.StatusServiceUnavailable, "browser push is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"firebase":  a.push.webConfig,
		"publicKey": a.push.publicKey,
	})
}

// POST /api/push/subscribe — one row per browser. The same person on a phone
// and a laptop gets two, and both are notified.
func (a *API) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	a.savePushSubscription(w, r, a.owner(r))
}

// Delivery staff have a staff session rather than a shopper session. Store
// their browser under a namespaced subject so an accepted order can wake the
// rider who was assigned without exposing their phone number as an account.
func (a *API) handleRiderPushSubscribe(w http.ResponseWriter, r *http.Request) {
	a.savePushSubscription(w, r, "rider:"+a.staffOf(r))
}

func (a *API) savePushSubscription(w http.ResponseWriter, r *http.Request, subject string) {
	var in subscription
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Token == "" {
		writeError(w, http.StatusBadRequest, "Firebase token is required")
		return
	}
	in.Endpoint = "fcm:" + in.Token
	// Re-subscribing with the same token refreshes it rather than duplicating.
	if _, err := a.db.sql.ExecContext(r.Context(), `
		INSERT INTO push_subscriptions (endpoint, email, p256dh, auth)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (endpoint) DO UPDATE SET
			email = EXCLUDED.email, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth`,
		in.Endpoint, subject, "", ""); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/push/subscribe — the browser unsubscribed, or the seller turned
// notifications off.
func (a *API) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Token == "" {
		writeError(w, http.StatusBadRequest, "Firebase token is required")
		return
	}
	if _, err := a.db.sql.ExecContext(r.Context(),
		`DELETE FROM push_subscriptions WHERE endpoint = $1 AND email = $2`,
		"fcm:"+in.Token, a.owner(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/push/test — proves the whole chain end to end: this backend, the
// push service, the browser, the service worker. The notification carries a
// confirm button, so the seller answering it is the proof it arrived.
func (a *API) handlePushTest(w http.ResponseWriter, r *http.Request) {
	if a.push == nil {
		writeError(w, http.StatusServiceUnavailable, "push is not configured")
		return
	}
	if a.push.fcm == nil {
		writeError(w, http.StatusServiceUnavailable, "Firebase service account is not configured")
		return
	}
	email := a.owner(r)
	subs, err := a.db.pushSubscriptions(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(subs) == 0 {
		writeError(w, http.StatusNotFound, "this browser is not subscribed yet")
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"title": "Notifications are on",
		"body":  "Tap below to confirm you got this.",
		"tag":   "lamazon-test",
		// The service worker turns this into a button on the notification.
		"confirm": true,
	})
	var sent int
	for _, s := range subs {
		if err := a.push.send(r.Context(), s, payload); err != nil {
			log.Printf("test push to %s: %v", email, err)
			if errors.Is(err, errSubscriptionGone) {
				a.db.sql.ExecContext(r.Context(),
					`DELETE FROM push_subscriptions WHERE endpoint = $1`, s.Endpoint)
			}
			continue
		}
		sent++
	}
	if sent == 0 {
		writeError(w, http.StatusBadGateway, "the push service would not take it")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent})
}

// notify reaches one person on every channel that is set up: email always,
// because it works on every phone with nothing installed, and a browser
// notification on top wherever they allowed one.
//
// Nothing here is fatal. A failed notification must never fail the order that
// triggered it, so problems are logged and the caller carries on.
func (a *API) notify(ctx context.Context, email, title, body string) {
	a.notifyEvent(ctx, email, title, body, "account", "/account")
}
func (a *API) notifyOrder(ctx context.Context, email, title, body string) {
	a.notifyEvent(ctx, email, title, body, "order", "/orders")
}
func (a *API) notifyOrderAt(ctx context.Context, email, title, body, target string) {
	a.notifyEvent(ctx, email, title, body, "order", target)
}

// notifyOrderLater makes order writes independent of third-party latency. An
// accepted order is committed before notifications begin, and email/FCM get a
// bounded background window instead of holding checkout or an action button.
func (a *API) notifyOrderLater(email, title, body, target string) {
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		a.notifyOrderAt(ctx, email, title, body, target)
	}
	if a.asyncNotifications {
		go run()
		return
	}
	run()
}

func (a *API) notifyEvent(ctx context.Context, email, title, body, kind, target string) {
	rider := strings.HasPrefix(email, "rider:")
	prefs, err := a.preferences(ctx, email)
	if err != nil {
		log.Printf("notification preferences: %v", err)
		return
	}
	if kind == "order" && !prefs.OrderUpdates || kind == "offer" && !prefs.EmailOffers {
		return
	}
	// Rider subjects are push-only identities (rider:<phone>), not email
	// addresses. Buyer and seller order events continue over both channels.
	if a.mail != nil && !rider {
		if err := a.mail.send(ctx, email, title, body, notifyHTML(title, body)); err != nil {
			log.Printf("notify %s by email: %v", email, err)
		}
	}
	if a.push == nil || !prefs.Push {
		return
	}

	subs, err := a.db.pushSubscriptions(ctx, email)
	if err != nil {
		log.Printf("notify %s: reading subscriptions: %v", email, err)
		return
	}

	payload, _ := json.Marshal(map[string]string{"title": title, "body": body, "url": target})
	for _, s := range subs {
		if err := a.push.send(ctx, s, payload); err != nil {
			log.Printf("notify %s by push: %v", email, err)
			// A browser that has thrown the subscription away answers 404 or
			// 410 forever. Dropping it keeps the table from filling with dead
			// endpoints that are retried on every order.
			if errors.Is(err, errSubscriptionGone) {
				a.db.sql.ExecContext(ctx,
					`DELETE FROM push_subscriptions WHERE endpoint = $1`, s.Endpoint)
			}
		}
	}
}

var errSubscriptionGone = errors.New("subscription no longer exists")

func (p *Push) send(ctx context.Context, s subscription, payload []byte) error {
	token, ok := strings.CutPrefix(s.Endpoint, "fcm:")
	if !ok || token == "" {
		return errSubscriptionGone
	}
	if p.fcm == nil {
		return errors.New("Firebase Cloud Messaging is not configured")
	}
	return p.fcm.send(ctx, token, payload)
}

// FCM is the minimum needed for Firebase Cloud Messaging HTTP v1. Configure it
// with FIREBASE_CREDENTIALS_JSON or GOOGLE_APPLICATION_CREDENTIALS.
type FCM struct {
	projectID, clientEmail string
	privateKey             *rsa.PrivateKey
	http                   *http.Client
	token                  string
	tokenExpires           time.Time
	tokenURL               string
	messagingBaseURL       string
}

func fcmFromEnv() *FCM {
	raw := strings.TrimSpace(os.Getenv("FIREBASE_CREDENTIALS_JSON"))
	if raw == "" {
		if path := strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				log.Printf("Firebase credentials: %v", err)
				return nil
			}
			raw = string(b)
		}
	}
	if raw == "" {
		return nil
	}

	var in struct {
		ProjectID   string `json:"project_id"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		log.Printf("Firebase credentials JSON: %v", err)
		return nil
	}
	key, err := parsePrivateKey(in.PrivateKey)
	if err != nil {
		log.Printf("Firebase private key: %v", err)
		return nil
	}
	if in.ProjectID == "" || in.ClientEmail == "" {
		log.Print("Firebase credentials missing project_id or client_email")
		return nil
	}
	return &FCM{
		projectID: in.ProjectID, clientEmail: in.ClientEmail,
		privateKey: key, http: &http.Client{Timeout: 8 * time.Second},
		tokenURL:         "https://oauth2.googleapis.com/token",
		messagingBaseURL: "https://fcm.googleapis.com",
	}
}

func parsePrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("missing PEM block")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("key is not RSA")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func (f *FCM) send(ctx context.Context, token string, payload []byte) error {
	access, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return err
	}
	data := make(map[string]string, len(raw))
	for k, v := range raw {
		switch typed := v.(type) {
		case string:
			data[k] = typed
		case bool:
			if typed {
				data[k] = "true"
			} else {
				data[k] = "false"
			}
		default:
			data[k] = strings.TrimSpace(strings.ReplaceAll(strings.Trim(fmt.Sprint(typed), "\""), "\n", " "))
		}
	}
	// Data-only, deliberately. A "notification" block makes FCM display the
	// message itself, and that changes behaviour depending on whether the tab
	// happens to be focused: in the background Chrome draws its own
	// notification — without our confirm button and without our click data —
	// and in the foreground it suppresses display entirely. Sending data only
	// means our own handlers draw it every time, the same way, in both states.
	body, _ := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": token,
			"data":  data,
			"webpush": map[string]any{
				"headers": map[string]string{"TTL": "86400"},
			},
		},
	})

	endpoint := strings.TrimRight(f.messagingBaseURL, "/") + "/v1/projects/" +
		url.PathEscape(f.projectID) + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	res, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
		return errSubscriptionGone
	}
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return errors.New("FCM said " + strings.TrimSpace(res.Status+" "+string(b)))
	}
	return nil
}

func (f *FCM) accessToken(ctx context.Context) (string, error) {
	if f.token != "" && time.Now().Before(f.tokenExpires.Add(-time.Minute)) {
		return f.token, nil
	}
	now := time.Now()
	claims, _ := json.Marshal(map[string]any{
		"iss":   f.clientEmail,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   "https://oauth2.googleapis.com/token",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	assertion, err := f.signJWT(claims)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		f.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := f.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return "", errors.New("Google token endpoint said " + strings.TrimSpace(res.Status+" "+string(b)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("Google token endpoint returned no access token")
	}
	f.token = out.AccessToken
	f.tokenExpires = now.Add(time.Duration(out.ExpiresIn) * time.Second)
	return f.token, nil
}

func (f *FCM) signJWT(claims []byte) (string, error) {
	header := []byte(`{"alg":"RS256","typ":"JWT"}`)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.privateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// pushSubscriptions is every browser this address has registered — a phone and
// a laptop are two rows, and both get told.
func (d *DB) pushSubscriptions(ctx context.Context, email string) ([]subscription, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT endpoint FROM push_subscriptions WHERE email = $1`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []subscription
	for rows.Next() {
		var s subscription
		if err := rows.Scan(&s.Endpoint); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
