package site

import (
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"lamazon/website/backend"
	"lamazon/website/shop"
	"lamazon/website/static"
)

// The Uniminute storefront website: Go + templ + HTMX + Alpine + Tailwind.
//
// It renders every page server-side and calls the backend API for all data.
// The only state this side owns (cart, wishlist, session tokens) is in cookies.

const defaultAPIBase = "https://api.geltrax.engineer"

// APIBase is the backend this site calls: $API_BASE, else production.
func APIBase() string {
	if v := os.Getenv("API_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultAPIBase
}

// New is the whole site as one handler, shared by cmd/server and the Vercel function.
func New(apiBase string) http.Handler {
	site := &Site{backend: backend.NewBackend(apiBase), apiBase: apiBase}
	return recoverer(site.keepSession(freshForStaff(site.routes())))
}

type Site struct {
	backend *backend.Backend
	apiBase string
}

func (s *Site) routes() http.Handler {
	mux := http.NewServeMux()

	// --- pages -----------------------------------------------------------
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /shop", s.handleShopRedirect)
	mux.HandleFunc("GET /c/{name}", s.handleCollectionRedirect)
	mux.HandleFunc("GET /stores", s.handleStores)
	mux.HandleFunc("GET /store/{name}", s.handleStore)
	mux.HandleFunc("GET /p/{id}", s.handleProduct)
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /cart", s.handleCartPage)
	mux.HandleFunc("GET /checkout", s.handleCheckoutPage)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("GET /register", s.handleRegisterPage)
	mux.HandleFunc("GET /account", s.handleAccountPage)
	mux.HandleFunc("GET /addresses", s.handleAddressesPage)
	mux.HandleFunc("GET /addresses/new", s.handleAddressForm)
	mux.HandleFunc("GET /addresses/{id}/edit", s.handleAddressForm)
	mux.HandleFunc("GET /settings", s.handleSettingsPage)
	mux.HandleFunc("GET /orders", s.handleOrdersPage)
	mux.HandleFunc("GET /orders/placed", s.handleOrderPlaced)
	mux.HandleFunc("GET /orders/{id}", s.handleOrderDetail)
	mux.HandleFunc("GET /help", s.handleHelpPage)
	mux.HandleFunc("GET /admin", s.handleAdmin)
	mux.HandleFunc("GET /admin/{rest...}", s.handleAdminAlias)
	mux.HandleFunc("POST /admin/login", s.handleAdminLogin)
	mux.HandleFunc("POST /admin/logout", s.handleAdminLogout)
	mux.HandleFunc("GET /admin/export", s.handleAdminExport)
	mux.HandleFunc("GET /admin/banners/new", s.handleAdminBanner)
	mux.HandleFunc("GET /admin/banners/{id}", s.handleAdminBanner)
	mux.HandleFunc("GET /admin/policies/{slug}", s.handleAdminPolicy)
	mux.HandleFunc("GET /admin/stores/{owner}/photos", s.handleAdminStorePhotos)
	mux.Handle("/staff-api/admin/", staffProxy(s.apiBase, "admin", s.backend))
	mux.Handle("/staff-api/delivery/", staffProxy(s.apiBase, "rider", s.backend))
	mux.HandleFunc("GET /delivery", s.handleDelivery)
	mux.HandleFunc("POST /delivery/login", s.handleDeliveryLogin)
	mux.HandleFunc("POST /delivery/logout", s.handleDeliveryLogout)
	mux.HandleFunc("POST /delivery/orders/{id}/pick", s.handleDeliveryPick)
	mux.HandleFunc("POST /delivery/orders/{id}/deliver", s.handleDeliveryDeliver)
	mux.HandleFunc("GET /seller", s.handleSellerDashboard)
	mux.HandleFunc("GET /seller/store", s.handleSellerStoreForm)
	mux.HandleFunc("GET /seller/products/new", s.handleSellerProductForm)
	mux.HandleFunc("GET /seller/products/{id}", s.handleSellerProductForm)
	mux.HandleFunc("GET /compare/{id}", s.handleCompare)
	mux.HandleFunc("GET /notifications", s.handleNotificationsPage)
	mux.HandleFunc("GET /profile/setup", s.handleProfileSetupPage)
	mux.HandleFunc("POST /profile/setup", s.handleProfileSetup)
	mux.HandleFunc("GET /policies", s.handlePoliciesPage)
	mux.HandleFunc("GET /policy/{slug}", s.handlePolicyPage)
	mux.HandleFunc("GET /saved", s.handleSavedPage)

	// --- HTMX fragments and mutations ------------------------------------
	mux.HandleFunc("GET /fragments/home-products", s.handleHomeProducts)
	mux.HandleFunc("GET /fragments/products", s.handleProductsFragment)
	mux.HandleFunc("GET /fragments/search", s.handleSearchFragment)
	mux.HandleFunc("POST /cart/add", s.handleCartAdd)
	mux.HandleFunc("POST /cart/set", s.handleCartSet)
	mux.HandleFunc("POST /cart/remove", s.handleCartRemove)
	mux.HandleFunc("POST /wishlist/toggle", s.handleWishlistToggle)

	mux.HandleFunc("POST /login/start", s.handleLoginStart)
	mux.HandleFunc("POST /login/verify", s.handleLoginVerify)
	mux.HandleFunc("POST /login/password", s.handleLoginPassword)
	mux.HandleFunc("POST /logout", s.handleLogout)

	mux.HandleFunc("POST /account/profile", s.handleProfileUpdate)
	mux.HandleFunc("POST /account/preferences", s.handlePreferencesUpdate)
	mux.HandleFunc("POST /account/addresses", s.handleAddressCreate)
	mux.HandleFunc("POST /account/addresses/{id}", s.handleAddressUpdate)
	mux.HandleFunc("POST /account/addresses/{id}/default", s.handleAddressDefault)
	mux.HandleFunc("POST /account/addresses/{id}/delete", s.handleAddressDelete)

	mux.HandleFunc("POST /checkout", s.handleCheckout)
	mux.HandleFunc("POST /orders/{id}/cancel", s.handleOrderCancel)

	// --- static and proxy -------------------------------------------------
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFileServer()))
	mux.HandleFunc("GET /firebase-messaging-sw.js", firebaseMessagingWorker)

	// /api/* passes through untouched to the existing backend, with the
	// session cookie injected as the Bearer token the backend expects.
	mux.Handle("/api/", bearerProxy(s.apiBase, s.backend))

	mux.HandleFunc("/", s.handleNotFound)
	return mux
}

