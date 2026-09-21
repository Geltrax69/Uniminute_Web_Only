package site

// handlers.go — every HTTP handler the Site struct exposes. Each handler
// follows the same three-step pattern:
//   1. Build viewdata.Page (session + season + cart count).
//   2. Fetch the page-specific data from the backend.
//   3. Render the appropriate templ component.
//
// HTMX fragment handlers return partial HTML; full-page handlers return the
// complete document via one of the layout templates.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"lamazon/website/backend"
	productfrag "lamazon/website/templates/fragments/product"
	searchfrag "lamazon/website/templates/fragments/search"
	wishlistfrag "lamazon/website/templates/fragments/wishlist"

	cartfrag "lamazon/website/templates/fragments/cart"

	"lamazon/website/shop"
	"lamazon/website/templates/components/cart"
	"lamazon/website/templates/components/collection"
	"lamazon/website/templates/components/navigation"
	"lamazon/website/templates/components/ui"
	"lamazon/website/templates/layouts"
	"lamazon/website/templates/pages"
	"lamazon/website/tpl"
	"lamazon/website/viewdata"

	"github.com/a-h/templ"
)

// ── page helpers ─────────────────────────────────────────────────────────────

// buildPage constructs the chrome data that every page layout needs.
func (s *Site) buildPage(r *http.Request) viewdata.Page {
	access, refresh := shop.SessionTokens(r)
	expired := false

	// Try to refresh an expired access token silently.
	if access == "" && refresh != "" {
		if sess, err := s.backend.Refresh(r.Context(), refresh); err == nil {
			access = sess.Token
		} else if !backendDown(err) {
			expired = true
		}
	}

	p := viewdata.Page{
		CartCount:   shop.CartCount(r),
		Wishlist:    shop.ReadWishlist(r),
		AccessToken: access,
	}

	if access != "" {
		if u, err := s.backend.Me(r.Context(), access); err == nil {
			p.User = &u
		} else if !backendDown(err) {
			expired = true
		}
	}
	p.SessionExpired = expired && p.User == nil

	if season, err := s.backend.Season(r.Context()); err == nil {
		p.Season = season
	}

	// Default delivery address for signed-in shoppers.
	if p.User != nil && p.User.HasAddress {
		if addrs, err := s.backend.Addresses(r.Context(), access); err == nil {
			for i := range addrs {
				if addrs[i].Default {
					p.Address = &addrs[i]
					break
				}
			}
			if p.Address == nil && len(addrs) > 0 {
				p.Address = &addrs[0]
			}
		}
	}

	return p
}

// shopLayout builds the ShopData structure for pages that use layouts.Shop.
func (s *Site) shopLayout(r *http.Request, p viewdata.Page, content templ.Component) layouts.ShopData {
	cats, _ := s.backend.Categories(r.Context())
	camps, _ := s.backend.Campaigns(r.Context())
	cities, eta, _ := s.backend.Locations(r.Context())
	departments := buildDepartments(cats)
	return layouts.ShopData{
		Page:        p,
		Campaigns:   camps,
		Categories:  cats,
		Departments: departments,
		Cities:      cities,
		ETA:         eta,
		Content:     content,
	}
}

// render writes a templ component to the response writer.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		log.Printf("render: %v", err)
	}
}

// renderOK is render with 200.
func renderOK(w http.ResponseWriter, r *http.Request, c templ.Component) {
	render(w, r, http.StatusOK, c)
}

// isHTMX returns true when the request came from an HTMX hx-* attribute.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// addTrigger merges one event into the response's HX-Trigger header. HTMX
// fires it on the requesting element, and it bubbles to window listeners.
func addTrigger(w http.ResponseWriter, name string, detail any) {
	events := map[string]any{}
	if h := w.Header().Get("HX-Trigger"); h != "" {
		_ = json.Unmarshal([]byte(h), &events)
	}
	events[name] = detail
	b, _ := json.Marshal(events)
	w.Header().Set("HX-Trigger", string(b))
}

// toast is showAppSnack: a plain transient message.
func toast(w http.ResponseWriter, message string) {
	addTrigger(w, "lw:toast", map[string]any{"message": message})
}

func errorToast(w http.ResponseWriter, message string) {
	addTrigger(w, "lw:toast", map[string]any{"message": message, "tone": "error"})
}

func successToast(w http.ResponseWriter, message string) {
	addTrigger(w, "lw:toast", map[string]any{"message": message, "tone": "success"})
}

// addedToast is showAddedToast: a basket for food and grocery, a cart otherwise.
func addedToast(w http.ResponseWriter, p backend.Product) {
	basket := p.Tab == "Food" || p.Tab == "Grocery"
	addTrigger(w, "lw:toast", map[string]any{
		"added":   true,
		"basket":  basket,
		"title":   tpl.When(basket, "Added to Basket!", "Added to Cart!"),
		"message": p.Name,
	})
}

// cartCountTrigger updates the bottom bar's cart badge without a reload.
func cartCountTrigger(w http.ResponseWriter, count int) {
	addTrigger(w, "lw:cart:count", map[string]int{"count": count})
}

