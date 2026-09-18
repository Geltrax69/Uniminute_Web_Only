package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A fake FCM service: it grants a token, accepts messages, and records what
// the backend sent over the wire.
type fakeFCMService struct {
	*httptest.Server
	mu    sync.Mutex
	calls int
	auth  string
}

func newFakeFCMService(t *testing.T) *fakeFCMService {
	t.Helper()
	f := &fakeFCMService{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			writeJSON(w, http.StatusOK, map[string]any{
				"access_token": "stub-access-token",
				"expires_in":   3600,
			})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls++
		f.auth = r.Header.Get("Authorization")
		writeJSON(w, http.StatusOK, map[string]string{"name": "stub"})
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeFCMService) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// notifyAPI is the handler with both channels stubbed, plus the two stubs.
func notifyAPI(t *testing.T) (http.Handler, *sentMail, *fakeFCMService) {
	t.Helper()
	sent := &sentMail{}
	mailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		sent.to, sent.text = body.To[0], body.Subject+"\n"+body.Text
		sent.count++
		w.Write([]byte(`{"id":"stub"}`))
	}))
	t.Cleanup(mailSrv.Close)

	fcm := newFakeFCMService(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return routes(&API{
		cloud: fixtureCloud(t),
		db:    testDB(t),
		mail: &Mailer{
			key: "k", from: "auth@simpedu.in", http: mailSrv.Client(), base: mailSrv.URL,
		},
		push: &Push{
			publicKey: "firebase-web-push-public-key",
			fcm: &FCM{
				projectID: "messages-34023", clientEmail: "firebase@example.com",
				privateKey: key, http: fcm.Client(), tokenURL: fcm.URL + "/token",
				messagingBaseURL: fcm.URL,
			},
		},
	}), sent, fcm
}

// A placed order reaches the seller and buyer by email, with push wherever
// the account allowed it.
func TestPlacedOrderNotifiesSellerAndBuyer(t *testing.T) {
	h, sent, fcm := notifyAPI(t)

	openApprovedStore(t, h, map[string]any{
		"name": "Farm", "location": "Block 32", "city": "LPU",
		"categories": []string{"Grocery"},
	})
	addRider(t, h, adminSignIn(t, h), "9876543210")
	somewhereToDeliver(t, h)
	_, item := call(t, h, http.MethodPost, "/api/seller/items",
		map[string]any{"title": "Straubery", "price": 120, "stock": 25})

	sent.count = 0 // the sign-in code and the approval notice are not this test
	code, _ := call(t, h, http.MethodPost, "/api/push/subscribe", map[string]any{
		"token": "firebase-token-abc",
	})
	if code != http.StatusNoContent {
		t.Fatalf("subscribe: want 204, got %d", code)
	}

	if code, _ := call(t, h, http.MethodPost, "/api/seller/orders",
		map[string]any{"itemId": item["id"], "units": 2, "expectedTotal": item["price"].(float64)*2 + 15}); code != http.StatusCreated {
		t.Fatalf("place order: want 201, got %d", code)
	}

	if sent.count != 2 {
		t.Fatalf("seller and buyer should each get an email, got %d", sent.count)
	}
	if sent.to != DefaultOwner {
		t.Fatalf("email went to %s", sent.to)
	}
	for _, want := range []string{"order", "2 × Straubery", "Farm", "waiting"} {
		if !strings.Contains(sent.text, want) {
			t.Fatalf("email missing %q:\n%s", want, sent.text)
		}
	}
	if fcm.count() != 2 {
		t.Fatalf("want seller and buyer push deliveries, got %d", fcm.count())
	}
	if fcm.auth != "Bearer stub-access-token" {
		t.Fatalf("push was not FCM-authorized: %q", fcm.auth)
	}
}

// Push being unconfigured must not cost the seller their email.
func TestEmailStillSendsWithoutPush(t *testing.T) {
	h, sent := mailAPI(t)
	openApprovedStore(t, h, map[string]any{
		"name": "Farm", "location": "L", "city": "LPU", "categories": []string{"Grocery"},
	})
	addRider(t, h, adminSignIn(t, h), "9876543210")
	somewhereToDeliver(t, h)
	_, item := call(t, h, http.MethodPost, "/api/seller/items",
		map[string]any{"title": "Straubery", "price": 120, "stock": 5})
	sent.count = 0 // the sign-in code and the approval notice do not count here

	call(t, h, http.MethodPost, "/api/seller/orders",
		map[string]any{"itemId": item["id"], "units": 1, "expectedTotal": item["price"].(float64)*1 + 15})
	if sent.count != 2 {
		t.Fatalf("seller and buyer email should still go out with push off, got %d", sent.count)
	}
}

