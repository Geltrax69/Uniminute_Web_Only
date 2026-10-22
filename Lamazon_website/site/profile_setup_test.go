package site

import (
	"encoding/json"
	"fmt"
	"lamazon/website/shop"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProfileSetupSavesCardAndAddressTogether(t *testing.T) {
	var update map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/me":
			if r.Method == "PATCH" {
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("missing owner token")
				}
				json.NewDecoder(r.Body).Decode(&update)
			}
			fmt.Fprint(w, `{"email":"buyer@example.com","ready":false}`)
		case "/api/storefront/season":
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected call: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	form := url.Values{"name": {"Buyer"}, "phone": {"9876543210"}, "line": {"Room 214"}, "city": {"LPU"}, "label": {"Home"}, "avatar": {"gym-female"}, "setupRequestId": {"fixture-profile-12345"}, "next": {"//evil.example"}}
	r := httptest.NewRequest("POST", "/profile/setup", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	r.AddCookie(&http.Cookie{Name: shop.AccessCookie, Value: "fixture"})
	w := httptest.NewRecorder()
	New(api.URL).ServeHTTP(w, r)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out["saved"] != true || out["redirect"] != "/" {
		t.Fatalf("save response: %d %s", w.Code, w.Body.String())
	}
	if update["avatar"] != "gym-female" || update["setupAddress"].(map[string]any)["line"] != "Room 214" {
		t.Fatal("card and address not sent together")
	}
}
func TestProfileSetupJSONRejectsGuest(t *testing.T) {
	r := httptest.NewRequest("POST", "/profile/setup", strings.NewReader("name=Buyer"))
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	New("http://127.0.0.1:1").ServeHTTP(w, r)
	if w.Code != 401 || strings.Contains(w.Body.String(), `"saved":true`) {
		t.Fatal("guest setup reported success")
	}
}