// redirect sends a full-page redirect, or HX-Redirect for HTMX requests.
func redirect(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// requireAuth redirects to /login if the user is not signed in.
func requireAuth(w http.ResponseWriter, r *http.Request, p viewdata.Page) bool {
	if p.User == nil {
		target := "/login?next=" + url.QueryEscape(r.URL.RequestURI())
		if p.SessionExpired {
			shop.ClearSessionCookies(w)
			target += "&expired=1"
		}
		redirect(w, r, target)
		return false
	}
	return true
}

// ── Department strip helper ───────────────────────────────────────────────────

func buildDepartments(cats []backend.Category) []navigation.Dept {
	// "All" is always first.
	depts := []navigation.Dept{{Name: "All"}}
	for _, c := range cats {
		if c.Parent == "" {
			d := navigation.Dept{Name: c.Name}
			if c.ImageURL != "" {
				d.Art = c.ImageURL
			}
			depts = append(depts, d)
		}
	}
	return depts
}

// sortProducts applies the sort param to a slice in place.
func sortProducts(products []backend.Product, by string) {
	switch by {
	case "price_asc":
		sort.Slice(products, func(i, j int) bool { return products[i].Price < products[j].Price })
	case "price_desc":
		sort.Slice(products, func(i, j int) bool { return products[i].Price > products[j].Price })
	case "name_asc":
		sort.Slice(products, func(i, j int) bool {
			return strings.ToLower(products[i].Name) < strings.ToLower(products[j].Name)
		})
	}
}

// paginate slices a product list; returns (page slice, current page, total pages).
func paginate(products []backend.Product, pageStr, _ string) ([]backend.Product, int, int) {
	total := len(products)
	pageNum, _ := strconv.Atoi(pageStr)
	if pageNum < 1 {
		pageNum = 1
	}
	const pageSize = collection.PageSize
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	start := (pageNum - 1) * pageSize
	if start >= total {
		start = 0
		pageNum = 1
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return products[start:end], pageNum, totalPages
}

// ── Page handlers ─────────────────────────────────────────────────────────────

func (s *Site) handleHome(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	// Someone who has never been here starts at sign-up; "Browse the shop" there lets them in.
	// ?browse=1 is "Browse the shop" on that page: always let it through, even
	// if the browser dropped the cookie that remembers the visit.
	if r.URL.Query().Has("browse") {
		markVisited(w)
	} else if p.User == nil && !visited(r) && r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/login?next=%2F", http.StatusSeeOther)
		return
	}
	p.Title = "Uniminute — Local shops, delivered"
	p.Description = "Browse and order from local campus shops. Real stock, real prices, cash on delivery."

	ctx := r.Context()
	cats, _ := s.backend.Categories(ctx)
	products, err := s.backend.Products(ctx, "", "", "")
	if maintenance(w, r, p, err) {
		return
	}
	shops, _ := s.backend.Shops(ctx, "")
	camps, err := s.backend.Campaigns(ctx)
	if err != nil {
		// Only an unreachable API gets the starter banner; an empty answer
		// means staff hid every banner on purpose.
		camps = []backend.Campaign{{ID: "everyday", Title: "Little joys. Everyday.",
			Subtitle: "Your local favourites, all in one place.", CTA: "Explore the collection",
			Colour: "#F2E8CE", Enabled: true}}
	}

	d := pages.HomeData{
		Page:      p,
		Tab:       "All",
		Faces:     map[string]string{},
		DeptOf:    map[string]string{},
		DeptIcons: map[string]string{},
	}
	for _, c := range cats {
		if c.Parent != "" {
			continue
		}
		d.Departments = append(d.Departments, c)
		d.DeptIcons[c.Name] = shop.DepartmentIcon(c.Name, c.Icon)
		indexCategories(d.DeptOf, c.Name, c)
		if c.Name == r.URL.Query().Get("tab") {
			d.Tab = c.Name
		}
	}
	inTab := func(tab string) bool { return d.Tab == "All" || tab == d.Tab }

	for _, pr := range products {
		if pr.ImageURL != "" && (pr.AvailableStock == nil || *pr.AvailableStock != 0) {
			if _, ok := d.Faces[pr.Category]; !ok {
				d.Faces[pr.Category] = pr.ImageURL
			}
		}
		if !inTab(pr.Tab) {
			continue
		}
		d.Scoped = append(d.Scoped, pr)
		if pr.Discounted() && (pr.AvailableStock == nil || *pr.AvailableStock != 0) && len(d.Offers) < 10 {
			d.Offers = append(d.Offers, pr)
		}
		if p.Wishlist[pr.ID] && len(d.Saved) < 10 {
			d.Saved = append(d.Saved, pr)
		}
	}
	for _, sh := range shops {
		if inTab(sh.Tab) {
			d.Shops = append(d.Shops, sh)
		}
	}
	for _, c := range camps {
		if c.Enabled && (d.Tab == "All" || c.Department == d.Tab || c.Category == d.Tab) {
			d.Campaigns = append(d.Campaigns, c)
		}
	}
	renderOK(w, r, pages.Home(d))
}

// indexCategories is departmentOf's table: every name under a department.
func indexCategories(out map[string]string, dept string, c backend.Category) {
	if _, ok := out[c.Name]; !ok {
		out[c.Name] = dept
	}
	for _, child := range c.Children {
		indexCategories(out, dept, child)
	}
}

// handleHomeProducts is "Show N more" on the home grid.
func (s *Site) handleHomeProducts(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	products, _ := s.backend.Products(r.Context(), "", "", "")
	var scoped []backend.Product
	for _, pr := range products {
		if tab == "" || tab == "All" || pr.Tab == tab {
			scoped = append(scoped, pr)
		}
	}
	if offset < 0 || offset > len(scoped) {
		offset = len(scoped)
	}
	end := min(offset+pages.HomePage, len(scoped))
	renderOK(w, r, pages.HomeProducts(scoped[offset:end], shop.ReadWishlist(r),
		shop.MixesStores(scoped), tab, end, len(scoped)))
}

func (s *Site) handleShop(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tab := q.Get("tab")
	category := q.Get("category")
	search := q.Get("q")
	sortBy := q.Get("sort")
	pageStr := q.Get("page")

	p := s.buildPage(r)
	p.Title = tabTitle(tab) + " — Uniminute"
	p.ActiveTab = tab
	p.Query = search

	products, _ := s.backend.Products(r.Context(), search, tab, category)
	sortProducts(products, sortBy)
	cats, _ := s.backend.Categories(r.Context())
	wishlist := p.Wishlist

	// Build base URL without page param so pagination links are clean.
	qBase := r.URL.Query()
	qBase.Del("page")
	baseURL := "/shop?" + qBase.Encode()
	paged, pageNum, totalPages := paginate(products, pageStr, baseURL)

	sd := pages.ShopPageData{
		Products:   paged,
		Categories: childCategories(cats, tab),
		Wishlist:   wishlist,
		Filter:     collection.FilterState{Tab: tab, Category: category, Q: search},
		Sort:       collection.SortState{Sort: sortBy, Tab: tab, Cat: category, Q: search},
		PageLinks:  collection.PageLinks{Current: pageNum, Total: totalPages, BaseURL: baseURL},
		TotalCount: len(products),
	}

	shopData := s.shopLayout(r, p, pages.ShopContent(sd))
	renderOK(w, r, layouts.Shop(shopData))
}

func (s *Site) handleCollection(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sortBy := r.URL.Query().Get("sort")
	pageStr := r.URL.Query().Get("page")

	p := s.buildPage(r)
	p.Title = name + " — Uniminute"

	products, _ := s.backend.Products(r.Context(), "", "", name)
	sortProducts(products, sortBy)
	cats, _ := s.backend.Categories(r.Context())

	baseURL := "/c/" + name
	if sortBy != "" {
		baseURL += "?sort=" + sortBy
	}
	paged, pageNum, totalPages := paginate(products, pageStr, baseURL)

	content := pages.CollectionContent(pages.CollectionPageData{
		Name:       name,
		Products:   paged,
		Categories: cats,
		Wishlist:   p.Wishlist,
		Filter:     collection.FilterState{Category: name},
		Sort:       collection.SortState{Sort: sortBy, Cat: name},
		PageLinks:  collection.PageLinks{Current: pageNum, Total: totalPages, BaseURL: baseURL},
	})
	shopData := s.shopLayout(r, p, content)
	renderOK(w, r, layouts.Shop(shopData))
}

func (s *Site) handleStores(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	tab := r.URL.Query().Get("tab")
	if tab == "All" {
		tab = ""
	}
	d := pages.StoresPageData{Page: p, Tab: tab}
	all, err := s.backend.Shops(r.Context(), "")
	if maintenance(w, r, p, err) {
		return
	}
	for _, sh := range all {
		if tab == "" || sh.Tab == tab {
			d.Stores = append(d.Stores, sh)
		}
	}
	d.Page.Title = tpl.When(tab == "", "Stores near you", tab+" stores") + " — Uniminute"
	renderOK(w, r, pages.StoresPage(d))
}

func (s *Site) handleStore(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p := s.buildPage(r)
	p.Title = name + " — Uniminute"
	// There is no single-shop endpoint; the list carries the picture and tagline.
	d := pages.StorePageData{Page: p, Store: backend.Shop{Name: name}}
	shops, err := s.backend.Shops(r.Context(), "")
	if maintenance(w, r, p, err) {
		return
	}
	found := false
	for _, sh := range shops {
		if sh.Name == name {
			d.Store, found = sh, true
		}
	}
	d.Products, _ = s.backend.ShopProducts(r.Context(), name)
	if !found && len(d.Products) == 0 {
		s.handleNotFound(w, r)
		return
	}
	renderOK(w, r, pages.StorePage(d))
}

func (s *Site) handleProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.buildPage(r)

	prod, err := s.cartProduct(r.Context(), id)
	if err != nil {
		if maintenance(w, r, p, err) {
			return
		}
		s.handleNotFound(w, r)
		return
	}
	p.Title = prod.Name + " — Uniminute"
	p.Description = prod.Description
	reviews, _ := s.backend.ProductReviews(r.Context(), prod.ID)
	renderOK(w, r, pages.ProductPage(pages.ProductPageData{
		Page:       p,
		Product:    prod,
		Wishlisted: p.Wishlist[prod.ID],
		Reviews:    reviews,
	}))
}

func (s *Site) handleSearch(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Search — Uniminute"
	d := s.searchData(r)
	d.Wishlist = p.Wishlist
	renderOK(w, r, pages.Search(p, d))
}

// searchData is SearchScreen's state for one request: the server's hits for a
// query, or the hint board's departments, faces and picks when nothing is typed.
func (s *Site) searchData(r *http.Request) searchfrag.Data {
	qs := r.URL.Query()
	d := searchfrag.Data{Q: strings.TrimSpace(qs.Get("q")), Tab: qs.Get("tab"), Sort: qs.Get("sort")}
	if d.Tab == "All" {
		d.Tab = ""
	}
	ctx := r.Context()
	if d.Q != "" {
		d.Results, _ = s.backend.Products(ctx, d.Q, d.Tab, "")
		sortResults(d.Results, d.Sort)
		return d
	}
	cats, _ := s.backend.Categories(ctx)
	for _, c := range cats {
		if c.Parent == "" {
			d.Departments = append(d.Departments, c)
		}
	}
	all, _ := s.backend.Products(ctx, "", "", "")
	seen := map[string]bool{}
	var discounted, rest []backend.Product
	for _, pr := range all {
		if d.Tab != "" && pr.Tab != d.Tab {
			continue
		}
		if pr.Category != "" && pr.ImageURL != "" && !seen[pr.Category] && len(d.Faces) < 12 {
			seen[pr.Category] = true
			d.Faces = append(d.Faces, searchfrag.Face{Category: pr.Category, Image: pr.ImageURL})
		}
		if pr.Discounted() {
			discounted = append(discounted, pr)
		} else {
			rest = append(rest, pr)
		}
	}
	d.Picks = append(discounted, rest...)
	if len(d.Picks) > 6 {
		d.Picks = d.Picks[:6]
	}
	return d
}

