package shop

// Staff sign-ins — the admin panel and the delivery panel — kept apart from
// the shopper's session the way the app's StaffSession is: different people
// with different tokens, one HttpOnly cookie per role, so signing in as a
// rider in the same browser does not sign the admin out.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
)

type Staff struct {
	Token   string `json:"token"`
	Subject string `json:"subject"` // the admin's username, or the rider's phone
	Name    string `json:"name"`
}

func staffCookie(role string) string { return "lw_staff_" + role }

func SetStaff(w http.ResponseWriter, role string, s Staff) {
	buf, _ := json.Marshal(s)
	WriteCookie(w, staffCookie(role), base64.RawURLEncoding.EncodeToString(buf))
}

func ReadStaff(r *http.Request, role string) (Staff, bool) {
	var s Staff
	c, err := r.Cookie(staffCookie(role))
	if err != nil {
		return s, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || json.Unmarshal(raw, &s) != nil || s.Token == "" {
		return Staff{}, false
	}
	return s, true
}

func ClearStaff(w http.ResponseWriter, role string) {
	http.SetCookie(w, &http.Cookie{
		Name: staffCookie(role), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}
