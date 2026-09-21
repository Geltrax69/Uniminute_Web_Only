package backend

// The single door to the existing Lamazon Go backend. Every screen in this
// website renders from data that came through here; nothing about the shop —
// prices, stock, orders, sessions — is reimplemented on this side.
//
// The shapes below mirror backend/types.go field-for-field (they only read
// JSON, so they stay in sync the way any API client does).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Backend is a tiny typed client for the routes registered in backend/main.go.
type Backend struct {
	base  string
	http  *http.Client
	cache *readCache
}

func NewBackend(base string) *Backend {
	return &Backend{
		base:  strings.TrimRight(base, "/"),
		http:  &http.Client{Timeout: 20 * time.Second},
		cache: &readCache{entries: map[string]*cached{}},
	}
}

// ---------- response shapes (mirrors of backend/types.go) ----------

type ItemOption struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	Values []string `json:"values"`
}

func (o ItemOption) IsColour() bool { return o.Kind == "colour" }

type Offer struct {
	Store string  `json:"store"`
	Price float64 `json:"price"`
}

type Product struct {
	AvailableStock *int           `json:"availableStock,omitempty"`
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Category       string         `json:"category"`
	Tab            string         `json:"tab"`
	Price          float64        `json:"price"`
	MRP            float64        `json:"mrp,omitempty"`
	Options        []ItemOption   `json:"options,omitempty"`
	VariantPrices  []VariantPrice `json:"variantPrices,omitempty"`
	ImageURL       string         `json:"imageUrl"`
	ImageURLs      []string       `json:"imageUrls"`
	Store          string         `json:"store"`
	Description    string         `json:"description"`
	Offers         []Offer        `json:"offers,omitempty"`
	CompareGroup   string         `json:"compareGroup,omitempty"`
}

type VariantPrice struct {
	Choices []Choice `json:"choices"`
	Price   float64  `json:"price"`
	MRP     float64  `json:"mrp,omitempty"`
}

// PriceFor returns the server-supplied price for an exact option combination.
// Legacy listings without a matrix keep their original single price.
func (p Product) PriceFor(picked []Choice) (float64, bool) {
	if len(p.VariantPrices) == 0 {
		return p.Price, true
	}
	// Nothing picked yet — the details page opens on the starting price.
	if len(picked) == 0 {
		return p.Price, true
	}
	for _, v := range p.VariantPrices {
		if len(v.Choices) != len(picked) {
			continue
		}
		match := true
		for _, c := range v.Choices {
			found := false
			for _, p := range picked {
				if p.Name == c.Name && p.Value == c.Value {
					found = true
					break
				}
			}
			if !found {
				match = false
				break
			}
		}
		if match {
			return v.Price, true
		}
	}
	return 0, false
}

// AnyDiscount is true when this listing shows a struck MRP for at least one
// combination, so the details page renders the MRP row Alpine then drives.
func (p Product) AnyDiscount() bool {
	if p.Discounted() {
		return true
	}
	for _, v := range p.VariantPrices {
		if v.MRP > v.Price {
			return true
		}
	}
	return false
}

// Discounted mirrors Product.discounted: an MRP equal to the price is a sale
// the seller has ended, not a 0% one worth a badge.
func (p Product) Discounted() bool { return p.MRP > p.Price }

func (p Product) DiscountPercent() int {
	if !p.Discounted() {
		return 0
	}
	return int(math.Round((p.MRP - p.Price) / p.MRP * 100))
}

// Choices mirrors Product.choices — a legacy `sizes` list becomes a Size group.
func (p Product) Choices() []ItemOption {
	if len(p.Options) > 0 {
		return p.Options
	}
	return nil
}

func (p Product) HasOptions() bool { return len(p.Choices()) > 0 }

func (p Product) Images() []string {
	if len(p.ImageURLs) > 0 {
		return p.ImageURLs
	}
	if p.ImageURL != "" {
		return []string{p.ImageURL}
	}
	return nil
}

type Shop struct {
	Name     string `json:"name"`
	Tagline  string `json:"tagline"`
	ImageURL string `json:"imageUrl"`
	Tab      string `json:"tab"`
}