// sortResults is _Sort; "" keeps the server's relevance order.
func sortResults(items []backend.Product, by string) {
	switch by {
	case "price_low":
		sort.SliceStable(items, func(i, j int) bool { return items[i].Price < items[j].Price })
	case "price_high":
		sort.SliceStable(items, func(i, j int) bool { return items[i].Price > items[j].Price })
	case "discount":
		sort.SliceStable(items, func(i, j int) bool { return items[i].DiscountPercent() > items[j].DiscountPercent() })
	}
}

func (s *Site) handleCartPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "My Cart — Uniminute"
	d, trimmed := s.cartData(w, r.Context(), shop.ReadCart(r), p)
	p.CartCount = d.Count()
	renderOK(w, r, pages.CartPage(pages.CartPageData{
		Page: p, Cart: d, Trimmed: trimmed,
		CheckoutError: strings.TrimSpace(r.URL.Query().Get("checkoutError")),
	}))
}

// cartData is Cart.reconcile then the screen's state: every line resolved
// against live stock, saved back when one had to shrink or vanish, with the
// names of the lines that shrank.
func (s *Site) cartData(w http.ResponseWriter, ctx context.Context, lines []shop.CartLine, p viewdata.Page) (cart.Data, []string) {
	entries := s.resolveCartEntries(ctx, lines)
	wanted := map[string]int{}
	for _, l := range lines {
		wanted[l.ID] = l.Qty
	}
	changed := len(entries) != len(lines)
	var trimmed []string
	for _, e := range entries {
		if e.Qty < wanted[e.Product.ID] {
			trimmed = append(trimmed, e.Product.Name)
			changed = true
		}
	}
	if changed {
		kept := make([]shop.CartLine, 0, len(entries))
		for _, e := range entries {
			kept = append(kept, shop.CartLine{ID: e.Product.ID, Qty: e.Qty})
		}
		shop.SaveCart(w, kept)
	}
	return cart.Data{Entries: entries, Charges: s.charges(ctx), Address: p.Address, SignedIn: p.User != nil}, trimmed
}

// The app has no catalogue page with filters: departments are browsed on the
// home screen and categories through search. Old links land where the app
// would take them.
func (s *Site) handleShopRedirect(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if term := q.Get("q"); term != "" {
		http.Redirect(w, r, "/search?q="+url.QueryEscape(term), http.StatusSeeOther)
		return
	}
	if tab := q.Get("tab"); tab != "" && tab != "All" {
		http.Redirect(w, r, "/?tab="+url.QueryEscape(tab), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Site) handleCollectionRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/search?q="+url.QueryEscape(r.PathValue("name")), http.StatusSeeOther)
}

// The app checks out from the cart itself; there is no separate step.
func (s *Site) handleCheckoutPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/cart", http.StatusSeeOther)
}

// handleOrderPlaced is OrderConfirmationScreen for the orders just placed.
func (s *Site) handleOrderPlaced(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	want := map[string]bool{}
	for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
		want[id] = true
	}
	// This page polls while an order is live. Bypass the storefront read cache
	// so a seller acceptance or rider update appears on the very next poll.
	all, _ := s.backend.MyOrders(backend.Fresh(r.Context()), p.AccessToken)
	var placed []backend.Order
	for _, o := range all {
		if want[o.ID] {
			placed = append(placed, o)
		}
	}
	if len(placed) == 0 {
		http.Redirect(w, r, "/orders", http.StatusSeeOther)
		return
	}
	p.Title = "Order placed — Uniminute"
	renderOK(w, r, pages.OrderPlaced(pages.PlacedPageData{Page: p, Orders: placed}))
}

func (s *Site) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	next := localPath(r.URL.Query().Get(shop.NextParam))
	if p.User != nil {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	p.Title = "Sign in — Uniminute"
	markVisited(w)
	d := pages.LoginPageData{Step: "email", Next: next}
	if r.URL.Query().Get("expired") == "1" || p.SessionExpired {
		shop.ClearSessionCookies(w)
		d.Notice = "Your session expired. Sign in again to pick up where you left off."
	}

	// The backdrop is what the shop actually sells, never stand-ins.
	products, _ := s.backend.Products(r.Context(), "", "", "")
	for _, tab := range []string{"Electronics", "Grocery", "Food", "Gifts", "Beauty"} {
		for _, pr := range products {
			if pr.Tab == tab && strings.TrimSpace(pr.ImageURL) != "" {
				d.Backdrop = append(d.Backdrop, shop.ThumbWidth(pr.ImageURL, 200))
			}
		}
	}
	// Consent is only claimed against documents that are actually written.
	if policies, err := s.backend.Policies(r.Context()); err == nil {
		live := 0
		for _, pol := range policies {
			if (pol.Slug == "terms" || pol.Slug == "privacy") && pol.Published {
				live++
			}
		}
		d.PoliciesPublished = live == 2
	}
	renderOK(w, r, pages.Login(p, d))
}

const visitedCookie = "um_visited"

func visited(r *http.Request) bool {
	_, err := r.Cookie(visitedCookie)
	return err == nil
}

func markVisited(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: visitedCookie, Value: "1", Path: "/", MaxAge: 365 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// localPath keeps a post-login destination on this site: a path, never a
// scheme-relative or absolute URL someone could use to send a shopper away.
func localPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

// loginError is the sentence the sign-in card shows for a failed step.
func loginError(err error, offline string) string {
	var apiErr *backend.APIError
	if errors.As(err, &apiErr) {
		return apiMessage(err)
	}
	return offline
}

func (s *Site) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	// Signing in creates the account ("Log in or sign up"), so there is no
	// separate form to show.
	next := localPath(r.URL.Query().Get(shop.NextParam))
	http.Redirect(w, r, "/login?"+shop.NextParam+"="+url.QueryEscape(next), http.StatusSeeOther)
}

func (s *Site) handleAccountPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Account — Uniminute"
	d := pages.AccountPageData{Page: p}
	if p.User != nil {
		d.Orders, _ = s.backend.MyOrders(backend.Fresh(r.Context()), p.AccessToken)
		d.Reviews, _ = s.backend.MyReviews(backend.Fresh(r.Context()), p.AccessToken)
		if inbox, err := s.backend.Notifications(r.Context(), p.AccessToken); err == nil {
			d.Unread = inbox.Unread
		}
	}
	renderOK(w, r, pages.AccountPage(d))
}

func (s *Site) handleAddressesPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	p.Title = "Delivery addresses — Uniminute"
	addrs, _ := s.backend.Addresses(r.Context(), p.AccessToken)
	renderOK(w, r, pages.AddressesPage(pages.AddressesPageData{Page: p, Addresses: addrs}))
}

// handleAddressForm is LocationScreen, adding (no id) or editing.
func (s *Site) handleAddressForm(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	d := pages.AddressFormData{Page: p}
	d.Cities, _, _ = s.backend.Locations(r.Context())
	if id := r.PathValue("id"); id != "" {
		addrs, _ := s.backend.Addresses(r.Context(), p.AccessToken)
		for i := range addrs {
			if addrs[i].ID == id {
				d.Address = &addrs[i]
			}
		}
		if d.Address == nil {
			http.Redirect(w, r, "/addresses", http.StatusSeeOther)
			return
		}
	}
	p.Title = tpl.When(d.Address == nil, "Add delivery address", "Edit delivery address") + " — Uniminute"
	d.Page = p
	renderOK(w, r, pages.AddressForm(d))
}

func (s *Site) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Settings — Uniminute"
	// The app's own starting values, shown (disabled) until the real ones load.
	d := pages.SettingsPageData{Page: p, Preferences: backend.Preferences{Push: true, OrderUpdates: true}}
	if p.User == nil {
		d.Error = "Sign in to manage notifications."
	} else if prefs, err := s.backend.GetPreferences(r.Context(), p.AccessToken); err != nil {
		d.Error = loginError(err, offlineMessage)
	} else {
		d.Preferences = prefs
	}
	renderOK(w, r, pages.SettingsPage(d))
}