func firebaseMessagingWorker(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(static.Files, "js/firebase-messaging-sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Service-Worker-Allowed", "/")
	_, _ = w.Write(data)
}

func staticFileServer() http.Handler {
	files := http.FileServer(http.FS(static.Files))
	// Fonts, CSS, JS and vendored libraries never change under a running
	// binary; a month of immutability is safe and quiet.
	return cache(files, "public, max-age=2592000")
}

func cache(next http.Handler, value string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}

// bearerProxy forwards /api/* to the existing backend unchanged — same paths,
// same handlers — adding only the session cookie as the Authorization header
// the backend's withAuth middleware looks for.
// staffProxy is /staff-api/<role>/* passed through as /api/<role>/* with that
// staff panel's own token. The panels' pages call the backend's routes through
// it exactly as the app does, without the token ever reaching page script.
func staffProxy(apiBase, role string, b *backend.Backend) http.Handler {
	target, err := url.Parse(apiBase)
	if err != nil {
		log.Fatalf("API_BASE %q: %v", apiBase, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		staff, ok := shop.ReadStaff(r, role)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"Your session ended. Sign in again."}`))
			return
		}
		r.URL.Path = "/api" + strings.TrimPrefix(r.URL.Path, "/staff-api")
		r.URL.RawPath = ""
		r.Header.Set("Authorization", "Bearer "+staff.Token)
		r.Header.Del("Cookie")
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
		forgetOnWrite(b, r)
	})
}

// forgetOnWrite drops every cached read once a write has passed through a
// proxy: the server did not make the change, so it cannot tell what it touched.
func forgetOnWrite(b *backend.Backend, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		b.Forget("")
	}
}

func bearerProxy(apiBase string, b *backend.Backend) http.Handler {
	target, err := url.Parse(apiBase)
	if err != nil {
		log.Fatalf("API_BASE %q: %v", apiBase, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		access, _ := shop.SessionTokens(r)
		if access != "" && r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+access)
		}
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
		forgetOnWrite(b, r)
	})
}

// notProxied lists every /api prefix the website answers itself — there are
// none today; the site never re-implements a backend route.
var _ = strings.TrimSpace

// freshForStaff makes admin and seller screens read live data, never the cache.
func freshForStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; strings.HasPrefix(p, "/admin") || strings.HasPrefix(p, "/seller") {
			r = r.WithContext(backend.Fresh(r.Context()))
		}
		next.ServeHTTP(w, r)
	})
}

// keepSession swaps an expired access token for a new pair before the page is
// built, and saves the pair in the browser. Without saving it, the spent
// refresh token was all the next request had, and the shopper was signed out
// an hour after signing in.
func (s *Site) keepSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			if access, refresh := shop.SessionTokens(r); access == "" && refresh != "" {
				if sess, err := s.backend.Refresh(r.Context(), refresh); err == nil {
					shop.SetSessionCookies(w, sess)
					r = shop.WithSession(r, sess)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