type Category struct {
	Name     string     `json:"name"`
	Parent   string     `json:"parent,omitempty"`
	Icon     string     `json:"icon,omitempty"`
	Colour   string     `json:"colour,omitempty"`
	ImageURL string     `json:"imageUrl,omitempty"`
	Position int        `json:"position"`
	Children []Category `json:"children,omitempty"`
}

type Campaign struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle"`
	CTA        string `json:"cta"`
	Category   string `json:"category"`
	Department string `json:"department"`
	ImageURL   string `json:"imageUrl"`
	Colour     string `json:"colour"`
	Enabled    bool   `json:"enabled"`
	Position   int    `json:"position"`
}

type Season struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Ground string   `json:"ground"`
	Accent string   `json:"accent"`
	Ink    string   `json:"ink"`
	Hints  []string `json:"hints"`
}

type User struct {
	Email       string   `json:"email"`
	PublicID    string   `json:"id"`
	Name        string   `json:"name"`
	Phone       string   `json:"phone"`
	Roles       []string `json:"roles"`
	HasStore    bool     `json:"hasStore"`
	HasPassword bool     `json:"hasPassword"`
	HasAddress  bool     `json:"hasAddress"`
	Ready       bool     `json:"ready"`
}

type Address struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Line    string `json:"line"`
	City    string `json:"city"`
	Pincode string `json:"pincode"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Default bool   `json:"isDefault"`
}

type OrderStage string

const (
	StageReceived  OrderStage = "received"
	StageAccepted  OrderStage = "accepted"
	StageRejected  OrderStage = "rejected"
	StagePicked    OrderStage = "picked"
	StageDelivered OrderStage = "delivered"
)

type Order struct {
	ID                string     `json:"id"`
	ItemID            string     `json:"itemId"`
	ItemTitle         string     `json:"itemTitle"`
	Options           []Choice   `json:"options"`
	Units             int        `json:"units"`
	Amount            float64    `json:"amount"`
	DeliveryFee       float64    `json:"deliveryFee"`
	PaymentMethod     string     `json:"paymentMethod"`
	PaymentStatus     string     `json:"paymentStatus"`
	RazorpayOrderID   string     `json:"razorpayOrderId,omitempty"`
	RazorpayPaymentID string     `json:"razorpayPaymentId,omitempty"`
	Stage             OrderStage `json:"stage"`
	PlacedAt          time.Time  `json:"placedAt"`
	StoreName         string     `json:"storeName,omitempty"`
	ReceiverName      string     `json:"receiverName,omitempty"`
	ReceiverPhone     string     `json:"receiverPhone,omitempty"`
	RejectReason      string     `json:"rejectReason,omitempty"`
	DeliveryCode      string     `json:"deliveryCode,omitempty"`
	ReceiverAddress   string     `json:"receiverAddress,omitempty"`
	RiderPhone        string     `json:"riderPhone,omitempty"`
	AssignedTo        string     `json:"assignedTo,omitempty"`
}

// LoginStart is POST /api/login's answer: either "this address has a password,
// ask for it" or "a code is on its way".
type LoginStart struct {
	Email         string `json:"email"`
	NeedsPassword bool   `json:"needsPassword"`
	ExpiresAt     string `json:"expiresAt"`
	// Set only by a server running with the code switched off.
	Token        string `json:"token,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresIn    int    `json:"expiresIn,omitempty"`
}

// Session is POST /api/login/verify, /api/login/password and
// /api/login/refresh's answer.
type Session struct {
	Email        string `json:"email"`
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

// ---------- errors ----------

// APIError carries the backend's own message — its copy is written for the
// shopper ("not enough stock for …"), so it is shown as-is rather than
// paraphrased here.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("backend %d: %s", e.Status, e.Body)
}

// ---------- calls ----------

type freshKey struct{}

// Fresh marks reads that must skip the cache: staff screens show what was just
// saved, and on Vercel the save may have landed on another instance whose
// forget never reached this one.
func Fresh(ctx context.Context) context.Context { return context.WithValue(ctx, freshKey{}, true) }