func (s *Site) handleOrdersPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Order history — Uniminute"
	if !requireAuth(w, r, p) {
		return
	}
	// Order detail also polls; cached data would make a successful acceptance
	// look stuck until the shopper manually refreshed.
	orders, err := s.backend.MyOrders(backend.Fresh(r.Context()), p.AccessToken)
	if maintenance(w, r, p, err) {
		return
	}
	d := pages.OrdersPageData{Page: p, Orders: orders}
	d.Reviews, _ = s.backend.MyReviews(backend.Fresh(r.Context()), p.AccessToken)
	if err != nil {
		d.Error = "Could not reach Uniminute — try again in a moment."
	}
	renderOK(w, r, pages.OrdersPage(d))
}

func (s *Site) handlePoliciesPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Policies — Uniminute"
	policies, _ := s.backend.Policies(r.Context())
	renderOK(w, r, pages.PoliciesPage(pages.PoliciesPageData{Page: p, Policies: policies}))
}

func (s *Site) handlePolicyPage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p := s.buildPage(r)
	policies, _ := s.backend.Policies(r.Context())
	// Unpublished drafts are shown too: the server sends a placeholder body
	// for them rather than half-written terms, exactly as the app reads it.
	for _, pol := range policies {
		if pol.Slug == slug {
			p.Title = pol.Title + " — Uniminute"
			renderOK(w, r, pages.PolicyPage(pages.PolicyPageData{Page: p, Policy: pol}))
			return
		}
	}
	// ponytail: the app bundles offline copies; the website shows its stand-in.
	p.Title = "Policy — Uniminute"
	renderOK(w, r, pages.PolicyPage(pages.PolicyPageData{Page: p, Policy: backend.Policy{
		Slug: slug, Title: "Policy", Body: "This policy has not been written yet.",
	}}))
}

// handleSavedPage renders wishlist products — the "Saved" tab in the bottom nav.
func (s *Site) handleSavedPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Wishlist — Uniminute"
	// One catalogue read, filtered, in the catalogue's order — not one
	// request per saved id.
	all, _ := s.backend.Products(r.Context(), "", "", "")
	var saved []backend.Product
	for _, pr := range all {
		if p.Wishlist[pr.ID] {
			saved = append(saved, pr)
		}
	}
	renderOK(w, r, pages.SavedPage(p, saved))
}

func (s *Site) handleNotFound(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Page not found — Uniminute"
	render(w, r, http.StatusNotFound, pages.NotFound(p))
}

var (
	statusMaintenance = ui.Status{
		Icon: "wrench", Tone: "warning", Title: "We'll be right back",
		Message: "The shop's server isn't answering right now. Your cart and saved items are safe — try again in a minute.",
		Action:  "Try again",
	}
	statusError = ui.Status{
		Icon: "circle-alert", Tone: "danger", Title: "Something went wrong",
		Message: "This page failed to load on our side. Try again, or head back to the shop.",
		Action:  "Try again", Secondary: "Go home", SecHref: "/",
	}
)

// backendDown is true when the API never answered or failed on its side —
// not when it answered "no" (404, 401, a validation error).
func backendDown(err error) bool {
	var apiErr *backend.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status >= 500
	}
	var netErr *url.Error
	return errors.As(err, &netErr)
}

// maintenance renders "We'll be right back" when err means the API is down.
func maintenance(w http.ResponseWriter, r *http.Request, p viewdata.Page, err error) bool {
	if !backendDown(err) {
		return false
	}
	p.Title = "Back soon — Uniminute"
	w.Header().Set("Retry-After", "30")
	render(w, r, http.StatusServiceUnavailable, pages.StatusPage(p, statusMaintenance))
	return true
}

// recoverer turns a handler panic into the error screen instead of a dropped connection.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			log.Printf("panic %s %s: %v\n%s", r.Method, r.URL.Path, v, debug.Stack())
			render(w, r, http.StatusInternalServerError,
				pages.StatusPage(viewdata.Page{Title: "Something went wrong — Uniminute"}, statusError))
		}()
		next.ServeHTTP(w, r)
	})
}

// ── HTMX fragment handlers ────────────────────────────────────────────────────

func (s *Site) handleProductsFragment(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tab := q.Get("tab")
	category := q.Get("category")
	search := q.Get("q")
	sortBy := q.Get("sort")

	wishlist := shop.ReadWishlist(r)
	products, _ := s.backend.Products(r.Context(), search, tab, category)
	sortProducts(products, sortBy)

	renderOK(w, r, productfrag.Grid(products, wishlist))
}

func (s *Site) handleSearchFragment(w http.ResponseWriter, r *http.Request) {
	d := s.searchData(r)
	d.Wishlist = shop.ReadWishlist(r)
	// The address bar follows the field, so a reload or a shared link lands
	// on the same results.
	w.Header().Set("HX-Replace-Url", searchfrag.URL(d.Q, d.Tab, d.Sort))
	renderOK(w, r, searchfrag.Body(d))
}

// ── Cart mutation handlers ────────────────────────────────────────────────────

func (s *Site) handleCartAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	qty, _ := strconv.Atoi(r.FormValue("qty"))
	qty = min(max(qty, 1), 99)

	prod, err := s.cartProduct(r.Context(), id)
	if err != nil {
		toast(w, "That product is no longer available.")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Every option the product has must be picked, from what the shop offers;
	// the picks become part of the line, so each colour is its own line.
	var picked []backend.Choice
	for _, o := range prod.Choices() {
		if len(o.Values) == 0 {
			continue
		}
		v := r.FormValue("opt." + o.Name)
		if !slices.Contains(o.Values, v) {
			errorToast(w, "Choose "+strings.ToLower(o.Name)+" first.")
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		picked = append(picked, backend.Choice{Name: o.Name, Value: v})
	}
	id = shop.LineID(id, picked)

	// Cart.add: take only what the shop still has room for beyond what the
	// basket already holds — across every choice of this product — and report
	// what actually went in.
	lines := shop.ReadCart(r)
	base, _ := shop.SplitLine(id)
	have, at := 0, -1
	for i, l := range lines {
		if b, _ := shop.SplitLine(l.ID); b == base {
			have += l.Qty
		}
		if l.ID == id {
			at = i
		}
	}
	got := qty
	if prod.AvailableStock != nil {
		got = min(got, max(*prod.AvailableStock-have, 0))
	}
	if got == 0 {
		// A failed request, so the quick-add shows no tick.
		toast(w, "Your cart already holds every one the shop has.")
		w.WriteHeader(http.StatusConflict)
		return
	}
	if at >= 0 {
		lines[at].Qty += got
	} else {
		lines = append(lines, shop.CartLine{ID: id, Qty: got})
	}
	shop.SaveCart(w, lines)

	count := 0
	for _, l := range lines {
		count += l.Qty
	}
	addedToast(w, prod)
	cartCountTrigger(w, count)
	if r.FormValue("buy") == "1" {
		w.Header().Set("HX-Redirect", "/cart")
	}
	// Nothing to swap: the toast and both badges follow the triggers above.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Site) handleCartSet(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	qty, _ := strconv.Atoi(r.FormValue("qty"))
	qty = max(qty, 0)

	before := 0
	for _, l := range shop.ReadCart(r) {
		if l.ID == id {
			before = l.Qty
		}
	}
	prod, err := s.cartProduct(r.Context(), id)
	if err == nil && prod.AvailableStock != nil {
		qty = min(qty, *prod.AvailableStock)
	}
	lines := shop.CartSetQty(r, id, qty)
	shop.SaveCart(w, lines)

	// _removeWithUndo: removing a line is one tap, so it is one tap to undo.
	if qty == 0 && before > 0 && err == nil {
		addTrigger(w, "lw:toast", map[string]any{
			"message":    "Removed " + prod.Name,
			"undo":       true,
			"undoAction": "/cart/set?id=" + url.QueryEscape(id) + "&qty=" + strconv.Itoa(before),
			"reload":     true,
		})
	}

	p := s.buildPage(r)
	d, _ := s.cartData(w, r.Context(), lines, p)
	cartCountTrigger(w, d.Count())
	renderOK(w, r, cartfrag.Updated(d))
}

// Removing is setting the quantity to zero.
func (s *Site) handleCartRemove(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	r.Form.Set("qty", "0")
	s.handleCartSet(w, r)
}

func (s *Site) handleWishlistToggle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	wl := shop.ReadWishlist(r)
	nowFilled := !wl[id]
	if nowFilled {
		wl[id] = true
	} else {
		delete(wl, id)
	}
	shop.SaveWishlist(w, wl)
	renderOK(w, r, wishlistfrag.Toggle(id, nowFilled))
}

