package shop

// The basket and the wishlist live in cookies, not in the database — because
// that is where the Flutter app keeps them. Cart (frontend/lib/data/cart.dart)
// is a snapshot of {product, qty} lines with a checkout requestId rotated on
// every change; Wishlist (data/wishlist.dart) is just a set of product ids,
// saved per device and working for guests. The existing backend has no cart or
// wishlist endpoint to call — flagged in the README — so this side reproduces
// the client behaviour the app already ships: identity in a cookie, and every
// price and stock check still made by the backend at checkout.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"

	"lamazon/website/backend"
)

const (
	CartCookie     = "lw_cart" // [{"id": "...", "qty": 1}]
	CartRIDCookie  = "lw_rid"  // checkout request id, rotated with every cart change
	WishlistCookie = "lw_wish" // ["productId", ...]
	cookieMaxAge   = 30 * 24 * 3600
)

type CartLine struct {
	ID  string `json:"id"`
	Qty int    `json:"qty"`
}

func WriteCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		MaxAge: cookieMaxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// writeListCookie stores JSON base64url-encoded. net/http drops the double
// quotes JSON is made of from a raw cookie value, which silently turned every
// basket into unparseable text.
func writeListCookie(w http.ResponseWriter, name string, v any) {
	buf, _ := json.Marshal(v)
	WriteCookie(w, name, base64.RawURLEncoding.EncodeToString(buf))
}

func readListCookie(r *http.Request, name string) []json.RawMessage {
	c, err := r.Cookie(name)
	if err != nil || c.Value == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		raw = []byte(c.Value) // a cookie written before the encoding
	}
	var out []json.RawMessage
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func ReadCart(r *http.Request) []CartLine {
	var out []CartLine
	for _, raw := range readListCookie(r, CartCookie) {
		var line CartLine
		if json.Unmarshal(raw, &line) == nil && line.ID != "" && line.Qty > 0 {
			out = append(out, line)
		}
	}
	if len(out) > 100 {
		out = out[:100] // backend/orders.go: "order between 1 and 100 items"
	}
	return out
}

// SaveCart writes the lines and a fresh request id together, exactly like
// Cart._save(rotate: true): a checkout that is already in flight can never be
// replayed against a basket that has since changed.
func SaveCart(w http.ResponseWriter, lines []CartLine) {
	if lines == nil {
		lines = []CartLine{}
	}
	writeListCookie(w, CartCookie, lines)
	WriteCookie(w, CartRIDCookie, NewRequestID())
}

// CheckoutRequestID is the id this basket revision will check out under. The
// backend requires 8–100 characters.
func CheckoutRequestID(r *http.Request) string {
	if c, err := r.Cookie(CartRIDCookie); err == nil && len(c.Value) >= 8 {
		return c.Value
	}
	return NewRequestID()
}

func NewRequestID() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

func CartCount(r *http.Request) int {
	n := 0
	for _, l := range ReadCart(r) {
		n += l.Qty
	}
	return n
}

// CartSetQty is Cart.setQty: the line keeps its place, and zero drops it.
func CartSetQty(r *http.Request, id string, qty int) []CartLine {
	lines := ReadCart(r)
	out := make([]CartLine, 0, len(lines)+1)
	found := false
	for _, l := range lines {
		if l.ID == id {
			found = true
			if qty > 0 {
				out = append(out, CartLine{ID: id, Qty: qty})
			}
			continue
		}
		out = append(out, l)
	}
	if !found && qty > 0 {
		out = append(out, CartLine{ID: id, Qty: qty})
	}
	return out
}

// CartEntry is a basket line with the product fetched back from the backend,
// so every price on screen is the backend's price.
type CartEntry struct {
	Product backend.Product
	Qty     int
}

func (e CartEntry) LineTotal() float64 { return e.Product.Price * float64(e.Qty) }

// Cap mirrors Cart.capFor: null availableStock means the seed catalogue, which
// does not track stock — those lines stay uncapped.
func (e CartEntry) Cap() (int, bool) {
	if e.Product.AvailableStock == nil {
		return 0, false
	}
	return *e.Product.AvailableStock, true
}

// CartSubtotal mirrors the ChangeNotifier getter of the same name.
func CartSubtotal(entries []CartEntry) float64 {
	t := 0.0
	for _, e := range entries {
		t += e.LineTotal()
	}
	return t
}

// ChargesTotal is what a basket pays on top of its items; the API charges the same sum.
func ChargesTotal(cs []backend.Charge) float64 {
	t := 0.0
	for _, c := range cs {
		t += c.Amount
	}
	return math.Round(t*100) / 100
}

// ---------- wishlist ----------

func ReadWishlist(r *http.Request) map[string]bool {
	out := map[string]bool{}
	for _, raw := range readListCookie(r, WishlistCookie) {
		var id string
		if json.Unmarshal(raw, &id) == nil && id != "" {
			out[id] = true
		}
	}
	return out
}

func SaveWishlist(w http.ResponseWriter, ids map[string]bool) {
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	writeListCookie(w, WishlistCookie, list)
}

// CartShortfall counts lines asking for more than the shop has left.
func CartShortfall(entries []CartEntry) int {
	n := 0
	for _, e := range entries {
		if limit, ok := e.Cap(); ok && e.Qty > limit {
			n++
		}
	}
	return n
}
