package main

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
)

// The shop window: plain server-rendered HTML, no framework and no build step.
//
// The Flutter app is the right tool for the parts behind a sign-in — carts,
// checkout, the seller and admin panels — but it is the wrong one for a page a
// stranger lands on. It ships its own renderer: about 2.7 MB of WebAssembly
// that a browser must download *and compile* before it can draw a single
// pixel. Measured on the live build, first visit, cache cold: 4.4s on a good
// phone, 8.1s on a mid-range one, 17.4s on a budget phone over slow 4G. Over
// half of mobile visitors leave before three.
//
// These pages have none of that. They are HTML and one inlined stylesheet, so
// the first paint is the first packet — and, unlike a canvas, a search engine
// can read the prices.
//
// Everything here is read-only and public. Nothing in this file touches a
// session, so there is no cookie to get wrong and no cache to poison.

//go:embed templates/*.html
var storefrontFiles embed.FS

var storefront = template.Must(
	template.New("").Funcs(storefrontFuncs).ParseFS(storefrontFiles, "templates/*.html"),
)

var storefrontFuncs = template.FuncMap{
	"money":    money,
	"thumb":    func(url string) string { return catalogueImage(url, 300) },
	"hero":     func(url string) string { return catalogueImage(url, 800) },
	"discount": discountPercent,
	"hasPrice": func(p Product) bool { return p.MRP > p.Price },
	// The app only puts a quick-add on things you buy without choosing a
	// size, which is food and groceries. Same rule here.
	"quickAdd": func(p Product) bool {
		return strings.EqualFold(p.Tab, "Food") || strings.EqualFold(p.Tab, "Grocery")
	},
	// Stock is a pointer because "not tracked" and "none left" are different
	// answers, and a template cannot follow one on its own.
	"deref": func(v *int) int {
		if v == nil {
			return 0
		}
		return *v
	},
}

// money writes a rupee amount the way it is read here: grouped in lakhs, and
// with the paise left off when there are none. Mirrors MoneyText in the app so
// a price does not change shape when Flutter takes over the page.
func money(v float64) string {
	paise := int64(v*100 + 0.5)
	whole, rem := paise/100, paise%100
	digits := fmt.Sprintf("%d", whole)
	if len(digits) > 3 {
		head, tail := digits[:len(digits)-3], digits[len(digits)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		digits = strings.Join(parts, ",") + "," + tail
	}
	if rem == 0 {
		return digits
	}
	return fmt.Sprintf("%s.%02d", digits, rem)
}

func discountPercent(p Product) int {
	if p.MRP <= p.Price {
		return 0
	}
	return int((p.MRP-p.Price)/p.MRP*100 + 0.5)
}

// catalogueImage asks the CDN for the size actually being drawn, in the same
// buckets the app uses so the two share a cache rather than doubling it.
func catalogueImage(url string, width int) string {
	const marker = "/image/upload/"
	if url == "" || !strings.Contains(url, marker) {
		return url
	}
	if strings.Contains(url, "c_fill") || strings.Contains(url, "c_pad") ||
		strings.Contains(url, "c_limit") {
		return url
	}
	return strings.Replace(url, marker, fmt.Sprintf(
		"%sc_fill,ar_1:1,g_auto,e_improve:30,w_%d,f_auto,q_auto/", marker, bucket(width)), 1)
}

func bucket(width int) int {
	for _, size := range []int{160, 300, 400, 800} {
		if width <= size {
			return size
		}
	}
	return 1200
}

type storePage struct {
	Title       string
	Description string
	Query       string
	Products    []Product
	Offers      []Product // discounted and in stock, for the "Around you" row
	Product     Product
	Departments []deptTile
	Banner      *Campaign
	BannerArt   string
	Canonical   string
	CartCount   int
	// Gate asks the browser to bounce a signed-out visitor to /login before
	// this page paints. Only the pages you land on to *shop* set it: a
	// product link somebody shared, and search, stay open so a stranger (and
	// a crawler) can still read a price.
	Gate bool
	// Bare drops the shop's furniture — the delivery-location picker, the
	// product search and the bottom bar. Sign-in is not a place in the shop
	// you can navigate away from; every one of those controls led back to a
	// page that would bounce you straight here again.
	Bare bool
}

func (a *API) render(w http.ResponseWriter, name string, page storePage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A shop window goes stale in minutes, not days.
	//
	// s-maxage is the one that matters: the origin is ~900ms away from an
	// Indian phone, so every request that reaches it undoes the point of
	// serving HTML. Cached at the edge it is tens of milliseconds, and
	// stale-while-revalidate means even the sixty-first second is served from
	// the POP while a fresh copy is fetched behind it. max-age stays low so a
	// price a shop just changed is not stuck in somebody's browser.
	w.Header().Set("Cache-Control",
		"public, max-age=30, s-maxage=60, stale-while-revalidate=600")
	if err := storefront.ExecuteTemplate(w, name, page); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// GET / — the shop window.
func (a *API) handleStoreHome(w http.ResponseWriter, r *http.Request) {
	// Anything other than the root is a product or a page that does not exist;
	// without this "/" would answer for every unmatched path.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	items, err := a.db.products(r.Context(), productFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.render(w, "home.html", storePage{
		Title:       "Uniminute — local shops, delivered on campus",
		Description: "Order from shops around campus. Real stock, real prices, cash on delivery.",
		Products:    items,
		Offers:      savingsOn(items),
		Departments: a.departments(r, items),
		Banner:      a.banner(r),
		BannerArt:   faceFor("", items),
		Canonical:   "/",
		Gate:        true,
	})
}