// ── Auth handlers ─────────────────────────────────────────────────────────────

func (s *Site) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	next := localPath(r.FormValue("next"))
	// mode is "" from the email step, "code" for "Email me a code instead"
	// and "reset" for "Forgot password?" (a separate reset code).
	mode := r.FormValue("mode")
	sendTo := tpl.When(mode == "reset", "reset", "code")
	d := pages.LoginPageData{Next: next, Email: email, Step: loginStep(r.FormValue("from")), HasPassword: mode != ""}

	var result backend.LoginStart
	var err error
	if mode == "reset" {
		err = s.backend.ForgotPassword(r.Context(), email)
	} else {
		result, err = s.backend.StartLogin(r.Context(), email, mode == "code")
	}
	var apiErr *backend.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests {
		// A code went out moments ago and is still good: ask for it.
		d.Step, d.Error = sendTo, loginError(err, "")
		renderOK(w, r, pages.LoginCard(d))
		return
	}
	if err != nil {
		d.Error = loginError(err, "Could not reach the server, so no code went out. Try again, or use Browse the shop to look around.")
		renderOK(w, r, pages.LoginCard(d))
		return
	}
	// A server running with the code switched off signs in on the spot.
	if result.Token != "" {
		shop.SetSessionCookies(w, backend.Session{Email: result.Email, Token: result.Token, RefreshToken: result.RefreshToken, ExpiresIn: result.ExpiresIn})
		redirect(w, r, s.afterSignIn(r.Context(), result.Token, next))
		return
	}
	if result.NeedsPassword {
		sendTo = "password"
	}
	if result.Email != "" {
		d.Email = result.Email
	}
	d.Step = sendTo
	renderOK(w, r, pages.LoginCard(d))
}

// handleLoginStep switches the card without calling the backend: "Use
// password instead", "Back to password" and "Use a different email".
func (s *Site) handleLoginStep(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	step := tpl.When(r.FormValue("step") == "password", "password", "email")
	renderOK(w, r, pages.LoginCard(pages.LoginPageData{
		Email: strings.TrimSpace(r.FormValue("email")), Step: step, Next: localPath(r.FormValue("next")), HasPassword: step == "password",
	}))
}

// loginStep is a step name from the form, or "email" for anything else.
func loginStep(s string) string {
	switch s {
	case "code", "password", "reset":
		return s
	}
	return "email"
}

func (s *Site) handleLoginReset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	next := localPath(r.FormValue("next"))

	sess, err := s.backend.ResetPassword(r.Context(), email,
		strings.TrimSpace(r.FormValue("code")), r.FormValue("password"))
	if err != nil {
		d := pages.LoginPageData{Email: email, Step: "reset", Next: next, Error: loginError(err, offlineMessage)}
		renderOK(w, r, pages.LoginCard(d))
		return
	}
	shop.SetSessionCookies(w, sess)
	redirect(w, r, s.afterSignIn(r.Context(), sess.Token, next))
}

func (s *Site) handleLoginVerify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	code := strings.TrimSpace(r.FormValue("code"))
	next := localPath(r.FormValue("next"))

	sess, err := s.backend.VerifyCode(r.Context(), email, code)
	if err != nil {
		d := pages.LoginPageData{Email: email, Step: "code", Next: next, Error: loginError(err, offlineMessage), HasPassword: r.FormValue("hasPassword") != ""}
		renderOK(w, r, pages.LoginCard(d))
		return
	}
	shop.SetSessionCookies(w, sess)
	redirect(w, r, s.afterSignIn(r.Context(), sess.Token, next))
}

func (s *Site) handleLoginPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")
	next := localPath(r.FormValue("next"))

	sess, err := s.backend.PasswordLogin(r.Context(), email, password)
	if err != nil {
		d := pages.LoginPageData{Email: email, Step: "password", Next: next, Error: loginError(err, offlineMessage)}
		renderOK(w, r, pages.LoginCard(d))
		return
	}
	shop.SetSessionCookies(w, sess)
	redirect(w, r, s.afterSignIn(r.Context(), sess.Token, next))
}

func (s *Site) handleLogout(w http.ResponseWriter, r *http.Request) {
	shop.ClearSessionCookies(w)
	// The app stays on the profile screen, now signed out.
	redirect(w, r, "/account")
}

// ── Account mutation handlers ─────────────────────────────────────────────────

func (s *Site) handleProfileUpdate(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	changes := map[string]any{
		"name":  r.FormValue("name"),
		"phone": r.FormValue("phone"),
	}
	if err := s.backend.UpdateMe(r.Context(), p.AccessToken, changes); err != nil {
		errorToast(w, "Couldn't save profile.")
	} else {
		successToast(w, "Profile saved.")
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Site) handlePreferencesUpdate(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Only the switch that moved: the app patches one field at a time.
	changes := map[string]bool{}
	for _, key := range []string{"push", "emailOffers", "orderUpdates"} {
		if v, ok := r.Form[key]; ok && len(v) > 0 {
			changes[key] = v[0] == "true"
		}
	}
	if len(changes) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := s.backend.PatchPreferences(r.Context(), p.AccessToken, changes); err != nil {
		errorToast(w, apiMessage(err))
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Site) handleAddressCreate(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	addr := backend.Address{
		Label:   r.FormValue("label"),
		Line:    r.FormValue("line"),
		City:    r.FormValue("city"),
		Pincode: r.FormValue("pincode"),
		Name:    r.FormValue("name"),
		Phone:   r.FormValue("phone"),
	}
	if _, err := s.backend.SaveAddress(r.Context(), p.AccessToken, addr, ""); err != nil {
		errorToast(w, apiMessage(err))
		w.WriteHeader(http.StatusOK)
		return
	}
	redirect(w, r, "/addresses")
}

func (s *Site) handleAddressUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	addr := backend.Address{
		Label:   r.FormValue("label"),
		Line:    r.FormValue("line"),
		City:    r.FormValue("city"),
		Pincode: r.FormValue("pincode"),
		Name:    r.FormValue("name"),
		Phone:   r.FormValue("phone"),
	}
	if _, err := s.backend.SaveAddress(r.Context(), p.AccessToken, addr, id); err != nil {
		errorToast(w, apiMessage(err))
		w.WriteHeader(http.StatusOK)
		return
	}
	redirect(w, r, "/addresses")
}

func (s *Site) handleAddressDefault(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	if err := s.backend.DefaultAddress(r.Context(), p.AccessToken, id); err != nil {
		errorToast(w, apiMessage(err))
		w.WriteHeader(http.StatusOK)
		return
	}
	redirect(w, r, "/addresses")
}

func (s *Site) handleAddressDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	if err := s.backend.DeleteAddress(r.Context(), p.AccessToken, id); err != nil {
		errorToast(w, apiMessage(err))
		w.WriteHeader(http.StatusOK)
		return
	}
	redirect(w, r, "/addresses")
}

// ── Checkout handler ──────────────────────────────────────────────────────────

func (s *Site) handleCheckout(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if p.User == nil {
		redirect(w, r, "/login?next=/cart")
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	addressID := r.FormValue("addressId")
	if addressID == "" {
		toast(w, "Add a delivery address first.")
		redirect(w, r, "/addresses/new")
		return
	}
	amount, ok := s.cartAmountPaise(r)
	if !ok {
		redirect(w, r, "/cart")
		return
	}
	result, err := s.checkoutCart(r, p.AccessToken, addressID, float64(amount)/100, nil)
	if err != nil {
		// Checkout is a native form submission so its loading screen cannot be
		// stranded by an AJAX redirect. The cart renders the recovery inline.
		http.Redirect(w, r, "/cart?checkoutError="+url.QueryEscape(apiMessage(err)), http.StatusSeeOther)
		return
	}

	shop.SaveCart(w, nil)
	ids := make([]string, 0, len(result.Orders))
	for _, o := range result.Orders {
		ids = append(ids, o.ID)
	}
	redirect(w, r, "/orders/placed?ids="+url.QueryEscape(strings.Join(ids, ",")))
}

// apiMessage is the backend's error text for the shopper, or the app's
// offline sentence when the backend never answered.
func apiMessage(err error) string {
	var apiErr *backend.APIError
	if errors.As(err, &apiErr) {
		var body struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(apiErr.Body), &body) == nil && body.Error != "" {
			return body.Error
		}
		return statusMessage(apiErr.Status)
	}
	return offlineMessage
}

