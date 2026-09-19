package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lamazon:lamazon@localhost:5433/lamazon?sslmode=disable"
	}
	db, err := OpenDB(dsn)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	// The admin account comes from the environment on every boot, so changing
	// ADMIN_PASSWORD and restarting is how the password is rotated.
	if err := db.seedAdmin(context.Background()); err != nil {
		log.Fatalf("admin: %v", err)
	}
	if os.Getenv("ADMIN_USER") == "" {
		log.Print("ADMIN_USER/ADMIN_PASSWORD unset: no admin can sign in")
	}

	// The same deal for one shopper account, so there is always an address
	// that can sign in without an emailed code.
	if err := db.seedUser(context.Background()); err != nil {
		log.Fatalf("seed user: %v", err)
	}

	// Each policy fills in only if it has no row, so an admin's edits survive
	// every restart and a document added later still arrives written.
	if err := db.seedPolicies(context.Background()); err != nil {
		log.Fatalf("policies: %v", err)
	}

	mail := mailerFromEnv()
	if mail == nil {
		log.Print("RESEND_API_KEY/EMAIL_SEND unset: sign-in codes go to this log")
	}

	push := pushFromEnv()
	if push == nil {
		log.Print("push credentials unset: notifications go by email only")
	} else if push.fcm == nil {
		log.Print("Firebase service account unset: browsers can subscribe, but notifications go by email only")
	}

	cloud := cloudinaryFromEnv()
	if cloud == nil {
		log.Print("CLOUDINARY_* unset: photo uploads will return 503")
	}

	api := &API{db: db, cloud: cloud, mail: mail, push: push, asyncNotifications: true}
	go api.reviewReminders(context.Background())

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	// Timeouts so one stalled client cannot hold a connection (and its pooled
	// database conn) open forever.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           routes(api),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Uniminute API on :%s, Postgres ready", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// commit is the git revision this binary was built from, which the Go
// toolchain stamps in by itself whenever it builds inside a checkout — so
// there is no version to bump and nothing to pass at build time.
//
// "unknown" is honest rather than broken: a `go build` outside a repository,
// or with -buildvcs=false, genuinely does not know.
func commit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 7 {
				return s.Value[:7] // what `git log --oneline` shows
			}
			return s.Value
		}
	}
	return "unknown"
}

