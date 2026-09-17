package shop

// Session plumbing. The backend mints token pairs (POST /api/login/verify and
// friends); this side carries them in cookies so the server-rendered pages can
// act for the shopper without JavaScript, and so /api/* proxied straight from
// the browser stays same-origin.
//
// The access token lives an hour (backend/auth.go accessLifetime) and the
// refresh token thirty days; the cookie lifetimes match so a stale cookie is
// never presented as if it were live.

import (
	"net/http"

	"lamazon/website/backend"
)

const (
	AccessCookie  = "lw_at"
	RefreshCookie = "lw_rt"
	// Where to land after signing in; set by gated pages.
	NextParam = "next"
)

func SetSessionCookies(w http.ResponseWriter, s backend.Session) {
	http.SetCookie(w, &http.Cookie{
		Name: AccessCookie, Value: s.Token, Path: "/",
		MaxAge: s.ExpiresIn, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: RefreshCookie, Value: s.RefreshToken, Path: "/",
		MaxAge: 30 * 24 * 60 * 60, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{AccessCookie, RefreshCookie} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
	}
}

// SessionTokens reads the pair out of the request.
func SessionTokens(r *http.Request) (access, refresh string) {
	if c, err := r.Cookie(AccessCookie); err == nil {
		access = c.Value
	}
	if c, err := r.Cookie(RefreshCookie); err == nil {
		refresh = c.Value
	}
	return access, refresh
}