const offlineMessage = "Could not reach Uniminute. Check your connection and try again."

// statusMessage stands in when the backend answers without a sentence of its
// own; a raw body can be an HTML error page, never something to show.
func statusMessage(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "Your session ended. Sign in again."
	case status == http.StatusForbidden:
		return "You don't have access to do that."
	case status == http.StatusNotFound:
		return "That is no longer here. Refresh the page and try again."
	case status == http.StatusTooManyRequests:
		return "Too many attempts. Wait a minute, then try again."
	case status >= 500:
		return "Uniminute had a problem on its side. Try again in a moment."
	}
	return "That didn't go through. Check the details and try again."
}

func (s *Site) handleOrderCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	if err := s.backend.CancelOrder(r.Context(), p.AccessToken, id); err != nil {
		errorToast(w, apiMessage(err))
		w.WriteHeader(http.StatusOK)
		return
	}
	// The detail page reloads showing the cancellation notice.
	redirect(w, r, "/orders/"+id)
}

// cartProduct is the product a basket line names. "id@Store" is the same
// product bought from another local vendor at that vendor's price — the id
// CompareScreen puts in the app's cart — so it is the base product with that
// vendor's store and price, and no stock figure of its own.
// charges is what every basket pays on top of its items, as the admin set it.
func (s *Site) charges(ctx context.Context) []backend.Charge {
	cs, err := s.backend.Charges(ctx)
	if err != nil {
		// ponytail: display-only fallback; the API re-checks the total at checkout and refuses a mismatch
		return []backend.Charge{{ID: "delivery", Name: "Delivery", Amount: 15}}
	}
	return cs
}

// cartProduct is the product a cart line is for. A line id may carry the
// buyer's choices ("item-44?Colour=..."); the product keeps that full id so
// the line can be found again.
func (s *Site) cartProduct(ctx context.Context, lineID string) (backend.Product, error) {
	id, picked := shop.SplitLine(lineID)
	p, err := s.offerProduct(ctx, id)
	if err == nil {
		if price, ok := p.PriceFor(picked); ok {
			p.Price = price
		} else {
			return p, fmt.Errorf("option combination unavailable")
		}
	}
	if err == nil && lineID != id {
		p.ID = lineID
	}
	return p, err
}

func (s *Site) offerProduct(ctx context.Context, id string) (backend.Product, error) {
	base, store, fromVendor := strings.Cut(id, "@")
	p, err := s.backend.Product(ctx, base)
	if err != nil || !fromVendor {
		return p, err
	}
	for _, o := range p.Offers {
		if o.Store == store {
			p.ID, p.Store, p.Price, p.MRP = id, store, o.Price, 0
			p.VariantPrices = nil
			p.AvailableStock, p.Offers = nil, nil
			return p, nil
		}
	}
	return backend.Product{}, errors.New("no offer from " + store)
}

// ---- admin panel ----

// adminStaff is the signed-in admin, or the sign-in page rendered in its place.
func (s *Site) adminStaff(w http.ResponseWriter, r *http.Request) (shop.Staff, bool) {
	staff, ok := shop.ReadStaff(r, "admin")
	if !ok {
		p := s.buildPage(r)
		p.Title = "Uniminute admin"
		renderOK(w, r, pages.AdminLogin(pages.AdminLoginData{Page: p}))
	}
	return staff, ok
}

// adminExpired signs the panel out when the backend no longer accepts its token.
func adminExpired(w http.ResponseWriter, r *http.Request, err error) bool {
	var apiErr *backend.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		shop.ClearStaff(w, "admin")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return true
	}
	return false
}

// adminData reads everything the panel shows, as the app's _load does.
func (s *Site) adminData(w http.ResponseWriter, r *http.Request, staff shop.Staff) (pages.AdminData, bool) {
	ctx, token := r.Context(), staff.Token
	q := r.URL.Query()
	p := s.buildPage(r)
	p.Title = "Admin — Uniminute"
	d := pages.AdminData{Page: p, Staff: staff, Tab: q.Get("tab"), Q: strings.TrimSpace(q.Get("q")),
		Stage: q.Get("stage"), From: q.Get("from"), To: q.Get("to"), OpenDept: q.Get("dept")}
	d.PageNum, _ = strconv.Atoi(q.Get("page"))
	if d.Tab == "" {
		d.Tab = "review"
	}
	// Independent reads, each a round trip to the API: run them side by side.
	var (
		wg          sync.WaitGroup
		overviewErr error
		// These three come back as plain JSON arrays; items is wrapped in {"items": …}.
		stores, orders, riders []map[string]any
		items                  struct {
			Items []backend.InventoryItem `json:"items"`
		}
		campaigns struct {
			Campaigns []backend.Campaign `json:"campaigns"`
		}
		cats []backend.Category
	)
	for _, load := range []func(){
		func() { overviewErr = s.backend.Get(ctx, "/api/admin/overview", token, &d.Overview) },
		func() { _ = s.backend.Get(ctx, "/api/admin/insights", token, &d.Insights) },
		func() { _ = s.backend.Get(ctx, "/api/admin/stores", token, &stores) },
		func() { _ = s.backend.Get(ctx, "/api/admin/orders", token, &orders) },
		func() { _ = s.backend.Get(ctx, "/api/admin/riders", token, &riders) },
		func() { _ = s.backend.Get(ctx, "/api/admin/items", token, &items) },
		func() { _ = s.backend.Get(ctx, "/api/admin/campaigns", token, &campaigns) },
		func() { d.Groups, _ = s.backend.CompareGroups(ctx) },
		func() { d.Policies, _ = s.backend.Policies(ctx) },
		func() { d.Charges = s.charges(ctx) },
		func() { _ = s.backend.Get(ctx, "/api/admin/reviews", token, &d.Reviews) },
		func() { cats, _ = s.backend.Categories(ctx) },
	} {
		wg.Add(1)
		go func() { defer wg.Done(); load() }()
	}
	wg.Wait()
	if overviewErr != nil {
		if adminExpired(w, r, overviewErr) {
			return d, false
		}
		d.Error = apiMessage(overviewErr)
	}
	d.Stores, d.Orders, d.Riders = stores, orders, riders
	d.Items, d.Campaigns = items.Items, campaigns.Campaigns
	for _, c := range cats {
		if c.Parent == "" {
			d.Departments = append(d.Departments, c)
		}
	}
	return d, true
}

func (s *Site) handleAdmin(w http.ResponseWriter, r *http.Request) {
	staff, ok := s.adminStaff(w, r)
	if !ok {
		return
	}
	d, ok := s.adminData(w, r, staff)
	if !ok {
		return
	}
	renderOK(w, r, pages.Admin(d))
}

// handleAdminAlias sends the app's own addresses (/admin/log_IN, in any case)
// and anything else under /admin that is not a page to the panel.
func (s *Site) handleAdminAlias(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Site) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	token, user, err := s.backend.AdminLogin(r.Context(), username, r.FormValue("password"))
	if err != nil {
		p := s.buildPage(r)
		p.Title = "Uniminute admin"
		renderOK(w, r, pages.AdminLogin(pages.AdminLoginData{Page: p, Username: username, Error: apiMessage(err)}))
		return
	}
	shop.SetStaff(w, "admin", shop.Staff{Token: token, Subject: user})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Site) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	shop.ClearStaff(w, "admin")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// handleAdminExport is Export CSV for the list and filters on screen.