// routes wires every endpoint. Go 1.22 pattern routing, so no router
// dependency. ponytail: one mux, no middleware stack until there is a
// second cross-cutting concern.
func routes(s *API) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ok",
			// Which commit is actually answering. Otherwise "did the deploy
			// land?" is a question about a binary on a box nobody can see,
			// and the honest check — grep the sha out of the file over ssh —
			// is wrong the moment a frontend-only commit means backend/ did
			// not deploy at all.
			"commit": commit(),
		})
	})

	// Catalog
	mux.HandleFunc("GET /api/policies", s.handlePolicies)
	mux.HandleFunc("GET /api/charges", s.handleCharges)
	mux.HandleFunc("GET /api/products/{id}/reviews", s.handleProductReviews)
	mux.HandleFunc("GET /api/orders/reviews", s.handleMyReviews)
	mux.HandleFunc("GET /api/notifications", s.handleNotifications)
	mux.HandleFunc("POST /api/notifications/read", s.handleNotificationsRead)
	mux.HandleFunc("PUT /api/orders/{id}/review", s.handleSaveReview)
	mux.HandleFunc("GET /api/categories", s.handleCategories)
	mux.HandleFunc("GET /api/campaigns", s.handleCampaigns)
	mux.HandleFunc("GET /api/compare-groups", s.handleCompareGroups)
	mux.HandleFunc("GET /api/compare", s.handleCompare)
	mux.HandleFunc("GET /api/products", s.handleProducts)
	mux.HandleFunc("GET /api/products/{id}", s.handleProduct)
	mux.HandleFunc("GET /api/shops", s.handleShops)
	mux.HandleFunc("GET /api/shops/{name}/products", s.handleShopProducts)

	// Delivery area
	// Public: it decides what colour the shop is, so it is read before
	// anyone signs in.
	mux.HandleFunc("GET /api/storefront/season", s.handleActiveSeason)
	mux.HandleFunc("GET /api/locations", handleLocations)
	mux.HandleFunc("GET /api/locations/check", handleLocationCheck)

	// Accounts
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/login/verify", s.handleVerifyCode)
	mux.HandleFunc("POST /api/login/password", s.handlePasswordLogin)
	mux.HandleFunc("POST /api/login/reset", s.handleResetPassword)
	mux.HandleFunc("POST /api/login/refresh", s.handleRefresh)

	// Account: who is signed in, and where they want things delivered.
	mux.HandleFunc("GET /api/me", s.handleMe)
	mux.HandleFunc("GET /api/preferences", s.handlePreferences)
	mux.HandleFunc("PATCH /api/preferences", s.handlePreferences)
	mux.HandleFunc("PATCH /api/me", s.handleUpdateMe)
	mux.HandleFunc("GET /api/addresses", s.handleAddresses)
	mux.HandleFunc("POST /api/addresses", s.handleAddAddress)
	mux.HandleFunc("PATCH /api/addresses/{id}", s.handleAddAddress)
	mux.HandleFunc("DELETE /api/addresses/{id}", s.handleDeleteAddress)
	mux.HandleFunc("PATCH /api/addresses/{id}/default", s.handleDefaultAddress)

	// Notifications
	mux.HandleFunc("GET /api/push/key", s.handlePushKey)
	mux.HandleFunc("GET /api/push/config", s.handlePushConfig)
	mux.HandleFunc("POST /api/push/subscribe", s.handlePushSubscribe)
	mux.HandleFunc("DELETE /api/push/subscribe", s.handlePushUnsubscribe)
	mux.HandleFunc("POST /api/push/test", s.handlePushTest)

	// Seller
	// Same tree the shop navigates by. It used to be a separate hardcoded
	// list, which is how a seller ended up able to list under "Stationary"
	// when no tab had ever shown one.
	mux.HandleFunc("GET /api/seller/categories", s.handleCategories)
	mux.HandleFunc("POST /api/seller/store", s.handleCreateStore)
	mux.HandleFunc("POST /api/seller/store/photo", s.handleStorePhoto)
	mux.HandleFunc("GET /api/seller/store", s.handleGetStore)
	mux.HandleFunc("GET /api/seller/items", s.handleItems)
	mux.HandleFunc("POST /api/seller/items", s.handleAddItem)
	mux.HandleFunc("PATCH /api/seller/items/{id}", s.handleUpdateItem)
	mux.HandleFunc("PATCH /api/seller/items/{id}/stock", s.handlePatchStock)
	mux.HandleFunc("PATCH /api/seller/items/{id}/listing", s.handlePatchListing)
	mux.HandleFunc("POST /api/seller/items/{id}/photos", s.handleItemPhotos)
	mux.HandleFunc("PUT /api/seller/items/{id}/photos", s.handleReorderItemPhotos)
	mux.HandleFunc("DELETE /api/seller/items/{id}", s.handleDeleteItem)
	mux.HandleFunc("GET /api/seller/orders", s.handleOrders)
	mux.HandleFunc("POST /api/seller/orders/{id}/accept", s.handleAcceptOrder)
	mux.HandleFunc("POST /api/seller/orders/{id}/reject", s.handleRejectOrder)
	mux.HandleFunc("POST /api/seller/orders/{id}/deliver", s.handleDeliverOrder)

	// Buyer. /api/seller/orders used to take the POST as well; it stays as an
	// alias so an app mid-update keeps working.
	mux.HandleFunc("POST /api/orders", s.handlePlaceOrder)
	mux.HandleFunc("POST /api/orders/checkout", s.handleCheckout)
	mux.HandleFunc("POST /api/seller/orders", s.handlePlaceOrder)
	mux.HandleFunc("GET /api/orders", s.handleMyOrders)
	mux.HandleFunc("POST /api/orders/{id}/cancel", s.handleCancelOrder)

	// Admin. Everything but the login needs an admin token, checked in
	// withStaff below.
	mux.HandleFunc("POST /api/admin/login", s.handleAdminLogin)
	mux.HandleFunc("GET /api/admin/overview", s.handleAdminOverview)
	mux.HandleFunc("GET /api/admin/campaigns", s.handleCampaigns)
	mux.HandleFunc("GET /api/admin/seasons", s.handleAdminSeasons)
	mux.HandleFunc("PUT /api/admin/seasons/{id}", s.handleSaveSeason)
	mux.HandleFunc("DELETE /api/admin/seasons/{id}", s.handleDeleteSeason)
	mux.HandleFunc("PUT /api/admin/campaigns/{id}", s.handleSaveCampaign)
	mux.HandleFunc("DELETE /api/admin/campaigns/{id}", s.handleDeleteCampaign)
	mux.HandleFunc("POST /api/admin/campaign-photos", s.handleCampaignPhoto)
	mux.HandleFunc("POST /api/admin/campaign-media", s.handleCampaignMedia)
	mux.HandleFunc("GET /api/admin/insights", s.handleAdminInsights)
	mux.HandleFunc("PUT /api/admin/policies/{slug}", s.handleSavePolicy)
	mux.HandleFunc("PUT /api/admin/charges", s.handleSaveCharges)
	mux.HandleFunc("GET /api/admin/reviews", s.handleAdminReviews)
	mux.HandleFunc("GET /api/admin/policies", s.handlePolicies)
	mux.HandleFunc("POST /api/admin/categories", s.handleAddCategory)
	mux.HandleFunc("POST /api/admin/compare-groups", s.handleSaveCompareGroup)
	mux.HandleFunc("DELETE /api/admin/compare-groups/{name}", s.handleDeleteCompareGroup)
	mux.HandleFunc("DELETE /api/admin/categories/{name}", s.handleDeleteCategory)
	mux.HandleFunc("POST /api/admin/categories/{name}/photo", s.handleCategoryPhoto)
	mux.HandleFunc("GET /api/admin/items", s.handleAdminItems)
	mux.HandleFunc("DELETE /api/admin/items/{id}", s.handleAdminDeleteItem)
	mux.HandleFunc("PATCH /api/admin/items/{id}/listing", s.handleAdminPatchListing)
	mux.HandleFunc("PATCH /api/admin/items/{id}/stock", s.handleAdminPatchStock)
	mux.HandleFunc("POST /api/admin/items/{id}/photos", s.handleItemPhotos)
	mux.HandleFunc("PUT /api/admin/items/{id}/photos", s.handleReorderItemPhotos)
	mux.HandleFunc("GET /api/admin/stores", s.handleAdminStores)
	mux.HandleFunc("PATCH /api/admin/stores/{owner}/categories", s.handleStoreCategories)
	mux.HandleFunc("POST /api/admin/stores/{owner}/approve", s.handleApproveStore)
	mux.HandleFunc("POST /api/admin/stores/{owner}/reject", s.handleRejectStore)
	mux.HandleFunc("GET /api/admin/orders", s.handleAdminOrders)
	mux.HandleFunc("POST /api/admin/orders/{id}/assign", s.handleAssignOrder)
	mux.HandleFunc("GET /api/admin/riders", s.handleListRiders)
	mux.HandleFunc("POST /api/admin/riders", s.handleAddRider)
	mux.HandleFunc("POST /api/admin/riders/{phone}/pin", s.handleResetRiderPIN)
	mux.HandleFunc("POST /api/admin/riders/{phone}/number", s.handleChangeRiderNumber)
	mux.HandleFunc("POST /api/admin/riders/{phone}/on", s.handleRestoreRider)
	mux.HandleFunc("DELETE /api/admin/riders/{phone}", s.handleRemoveRider)

	// Delivery panel.
	mux.HandleFunc("POST /api/delivery/login", s.handleRiderLogin)
	mux.HandleFunc("POST /api/delivery/push/subscribe", s.handleRiderPushSubscribe)
	mux.HandleFunc("GET /api/delivery/orders", s.handleRiderOrders)
	mux.HandleFunc("GET /api/delivery/history", s.handleRiderHistory)
	mux.HandleFunc("POST /api/delivery/orders/{id}/pick", s.handleRiderPick)
	mux.HandleFunc("POST /api/delivery/orders/{id}/deliver", s.handleRiderDeliver)

	// The shop window. Plain HTML, no session, no JavaScript — the pages a
	// stranger lands on, which the Flutter bundle is far too heavy to serve.
	// Everything behind a sign-in stays in the app at /app.
	mux.HandleFunc("GET /", s.handleStoreHome)
	mux.HandleFunc("GET /p/{id}", s.handleStoreProduct)
	mux.HandleFunc("GET /search", s.handleStoreSearch)
	mux.HandleFunc("GET /login", s.handleStoreLogin)
	mux.HandleFunc("GET /app", s.handleAppRedirect)
	mux.HandleFunc("GET /app/{rest...}", s.handleAppRedirect)

	return withCORS(withGzip(s.withStaff(s.withAuth(mux))))
}