// Subscribing is per-person, so it needs a session like the seller routes.
func TestPushSubscribeNeedsASession(t *testing.T) {
	h := testAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/push/subscribe",
		strings.NewReader(`{"token":"firebase-token-abc"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("subscribe without a token: want 401, got %d", rec.Code)
	}
}

func TestPushSubscribeAcceptsFirebaseToken(t *testing.T) {
	h, _, _ := notifyAPI(t)

	code, _ := call(t, h, http.MethodPost, "/api/push/subscribe", map[string]any{
		"token": "firebase-token-abc",
	})
	if code != http.StatusNoContent {
		t.Fatalf("subscribe FCM token: want 204, got %d", code)
	}
}

func TestPushConfigExposesOnlyBrowserSettings(t *testing.T) {
	h := routes(&API{db: testDB(t), push: &Push{
		publicKey: "vapid-public",
		webConfig: map[string]string{
			"apiKey": "browser-key", "projectId": "uniminute",
			"messagingSenderId": "123", "appId": "app-123",
		},
	}})
	code, body := call(t, h, http.MethodGet, "/api/push/config", nil)
	if code != http.StatusOK || body["publicKey"] != "vapid-public" {
		t.Fatalf("push config: %d %v", code, body)
	}
	config := body["firebase"].(map[string]any)
	if config["projectId"] != "uniminute" || config["privateKey"] != nil {
		t.Fatalf("unexpected browser config: %v", config)
	}
}

func TestRiderCanSubscribeForAssignments(t *testing.T) {
	h, sent, _ := notifyAPI(t)
	admin := adminSignIn(t, h)
	pin := addRider(t, h, admin, "9876543210")
	_, login := callAs(t, h, "", http.MethodPost, "/api/delivery/login", map[string]string{
		"phone": "9876543210", "pin": pin,
	})
	code, body := callAs(t, h, login["token"].(string), http.MethodPost,
		"/api/delivery/push/subscribe", map[string]string{"token": "rider-fcm-token"})
	if code != http.StatusNoContent {
		t.Fatalf("rider subscribe: %d %v", code, body)
	}
	var subject string
	if err := lastTestDB.sql.QueryRow(`SELECT email FROM push_subscriptions WHERE endpoint='fcm:rider-fcm-token'`).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	if subject != "rider:9876543210" {
		t.Fatalf("subscription filed under %q", subject)
	}
	before := sent.count
	(&API{db: lastTestDB, mail: &Mailer{}}).notifyOrderLater("rider:9876543210", "Assigned", "Collect it", "/delivery")
	if sent.count != before {
		t.Fatal("rider phone identity was incorrectly sent to the email provider")
	}
}

func TestPushKeyWorksBeforeServerSendCredentials(t *testing.T) {
	h := routes(&API{
		db:   testDB(t),
		push: &Push{publicKey: "firebase-web-push-public-key"},
	})

	code, body := call(t, h, http.MethodGet, "/api/push/key", nil)
	if code != http.StatusOK {
		t.Fatalf("push key: want 200, got %d", code)
	}
	if body["publicKey"] != "firebase-web-push-public-key" {
		t.Fatalf("wrong public key: %v", body["publicKey"])
	}
}

func TestPushTestReportsMissingServerSendCredentials(t *testing.T) {
	h := routes(&API{
		db:   testDB(t),
		push: &Push{publicKey: "firebase-web-push-public-key"},
	})

	code, body := call(t, h, http.MethodPost, "/api/push/test", nil)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("test push without FCM credentials: want 503, got %d", code)
	}
	if body["error"] != "Firebase service account is not configured" {
		t.Fatalf("wrong error: %v", body["error"])
	}
}

// The test notification is the one the seller answers, so it has to carry the
// confirm flag the service worker turns into a button.
func TestPushTestSendsAConfirmableNotification(t *testing.T) {
	h, _, fcm := notifyAPI(t)

	// Nothing subscribed yet: say so rather than pretending it went.
	if code, body := call(t, h, http.MethodPost, "/api/push/test", nil); code != http.StatusNotFound {
		t.Fatalf("test before subscribing: want 404, got %d (%v)", code, body["error"])
	}

	call(t, h, http.MethodPost, "/api/push/subscribe", map[string]any{
		"token": "firebase-token-abc",
	})

	code, body := call(t, h, http.MethodPost, "/api/push/test", nil)
	if code != http.StatusOK {
		t.Fatalf("test push: want 200, got %d (%v)", code, body["error"])
	}
	if body["sent"].(float64) != 1 {
		t.Fatalf("want one delivery, got %v", body["sent"])
	}
	if fcm.count() != 1 {
		t.Fatalf("push service saw %d requests", fcm.count())
	}
}

// FCM must be sent data-only. A "notification" block makes Chrome draw the
// message itself — without our confirm button, and only when the tab is in
// the background — which is why the first attempt never showed anything.
func TestFCMSendsDataOnly(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") { // the OAuth exchange
			w.Write([]byte(`{"access_token":"stub","expires_in":3600}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"name":"projects/p/messages/1"}`))
	}))
	t.Cleanup(srv.Close)

	f := &FCM{
		projectID: "p", clientEmail: "svc@p.iam", tokenURL: srv.URL + "/token",
		messagingBaseURL: srv.URL, http: srv.Client(), privateKey: testRSAKey(t),
	}
	payload, _ := json.Marshal(map[string]any{
		"title": "Notifications are on", "body": "Tap below.", "confirm": true,
	})
	if err := f.send(context.Background(), "device-token", payload); err != nil {
		t.Fatalf("send: %v", err)
	}

	msg := got["message"].(map[string]any)
	if _, present := msg["notification"]; present {
		t.Fatal("message carries a notification block; Chrome will draw its own " +
			"without the confirm button and only when backgrounded")
	}
	data := msg["data"].(map[string]any)
	if data["title"] != "Notifications are on" || data["confirm"] != "true" {
		t.Fatalf("data payload is wrong: %v", data)
	}
}

// A throwaway key, generated per run — signing is real, the identity is not.
func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