func (s *Site) handleAdminExport(w http.ResponseWriter, r *http.Request) {
	staff, ok := s.adminStaff(w, r)
	if !ok {
		return
	}
	d, ok := s.adminData(w, r, staff)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="lamazon-`+d.Tab+`.csv"`)
	_, _ = w.Write([]byte(d.ExportCSV()))
}

// handleAdminBanner is CampaignEditor, for a new banner or an existing one.
func (s *Site) handleAdminBanner(w http.ResponseWriter, r *http.Request) {
	staff, ok := s.adminStaff(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	p := s.buildPage(r)
	d := pages.BannerEditorData{Page: p}
	if id := r.PathValue("id"); id != "" {
		var campaigns struct {
			Campaigns []backend.Campaign `json:"campaigns"`
		}
		if err := s.backend.Get(ctx, "/api/admin/campaigns", staff.Token, &campaigns); adminExpired(w, r, err) {
			return
		}
		for i := range campaigns.Campaigns {
			if campaigns.Campaigns[i].ID == id {
				d.Campaign = &campaigns.Campaigns[i]
			}
		}
		if d.Campaign == nil {
			http.Redirect(w, r, "/admin?tab=banners", http.StatusSeeOther)
			return
		}
	}
	cats, _ := s.backend.Categories(ctx)
	seen := map[string]bool{"": true}
	d.Categories = []string{""}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			d.Categories = append(d.Categories, name)
		}
	}
	for _, c := range cats {
		if c.Parent == "" {
			d.Departments = append(d.Departments, c.Name)
			add(c.Name)
		}
	}
	for _, c := range cats {
		for _, child := range c.Children {
			add(child.Name)
			for _, leaf := range categoryLeaves(child) {
				add(leaf)
			}
		}
	}
	// A deleted destination stays visible, so staff replace it before saving.
	if d.Campaign != nil {
		add(d.Campaign.Category)
		if d.Campaign.Department != "" && !slices.Contains(d.Departments, d.Campaign.Department) {
			d.Departments = append(d.Departments, d.Campaign.Department)
		}
	}
	d.Page.Title = tpl.When(d.Campaign == nil, "Create banner", "Edit banner") + " — Uniminute"
	renderOK(w, r, pages.BannerEditor(d))
}

func (s *Site) handleAdminPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminStaff(w, r); !ok {
		return
	}
	policies, _ := s.backend.Policies(r.Context())
	for _, pol := range policies {
		if pol.Slug == r.PathValue("slug") {
			p := s.buildPage(r)
			p.Title = pol.Title + " — Uniminute"
			renderOK(w, r, pages.PolicyEditor(pages.PolicyEditorData{Page: p, Policy: pol}))
			return
		}
	}
	http.Redirect(w, r, "/admin?tab=policies", http.StatusSeeOther)
}

// handleAdminStorePhotos is AdminPhotosScreen: one store's listings.
func (s *Site) handleAdminStorePhotos(w http.ResponseWriter, r *http.Request) {
	staff, ok := s.adminStaff(w, r)
	if !ok {
		return
	}
	p := s.buildPage(r)
	d := pages.StorePhotosData{Page: p, StoreName: r.URL.Query().Get("name")}
	if d.StoreName == "" {
		d.StoreName = r.PathValue("owner")
	}
	var items struct {
		Items []backend.InventoryItem `json:"items"`
	}
	err := s.backend.Get(r.Context(), "/api/admin/items?owner="+url.QueryEscape(r.PathValue("owner")), staff.Token, &items)
	if adminExpired(w, r, err) {
		return
	}
	if err != nil {
		d.Error = apiMessage(err)
	}
	d.Items = items.Items
	d.Page.Title = d.StoreName + " — Uniminute"
	renderOK(w, r, pages.StorePhotos(d))
}

// handleDelivery is DeliveryScreen: the rider's sign-in, or their panel.
func (s *Site) handleDelivery(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Uniminute delivery"
	rider, ok := shop.ReadStaff(r, "rider")
	if !ok {
		renderOK(w, r, pages.RiderLogin(pages.RiderLoginData{Page: p}))
		return
	}
	ctx := r.Context()
	d := pages.DeliveryData{Page: p, Rider: rider, Message: r.URL.Query().Get("msg")}
	panel, err := s.backend.RiderPanelFor(ctx, rider.Token)
	var apiErr *backend.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		// An expired or revoked rider token signs the panel out, rather than
		// showing a run that cannot load.
		shop.ClearStaff(w, "rider")
		http.Redirect(w, r, "/delivery", http.StatusSeeOther)
		return
	}
	if err != nil {
		d.Error = apiMessage(err)
	}
	d.Panel = panel
	d.History, _ = s.backend.RiderHistoryFor(ctx, rider.Token)
	renderOK(w, r, pages.Delivery(d))
}

func (s *Site) handleDeliveryLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	phone := strings.TrimSpace(r.FormValue("phone"))
	sess, err := s.backend.RiderLogin(r.Context(), phone, strings.TrimSpace(r.FormValue("pin")))
	if err != nil {
		p := s.buildPage(r)
		p.Title = "Uniminute delivery"
		renderOK(w, r, pages.RiderLogin(pages.RiderLoginData{Page: p, Phone: phone, Error: apiMessage(err)}))
		return
	}
	shop.SetStaff(w, "rider", shop.Staff{Token: sess.Token, Subject: sess.Phone, Name: sess.Name})
	http.Redirect(w, r, "/delivery", http.StatusSeeOther)
}

func (s *Site) handleDeliveryLogout(w http.ResponseWriter, r *http.Request) {
	shop.ClearStaff(w, "rider")
	http.Redirect(w, r, "/delivery", http.StatusSeeOther)
}

func (s *Site) handleDeliveryPick(w http.ResponseWriter, r *http.Request) {
	rider, ok := shop.ReadStaff(r, "rider")
	if !ok {
		http.Redirect(w, r, "/delivery", http.StatusSeeOther)
		return
	}
	msg := ""
	if err := s.backend.RiderPick(r.Context(), rider.Token, r.PathValue("id")); err != nil {
		msg = apiMessage(err)
	}
	http.Redirect(w, r, "/delivery?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

// handleDeliveryDeliver closes a drop with the customer's code. A wrong code
// changes nothing, and the backend's sentence says so.
func (s *Site) handleDeliveryDeliver(w http.ResponseWriter, r *http.Request) {
	rider, ok := shop.ReadStaff(r, "rider")
	if !ok {
		http.Redirect(w, r, "/delivery", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	msg := "Delivered. The shop and the customer have been told."
	if err := s.backend.RiderDeliver(r.Context(), rider.Token, r.PathValue("id"), strings.TrimSpace(r.FormValue("code"))); err != nil {
		msg = apiMessage(err)
	}
	http.Redirect(w, r, "/delivery?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

// handleSellerDashboard is SellerDashboardScreen. With no store yet it sends
// the seller to open one, as the app's dashboard shows onboarding in place.
func (s *Site) handleSellerDashboard(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	ctx := r.Context()
	store, err := s.backend.SellerStoreFor(ctx, p.AccessToken)
	if err == nil && store == nil {
		http.Redirect(w, r, "/seller/store", http.StatusSeeOther)
		return
	}
	if err != nil {
		p.Title = "Your store — Uniminute"
		render(w, r, http.StatusBadGateway, pages.NotFound(p))
		return
	}
	d := pages.SellerDashboardData{Page: p, Store: *store, Pane: r.URL.Query().Get("pane")}
	d.Items, _ = s.backend.SellerItems(ctx, p.AccessToken)
	d.Orders, _ = s.backend.SellerOrders(ctx, p.AccessToken)
	d.Page.Title = "Your store — Uniminute"
	renderOK(w, r, pages.SellerDashboard(d))
}

// handleSellerStoreForm is SellerOnboardingScreen: open a store, or edit it.
func (s *Site) handleSellerStoreForm(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	ctx := r.Context()
	d := pages.SellerStoreFormData{Page: p}
	d.Store, _ = s.backend.SellerStoreFor(ctx, p.AccessToken)
	d.Cities, _, _ = s.backend.Locations(ctx)
	cats, _ := s.backend.Categories(ctx)
	for _, c := range cats {
		if c.Parent == "" {
			d.Departments = append(d.Departments, c.Name)
		}
	}
	d.Page.Title = tpl.When(d.Store == nil, "Open your store", "Edit store") + " — Uniminute"
	renderOK(w, r, pages.SellerStoreForm(d))
}

// handleSellerProductForm is SellerProductScreen: add (no id) or edit.
func (s *Site) handleSellerProductForm(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	ctx := r.Context()
	store, err := s.backend.SellerStoreFor(ctx, p.AccessToken)
	if err != nil || store == nil {
		http.Redirect(w, r, "/seller/store", http.StatusSeeOther)
		return
	}
	d := pages.SellerProductData{Page: p, DeptOf: map[string]string{}}
	if id := r.PathValue("id"); id != "" {
		items, _ := s.backend.SellerItems(ctx, p.AccessToken)
		for i := range items {
			if items[i].ID == id {
				d.Item = &items[i]
			}
		}
		if d.Item == nil {
			http.Redirect(w, r, "/seller?pane=inventory", http.StatusSeeOther)
			return
		}
	} else if !store.Approved() {
		// Adding stock is what approval gates; the dashboard says why.
		http.Redirect(w, r, "/seller", http.StatusSeeOther)
		return
	}
	cats, _ := s.backend.Categories(ctx)
	for _, c := range cats {
		if c.Parent == "" {
			indexCategories(d.DeptOf, c.Name, c)
		}
	}
	d.Sections = sellableSections(cats, store.Categories)
	d.Groups, _ = s.backend.CompareGroups(ctx)
	d.Page.Title = tpl.When(d.Item == nil, "Add product", "Edit product") + " — Uniminute"
	renderOK(w, r, pages.SellerProductForm(d))
}

// sellableSections is sellableGroups for the store's departments: each
// category with the leaf shelves a product is filed under, or a department
// with no categories standing for itself. Every department when the store
// names none the server still knows.
func sellableSections(cats []backend.Category, mine []string) []pages.CategorySection {
	var out []pages.CategorySection
	add := func(dept backend.Category) {
		if len(dept.Children) == 0 {
			out = append(out, pages.CategorySection{Name: dept.Name, Leaves: []string{dept.Name}})
			return
		}
		for _, c := range dept.Children {
			out = append(out, pages.CategorySection{Name: c.Name, Leaves: categoryLeaves(c)})
		}
	}
	for _, name := range mine {
		for _, c := range cats {
			if c.Parent == "" && c.Name == name {
				add(c)
			}
		}
	}
	if len(out) == 0 {
		for _, c := range cats {
			if c.Parent == "" {
				add(c)
			}
		}
	}
	return out
}

// categoryLeaves is CategoryNode.leaves: the deepest shelves, or itself.
func categoryLeaves(c backend.Category) []string {
	if len(c.Children) == 0 {
		return []string{c.Name}
	}
	var out []string
	for _, child := range c.Children {
		out = append(out, categoryLeaves(child)...)
	}
	return out
}

// handleCompare is CompareScreen for one product.
func (s *Site) handleCompare(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	prod, err := s.backend.Product(r.Context(), r.PathValue("id"))
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	p.Title = "Compare Prices — Uniminute"
	d := pages.CompareData{Page: p, Product: prod}
	if prod.CompareGroup != "" {
		d.Rivals, _ = s.backend.Compare(r.Context(), prod.CompareGroup)
	}
	renderOK(w, r, pages.ComparePage(d))
}

// handleOrderDetail is OrderDetailScreen for one of the shopper's orders.
func (s *Site) handleOrderDetail(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	id := r.PathValue("id")
	orders, err := s.backend.MyOrders(r.Context(), p.AccessToken)
	if maintenance(w, r, p, err) {
		return
	}
	for _, o := range orders {
		if o.ID == id {
			p.Title = "Order " + shop.OrderRef(id) + " — Uniminute"
			d := pages.OrderDetailData{Page: p, Order: o}
			if reviews, err := s.backend.MyReviews(backend.Fresh(r.Context()), p.AccessToken); err == nil {
				if rv, ok := reviews[id]; ok {
					d.Review = &rv
				}
			}
			renderOK(w, r, pages.OrderDetail(d))
			return
		}
	}
	http.Redirect(w, r, "/orders", http.StatusSeeOther)
}

func (s *Site) handleHelpPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	p.Title = "Help & Support — Uniminute"
	policies, _ := s.backend.Policies(r.Context())
	renderOK(w, r, pages.HelpPage(p, policies))
}

func (s *Site) handleNotificationsPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	p.Title = "Notifications — Uniminute"
	inbox, _ := s.backend.Notifications(r.Context(), p.AccessToken)
	renderOK(w, r, pages.NotificationsPage(p, inbox))
	// Shown now, so read: the next visit shows them without the "new" mark.
	if inbox.Unread > 0 {
		_ = s.backend.MarkNotificationsRead(r.Context(), p.AccessToken)
	}
}

// handleProfileSetupPage is ProfileSetupScreen.
func (s *Site) handleProfileSetupPage(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	p.Title = "Your details — Uniminute"
	d := pages.ProfileSetupData{Page: p, Next: localPath(r.URL.Query().Get(shop.NextParam))}
	if cities, _, err := s.backend.Locations(r.Context()); err == nil && len(cities) > 0 {
		d.City = cities[0]
	}
	renderOK(w, r, pages.ProfileSetup(d))
}

// handleProfileSetup saves the details, then the first address, in that
// order, the way the app does.
func (s *Site) handleProfileSetup(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if p.User == nil {
		redirect(w, r, "/login?next=/profile/setup")
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	changes := map[string]any{"name": name, "phone": phone}
	if pw := r.FormValue("password"); pw != "" {
		changes["password"] = pw
	}
	if err := s.backend.UpdateMe(r.Context(), p.AccessToken, changes); err != nil {
		errorToast(w, apiMessage(err))
		w.WriteHeader(http.StatusOK)
		return
	}
	addr := backend.Address{
		Label: r.FormValue("label"),
		Line:  strings.TrimSpace(r.FormValue("line")),
		City:  r.FormValue("city"),
		Name:  name,
		Phone: phone,
	}
	if _, err := s.backend.SaveAddress(r.Context(), p.AccessToken, addr, ""); err != nil {
		errorToast(w, apiMessage(err))
		w.WriteHeader(http.StatusOK)
		return
	}
	redirect(w, r, localPath(r.FormValue("next")))
}

// afterSignIn sends a first-time shopper through "Your details" before
// wherever they were going.
func (s *Site) afterSignIn(ctx context.Context, token, next string) string {
	if u, err := s.backend.Me(ctx, token); err == nil && !u.Ready {
		return "/profile/setup?" + shop.NextParam + "=" + url.QueryEscape(next)
	}
	return next
}

// ── Cart resolution ───────────────────────────────────────────────────────────

// resolveCartEntries fetches the live product for every cart line. Lines whose
// product is no longer available are silently dropped.
func (s *Site) resolveCartEntries(ctx context.Context, lines []shop.CartLine) []shop.CartEntry {
	entries := make([]shop.CartEntry, 0, len(lines))
	for _, l := range lines {
		prod, err := s.cartProduct(ctx, l.ID)
		if err != nil {
			continue
		}
		qty := l.Qty
		if cap, ok := (shop.CartEntry{Product: prod, Qty: qty}).Cap(); ok && qty > cap {
			qty = cap
		}
		if qty > 0 {
			entries = append(entries, shop.CartEntry{Product: prod, Qty: qty})
		}
	}
	return entries
}

// ── Small helpers ─────────────────────────────────────────────────────────────

func tabTitle(tab string) string {
	if tab == "" || strings.EqualFold(tab, "all") {
		return "Shop"
	}
	return tab
}

func childCategories(cats []backend.Category, tab string) []backend.Category {
	if tab == "" {
		// Return all top-level categories.
		var out []backend.Category
		for _, c := range cats {
			if c.Parent == "" {
				out = append(out, c)
			}
		}
		return out
	}
	// Return children of the matching department.
	for _, c := range cats {
		if strings.EqualFold(c.Name, tab) {
			return c.Children
		}
	}
	return nil
}

// handleReview saves the buyer's stars and words for a delivered order.
func (s *Site) handleReview(w http.ResponseWriter, r *http.Request) {
	p := s.buildPage(r)
	if !requireAuth(w, r, p) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	item, _ := strconv.Atoi(r.FormValue("itemRating"))
	rider, _ := strconv.Atoi(r.FormValue("riderRating"))
	err := s.backend.SaveReview(r.Context(), p.AccessToken, r.PathValue("id"),
		item, r.FormValue("itemText"), rider, r.FormValue("riderText"))
	if err != nil {
		errorToast(w, apiMessage(err))
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	// The product page caches reviews like any public read; drop them.
	s.backend.Forget("")
	w.WriteHeader(http.StatusNoContent)
}