// Everything under /api/seller/ belongs to one signed-in address, so the
// token is checked once here instead of in a dozen handlers.
const sellerPrefix = "/api/seller/"

// Subscribing files a browser under one address, so it needs a session too.
// The public key itself is not a secret and stays open.
func needsSession(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, sellerPrefix) ||
		strings.HasPrefix(r.URL.Path, "/api/orders") ||
		r.URL.Path == "/api/me" ||
		r.URL.Path == "/api/preferences" ||
		strings.HasPrefix(r.URL.Path, "/api/notifications") ||
		strings.HasPrefix(r.URL.Path, "/api/addresses") ||
		r.URL.Path == "/api/push/subscribe" ||
		r.URL.Path == "/api/push/test"
}

// withStaff guards the admin and delivery panels. They are not shopper
// sessions: an admin signs in with a password and a rider with a PIN, and the
// token carries which of the two it is, so an admin token cannot close a
// delivery and a rider token cannot approve a store.
func (s *API) withStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var role string
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/admin/"):
			role = "admin"
		case strings.HasPrefix(r.URL.Path, "/api/delivery/"):
			role = "rider"
		default:
			next.ServeHTTP(w, r)
			return
		}
		// The two login endpoints are how a token is got in the first place.
		if strings.HasSuffix(r.URL.Path, "/login") || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "sign in first")
			return
		}
		subject, err := s.db.staffSubject(r.Context(), role, strings.TrimSpace(token))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired — sign in again")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), staffKey{}, subject)))
	})
}