func (b *Backend) do(ctx context.Context, method, path string, token string, in, out any) error {
	if method == http.MethodGet && ctx.Value(freshKey{}) == nil {
		raw, err := b.cache.getRaw(token, path, func(ctx context.Context) ([]byte, error) {
			return b.send(ctx, method, path, token, nil)
		}, ctx)
		if err != nil || out == nil || len(raw) == 0 {
			return err
		}
		return json.Unmarshal(raw, out)
	}
	raw, err := b.send(ctx, method, path, token, in)
	// A write changes what this token (or, signed out, anyone) would read.
	b.cache.forget(token)
	if err != nil || out == nil || len(raw) == 0 {
		return err
	}
	return json.Unmarshal(raw, out)
}

// send is one request to the API, answering the body of a 2xx.
func (b *Backend) send(ctx context.Context, method, path string, token string, in any) ([]byte, error) {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, body)
	if err != nil {
		return nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, &APIError{Status: res.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	return raw, nil
}

func (b *Backend) get(ctx context.Context, path string, token string, out any) error {
	return b.do(ctx, http.MethodGet, path, token, nil, out)
}

// GET /api/products/{id}
func (b *Backend) Product(ctx context.Context, id string) (Product, error) {
	var out Product
	err := b.get(ctx, "/api/products/"+url.PathEscape(id), "", &out)
	return out, err
}

// GET /api/shops
func (b *Backend) Shops(ctx context.Context, tab string) ([]Shop, error) {
	path := "/api/shops"
	if tab != "" && !strings.EqualFold(tab, "all") {
		path += "?tab=" + url.QueryEscape(tab)
	}
	var out []Shop
	err := b.get(ctx, path, "", &out)
	return out, err
}

// GET /api/shops/{name}/products — everything the shop sells at its own price.
func (b *Backend) ShopProducts(ctx context.Context, name string) ([]Product, error) {
	var out []Product
	err := b.get(ctx, "/api/shops/"+url.PathEscape(name)+"/products", "", &out)
	return out, err
}

// GET /api/categories — the whole navigation tree.
func (b *Backend) Categories(ctx context.Context) ([]Category, error) {
	var out []Category
	err := b.get(ctx, "/api/categories", "", &out)
	return out, err
}

// Charge is one line a basket pays on top of its items (Delivery, packaging, …).
type Charge struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

// GET /api/charges — Delivery first, then whatever an admin added.
func (b *Backend) Charges(ctx context.Context) ([]Charge, error) {
	var out []Charge
	err := b.get(ctx, "/api/charges", "", &out)
	return out, err
}

// GET /api/campaigns — enabled banners, in merchandising order.
func (b *Backend) Campaigns(ctx context.Context) ([]Campaign, error) {
	var out []Campaign
	err := b.get(ctx, "/api/campaigns", "", &out)
	return out, err
}

// GET /api/storefront/season — {"season": null} most of the year.
func (b *Backend) Season(ctx context.Context) (*Season, error) {
	var out struct {
		Season *Season `json:"season"`
	}
	err := b.get(ctx, "/api/storefront/season", "", &out)
	return out.Season, err
}

// GET /api/locations — serviceable cities and the promised eta.
func (b *Backend) Locations(ctx context.Context) (cities []string, eta string, err error) {
	var out struct {
		Cities []string `json:"cities"`
		ETA    string   `json:"eta"`
	}
	err = b.get(ctx, "/api/locations", "", &out)
	return out.Cities, out.ETA, err
}

// POST /api/login — code forces a mailed code even when there is a password.
func (b *Backend) StartLogin(ctx context.Context, email string, code bool) (LoginStart, error) {
	var out LoginStart
	err := b.do(ctx, http.MethodPost, "/api/login", "", map[string]any{"email": email, "code": code}, &out)
	return out, err
}

// POST /api/login/forgot — mails a password-reset code.
func (b *Backend) ForgotPassword(ctx context.Context, email string) error {
	var out LoginStart
	return b.do(ctx, http.MethodPost, "/api/login/forgot", "", map[string]string{"email": email}, &out)
}

// POST /api/login/verify
func (b *Backend) VerifyCode(ctx context.Context, email, code string) (Session, error) {
	var out Session
	err := b.do(ctx, http.MethodPost, "/api/login/verify", "",
		map[string]string{"email": email, "code": code}, &out)
	return out, err
}

// POST /api/login/password
func (b *Backend) PasswordLogin(ctx context.Context, email, password string) (Session, error) {
	var out Session
	err := b.do(ctx, http.MethodPost, "/api/login/password", "",
		map[string]string{"email": email, "password": password}, &out)
	return out, err
}

// POST /api/login/reset — a mailed code and a new password.
func (b *Backend) ResetPassword(ctx context.Context, email, code, password string) (Session, error) {
	var out Session
	err := b.do(ctx, http.MethodPost, "/api/login/reset", "",
		map[string]string{"email": email, "code": code, "password": password}, &out)
	return out, err
}

// POST /api/login/refresh
func (b *Backend) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	var out Session
	err := b.do(ctx, http.MethodPost, "/api/login/refresh", "",
		map[string]string{"refreshToken": refreshToken}, &out)
	return out, err
}