// GET /p/{id} — one product, readable by a person and by a crawler.
func (a *API) handleStoreProduct(w http.ResponseWriter, r *http.Request) {
	p, err := a.db.product(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	summary := p.Description
	if len(summary) > 150 {
		summary = summary[:150] + "…"
	}
	a.render(w, "product.html", storePage{
		Title:       p.Name + " — ₹" + money(p.Price) + " from " + p.Store,
		Description: summary,
		Product:     p,
		Canonical:   "/p/" + p.ID,
	})
}

// GET /search?q= — the same list the app searches, as a page.
func (a *API) handleStoreSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := a.db.products(r.Context(), productFilter{Q: q})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	title := "Search — Uniminute"
	if q != "" {
		title = q + " — Uniminute"
	}
	a.render(w, "search.html", storePage{
		Title:       title,
		Description: "Search the shops around campus.",
		Query:       q,
		Products:    items,
		Canonical:   "/search",
	})
}

// GET /login — the sign-in form, usable before the app could have booted.
//
// The only page here that needs JavaScript, because signing in is a
// conversation with the API rather than a document. The markup and the styling
// still arrive rendered, so the form is on screen and typeable in a couple of
// hundred milliseconds; the script only wakes up when somebody presses a
// button.
func (a *API) handleStoreLogin(w http.ResponseWriter, r *http.Request) {
	a.render(w, "login.html", storePage{
		Title:       "Log in — Uniminute",
		Description: "Sign in to order from shops around campus.",
		Canonical:   "/login",
		Bare:        true,
	})
}

// deptTile is a shelf and the picture it is shown under.
type deptTile struct {
	Name string
	Art  string
}

// departments are the top-level shelves. Failing to read them costs the strip,
// not the page.
//
// Almost none of them carry artwork of their own — the app falls back to a
// bundled atlas, which is a Flutter asset these pages cannot reach — so each
// tile borrows a photograph of something the shelf actually sells. That is
// what CategoryVisual does in the app for the same reason, and it is truer
// than a stock illustration: the tiles fill in as stock arrives.
func (a *API) departments(r *http.Request, items []Product) []deptTile {
	flat, err := a.db.categories(r)
	if err != nil {
		return nil
	}
	out := make([]deptTile, 0, len(flat))
	for _, c := range flat {
		if c.Parent != "" || c.Name == "All" {
			continue
		}
		art := c.ImageURL
		if art == "" {
			art = faceFor(c.Name, items)
		}
		out = append(out, deptTile{Name: c.Name, Art: art})
	}
	return out
}

// faceFor is the first photograph on a shelf worth showing: something in
// stock, with a picture. An empty tab means "anything in the shop".
func faceFor(tab string, items []Product) string {
	for _, p := range items {
		if p.ImageURL == "" || (p.AvailableStock != nil && *p.AvailableStock == 0) {
			continue
		}
		if tab == "" || strings.EqualFold(p.Tab, tab) {
			return p.ImageURL
		}
	}
	return ""
}

// banner is the campaign the app would be showing: the first enabled one.
func (a *API) banner(r *http.Request) *Campaign {
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT id, title, subtitle, cta, category, department, image_url, colour
		FROM storefront_campaigns WHERE enabled ORDER BY position, id LIMIT 1`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	if !rows.Next() {
		return nil
	}
	var c Campaign
	if err := rows.Scan(&c.ID, &c.Title, &c.Subtitle, &c.CTA, &c.Category,
		&c.Department, &c.ImageURL, &c.Colour); err != nil {
		return nil
	}
	return &c
}

// savingsOn is what "Around you" shows: a real discount on something the shop
// can actually sell today. Ten, because it is a row you swipe, not a page.
func savingsOn(items []Product) []Product {
	out := make([]Product, 0, 10)
	for _, p := range items {
		if p.MRP > p.Price && (p.AvailableStock == nil || *p.AvailableStock > 0) {
			out = append(out, p)
			if len(out) == 10 {
				break
			}
		}
	}
	return out
}

// GET /app — the application itself, which this storefront is the doorway to.
//
// In production Vercel answers /app with the Flutter build before the request
// reaches Go, so this only runs when the API is being browsed directly. It
// sends people somewhere real rather than showing them a 404.
func (a *API) handleAppRedirect(w http.ResponseWriter, r *http.Request) {
	target := os.Getenv("APP_URL")
	if target == "" {
		target = "https://lamazon-two.vercel.app/"
	}
	http.Redirect(w, r, target, http.StatusFound)
}