type ownerKey struct{}

func (s *API) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !needsSession(r) || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			// This guards orders and addresses as well as the seller routes,
			// so it cannot talk about a store: a shopper reading their own
			// order history has never opened one.
			writeError(w, http.StatusUnauthorized, "sign in to see this")
			return
		}
		email, err := s.db.sessionEmail(r.Context(), strings.TrimSpace(token))
		if err != nil {
			// Expired and forged look the same from here; the app answers both
			// by refreshing, and signs in again if that fails too. "Expired"
			// would be a guess — and wrong for the forged half.
			writeError(w, http.StatusUnauthorized, "sign in again to continue")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, email)))
	})
}

// maxBody caps a request payload. Every endpoint here posts a small JSON
// object, so anything larger is a mistake or an attack.
const maxBody = 1 << 20 // 1 MiB

// maxUpload is the ceiling for a whole multipart request — several photos at
// maxPhoto each, plus the fields around them.
const maxUpload = 60 << 20

// The Flutter web build is served from another origin, so browsers preflight
// anything that is not a plain GET.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods",
			"GET, POST, PATCH, DELETE, OPTIONS")
		// Authorization has to be listed or the browser refuses the preflight
		// and the real request never leaves — which looks like the API being
		// down, since the app catches the failure and falls back.
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// JSON bodies are tiny; multipart carries photos and gets the bigger
		// ceiling, with each file checked against maxPhoto once parsed.
		cap := int64(maxBody)
		if isMultipart(r) {
			cap = maxUpload
		}
		r.Body = http.MaxBytesReader(w, r.Body, cap)
		next.ServeHTTP(w, r)
	})
}

// withGzip compresses GET responses, which are the big ones — the catalog is
// mostly repeated field names and URLs, so it shrinks by roughly 80%.
// ponytail: GET only, so there is no empty-body-with-Content-Encoding case to
// reason about on 204s.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet ||
			!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(gzipWriter{ResponseWriter: w, gz: gz}, r)
	})
}

type gzipWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g gzipWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

var emailPattern = regexp.MustCompile(`^[\w.+-]+@[\w-]+\.[\w.-]+$`)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("encode: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