// GET /api/me
func (b *Backend) Me(ctx context.Context, token string) (User, error) {
	var out User
	err := b.get(ctx, "/api/me", token, &out)
	return out, err
}

// PATCH /api/me
func (b *Backend) UpdateMe(ctx context.Context, token string, changes map[string]any) error {
	return b.do(ctx, http.MethodPatch, "/api/me", token, changes, nil)
}

// GET /api/addresses
func (b *Backend) Addresses(ctx context.Context, token string) ([]Address, error) {
	var out []Address
	err := b.get(ctx, "/api/addresses", token, &out)
	return out, err
}

// POST /api/addresses (create) and PATCH /api/addresses/{id} (edit) share one
// handler on the backend; the payload is the Address itself.
func (b *Backend) SaveAddress(ctx context.Context, token string, a Address, id string) (Address, error) {
	method, path := http.MethodPost, "/api/addresses"
	if id != "" {
		method, path = http.MethodPatch, "/api/addresses/"+url.PathEscape(id)
	}
	var out Address
	err := b.do(ctx, method, path, token, a, &out)
	return out, err
}

func (b *Backend) DeleteAddress(ctx context.Context, token, id string) error {
	return b.do(ctx, http.MethodDelete, "/api/addresses/"+url.PathEscape(id), token, nil, nil)
}

func (b *Backend) DefaultAddress(ctx context.Context, token, id string) error {
	return b.do(ctx, http.MethodPatch, "/api/addresses/"+url.PathEscape(id)+"/default", token, nil, nil)
}

// POST /api/orders/checkout — commits every line or none.
func (b *Backend) Checkout(ctx context.Context, token string, lines []CheckoutLine, addressID, requestID string, expectedTotal float64, payment *PaymentProof) (CheckoutResult, error) {
	in := struct {
		Lines         []CheckoutLine `json:"lines"`
		RequestID     string         `json:"requestId"`
		AddressID     string         `json:"addressId"`
		ExpectedTotal *float64       `json:"expectedTotal"`
		Payment       *PaymentProof  `json:"payment,omitempty"`
	}{Lines: lines, RequestID: requestID, AddressID: addressID, ExpectedTotal: &expectedTotal, Payment: payment}
	var out CheckoutResult
	err := b.do(ctx, http.MethodPost, "/api/orders/checkout", token, in, &out)
	return out, err
}

type CheckoutLine struct {
	ItemID  string   `json:"itemId"`
	Units   int      `json:"units"`
	Options []Choice `json:"options,omitempty"`
}

// Choice is one option the buyer picked, e.g. {Colour, Black}.
type Choice struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CheckoutResult struct {
	Orders      []Order `json:"orders"`
	Amount      float64 `json:"amount"`
	DeliveryFee float64 `json:"deliveryFee"`
}

// PaymentProof is the successful Razorpay Checkout response. The API verifies
// the signature again before it records an order as paid.
type PaymentProof struct {
	PaymentID string `json:"razorpay_payment_id"`
	OrderID   string `json:"razorpay_order_id"`
	Signature string `json:"razorpay_signature"`
}

// GET /api/orders — the buyer's own orders, newest first.
func (b *Backend) MyOrders(ctx context.Context, token string) ([]Order, error) {
	var out struct {
		Orders []Order `json:"orders"`
	}
	err := b.get(ctx, "/api/orders", token, &out)
	return out.Orders, err
}

// POST /api/orders/{id}/cancel
func (b *Backend) CancelOrder(ctx context.Context, token, id string) error {
	return b.do(ctx, http.MethodPost, "/api/orders/"+url.PathEscape(id)+"/cancel", token, map[string]any{}, nil)
}

// Policy is one published document from GET /api/policies.
type Policy struct {
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Published bool      `json:"published"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GET /api/policies
func (b *Backend) Policies(ctx context.Context) ([]Policy, error) {
	var out []Policy
	err := b.get(ctx, "/api/policies", "", &out)
	return out, err
}

// Preferences is GET /api/preferences; PATCH takes the same keys.
type Preferences struct {
	Push         bool `json:"push"`
	EmailOffers  bool `json:"emailOffers"`
	OrderUpdates bool `json:"orderUpdates"`
}

func (b *Backend) GetPreferences(ctx context.Context, token string) (Preferences, error) {
	var out Preferences
	err := b.get(ctx, "/api/preferences", token, &out)
	return out, err
}

func (b *Backend) PatchPreferences(ctx context.Context, token string, changes map[string]bool) error {
	return b.do(ctx, http.MethodPatch, "/api/preferences", token, changes, nil)
}

// ---------- admin ----------

// AdminLogin is POST /api/admin/login.
func (b *Backend) AdminLogin(ctx context.Context, username, password string) (token, user string, err error) {
	var out struct {
		Token    string `json:"token"`
		Username string `json:"username"`
	}
	err = b.do(ctx, http.MethodPost, "/api/admin/login", "", map[string]string{"username": username, "password": password}, &out)
	return out.Token, out.Username, err
}

// Get reads any backend route with a token into out. The admin panel reads
// a dozen lists whose rows it only displays, so they stay loosely typed.
func (b *Backend) Get(ctx context.Context, path, token string, out any) error {
	return b.get(ctx, path, token, out)
}

// ---------- delivery ----------

// RiderOrder is one order as the rider panel sees it: both ends of the run,
// and never the delivery code.
type RiderOrder struct {
	Options         []Choice   `json:"options"`
	ID              string     `json:"id"`
	Stage           OrderStage `json:"stage"`
	AssignedTo      string     `json:"assignedTo"`
	Amount          float64    `json:"amount"`
	PaymentMethod   string     `json:"paymentMethod"`
	PaymentStatus   string     `json:"paymentStatus"`
	Units           int        `json:"units"`
	ItemTitle       string     `json:"itemTitle"`
	StoreName       string     `json:"storeName"`
	StoreAddress    string     `json:"storeAddress"`
	ReceiverName    string     `json:"receiverName"`
	ReceiverPhone   string     `json:"receiverPhone"`
	ReceiverAddress string     `json:"receiverAddress"`
	DeliveredAt     *time.Time `json:"deliveredAt,omitempty"`
}

// RiderPanel is GET /api/delivery/orders.
type RiderPanel struct {
	Rider struct {
		Name      string `json:"name"`
		Delivered int    `json:"delivered"`
	} `json:"rider"`
	Orders []RiderOrder `json:"orders"`
}

// RiderHistory is GET /api/delivery/history.
type RiderHistory struct {
	Orders []RiderOrder `json:"orders"`
	Value  float64      `json:"value"`
}

// RiderSession is POST /api/delivery/login's answer.
type RiderSession struct {
	Token string `json:"token"`
	Phone string `json:"phone"`
	Name  string `json:"name"`
}

func (b *Backend) RiderLogin(ctx context.Context, phone, pin string) (RiderSession, error) {
	var out RiderSession
	err := b.do(ctx, http.MethodPost, "/api/delivery/login", "", map[string]string{"phone": phone, "pin": pin}, &out)
	return out, err
}

func (b *Backend) RiderPanelFor(ctx context.Context, token string) (RiderPanel, error) {
	var out RiderPanel
	err := b.get(ctx, "/api/delivery/orders", token, &out)
	return out, err
}

func (b *Backend) RiderHistoryFor(ctx context.Context, token string) (RiderHistory, error) {
	var out RiderHistory
	err := b.get(ctx, "/api/delivery/history", token, &out)
	return out, err
}

func (b *Backend) RiderPick(ctx context.Context, token, id string) error {
	return b.do(ctx, http.MethodPost, "/api/delivery/orders/"+url.PathEscape(id)+"/pick", token, map[string]string{}, nil)
}

func (b *Backend) RiderDeliver(ctx context.Context, token, id, code string) error {
	return b.do(ctx, http.MethodPost, "/api/delivery/orders/"+url.PathEscape(id)+"/deliver", token, map[string]string{"code": code}, nil)
}

// ---------- selling ----------

// SellerStore is GET /api/seller/store: the signed-in seller's own store.
type SellerStore struct {
	Name         string   `json:"name"`
	Location     string   `json:"location"`
	City         string   `json:"city"`
	Categories   []string `json:"categories"`
	PhotoURL     string   `json:"photoUrl"`
	Status       string   `json:"status"`
	RejectReason string   `json:"rejectReason"`
}

func (s SellerStore) Approved() bool { return s.Status == "" || s.Status == "approved" }

// InventoryItem is one line of the seller's stock.
type InventoryItem struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Category      string            `json:"category"`
	Price         float64           `json:"price"`
	MRP           float64           `json:"mrp"`
	Options       []ItemOption      `json:"options"`
	VariantPrices []VariantPrice    `json:"variantPrices,omitempty"`
	CompareGroup  string            `json:"compareGroup"`
	Attributes    map[string]string `json:"attributes"`
	Stock         int               `json:"stock"`
	Delisted      bool              `json:"delisted"`
	Reserved      int               `json:"reserved"`
	Sold          int               `json:"sold"`
	ImageURLs     []string          `json:"imageUrls"`
	// Filled only by the admin catalogue, which spans every store.
	StoreName string `json:"storeName,omitempty"`
	Owner     string `json:"owner,omitempty"`
	Orders    int    `json:"orders,omitempty"`
}

// Available is what the shop will actually sell: stock minus live orders.
func (i InventoryItem) Available() int { return max(i.Stock-i.Reserved, 0) }

// CompareGroup is one entry of GET /api/compare-groups.
type CompareGroup struct {
	Name       string             `json:"name"`
	Attributes []CompareAttribute `json:"attributes"`
	Items      int                `json:"items"`
}

// SellerStoreFor returns nil, nil when the seller has not opened a store yet.
func (b *Backend) SellerStoreFor(ctx context.Context, token string) (*SellerStore, error) {
	var out SellerStore
	err := b.get(ctx, "/api/seller/store", token, &out)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Backend) SellerItems(ctx context.Context, token string) ([]InventoryItem, error) {
	var out struct {
		Items []InventoryItem `json:"items"`
	}
	err := b.get(ctx, "/api/seller/items", token, &out)
	return out.Items, err
}

func (b *Backend) SellerOrders(ctx context.Context, token string) ([]Order, error) {
	var out struct {
		Orders []Order `json:"orders"`
	}
	err := b.get(ctx, "/api/seller/orders", token, &out)
	return out.Orders, err
}

func (b *Backend) CompareGroups(ctx context.Context) ([]CompareGroup, error) {
	var out []CompareGroup
	err := b.get(ctx, "/api/compare-groups", "", &out)
	return out, err
}

// Comparison is GET /api/compare?group=: the group's fields, the products in
// it cheapest first, the computed per-unit rows, and the findings.
type Comparison struct {
	Group      string             `json:"group"`
	Attributes []CompareAttribute `json:"attributes"`
	Derived    []CompareDerived   `json:"derived"`
	Highlights []string           `json:"highlights"`
	Products   []CompareRow       `json:"products"`
}

type CompareAttribute struct {
	Name    string `json:"name"`
	Unit    string `json:"unit"`
	Mode    string `json:"mode"`
	PerUnit bool   `json:"perUnit"`
	Winner  string `json:"winner"`
}

type CompareDerived struct {
	Name   string             `json:"name"`
	Values map[string]float64 `json:"values"`
	Winner string             `json:"winner"`
}

type CompareRow struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Price    float64           `json:"price"`
	MRP      float64           `json:"mrp,omitempty"`
	Store    string            `json:"store"`
	ImageURL string            `json:"imageUrl"`
	Values   map[string]string `json:"values"`
}

// GET /api/compare?group=
func (b *Backend) Compare(ctx context.Context, group string) (Comparison, error) {
	var out Comparison
	err := b.get(ctx, "/api/compare?group="+url.QueryEscape(group), "", &out)
	return out, err
}

// GET /api/products?q=&tab=&category=
func (b *Backend) Products(ctx context.Context, q, tab, category string) ([]Product, error) {
	params := url.Values{}
	if q != "" {
		params.Set("q", q)
	}
	if tab != "" && !strings.EqualFold(tab, "all") {
		params.Set("tab", tab)
	}
	if category != "" {
		params.Set("category", category)
	}
	path := "/api/products"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var out []Product
	err := b.get(ctx, path, "", &out)
	return out, err
}

// Review is a buyer's verdict on one delivered order: the product and the rider.
type Review struct {
	OrderID     string    `json:"orderId"`
	ItemID      string    `json:"itemId"`
	ItemTitle   string    `json:"itemTitle"`
	StoreName   string    `json:"storeName"`
	BuyerEmail  string    `json:"buyerEmail"`
	BuyerName   string    `json:"buyerName"`
	ItemRating  int       `json:"itemRating"`
	ItemText    string    `json:"itemText"`
	RiderPhone  string    `json:"riderPhone"`
	RiderName   string    `json:"riderName"`
	RiderRating int       `json:"riderRating"`
	RiderText   string    `json:"riderText"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ProductReviews is what shoppers see: the average and the written reviews.
type ProductReviews struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
	Reviews []struct {
		Rating int       `json:"rating"`
		Text   string    `json:"text"`
		Name   string    `json:"name"`
		At     time.Time `json:"at"`
	} `json:"reviews"`
}

// GET /api/orders/reviews — the signed-in buyer's reviews, by order.
func (b *Backend) MyReviews(ctx context.Context, token string) (map[string]Review, error) {
	var list []Review
	err := b.get(ctx, "/api/orders/reviews", token, &list)
	out := map[string]Review{}
	for _, r := range list {
		out[r.OrderID] = r
	}
	return out, err
}

// GET /api/products/{id}/reviews
func (b *Backend) ProductReviews(ctx context.Context, id string) (ProductReviews, error) {
	var out ProductReviews
	err := b.get(ctx, "/api/products/"+url.PathEscape(id)+"/reviews", "", &out)
	return out, err
}

// PUT /api/orders/{id}/review
func (b *Backend) SaveReview(ctx context.Context, token, orderID string, itemRating int, itemText string, riderRating int, riderText string) error {
	return b.do(ctx, http.MethodPut, "/api/orders/"+url.PathEscape(orderID)+"/review", token, map[string]any{
		"itemRating": itemRating, "itemText": itemText, "riderRating": riderRating, "riderText": riderText,
	}, nil)
}

// Notification is one entry in the in-app inbox.
type Notification struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Target    string     `json:"target"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt"`
}

// Inbox is the latest notifications and how many are unread.
type Inbox struct {
	Unread int            `json:"unread"`
	Items  []Notification `json:"items"`
}

// GET /api/notifications
func (b *Backend) Notifications(ctx context.Context, token string) (Inbox, error) {
	var out Inbox
	err := b.get(Fresh(ctx), "/api/notifications", token, &out)
	return out, err
}

// POST /api/notifications/read
func (b *Backend) MarkNotificationsRead(ctx context.Context, token string) error {
	return b.do(ctx, http.MethodPost, "/api/notifications/read", token, nil, nil)
}
