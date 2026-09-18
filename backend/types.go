package main

import "time"

// Product is one listing in the catalog.
type Product struct {
	AvailableStock *int    `json:"availableStock,omitempty"`
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Category       string  `json:"category"`
	Tab            string  `json:"tab"`
	Price          float64 `json:"price"`
	// What it cost before the discount. Zero means there is no discount to
	// show — not a free item — so the badge and the struck-through line are
	// both driven off "is this above price", never off "is this set".
	MRP float64 `json:"mrp,omitempty"`
	// What the shopper picks before buying — size, colour, whatever this shop
	// sells by. Empty for the seeded catalogue and for anything sold one way.
	Options  []ItemOption `json:"options,omitempty"`
	ImageURL string       `json:"imageUrl"`
	// Every photo, in upload order. A seller's item can carry several; the
	// seeded rows have one. imageUrl stays the cover so old callers still work.
	ImageURLs   []string `json:"imageUrls"`
	Store       string   `json:"store"`
	Description string   `json:"description"`
	Offers      []Offer  `json:"offers,omitempty"`
}

// Offer is the same product priced at another vendor.
type Offer struct {
	Store string  `json:"store"`
	Price float64 `json:"price"`
}

type Shop struct {
	Name     string `json:"name"`
	Tagline  string `json:"tagline"`
	ImageURL string `json:"imageUrl"`
	Tab      string `json:"tab"`
}

type SellerStore struct {
	Owner      string   `json:"owner"` // email the seller signed in with
	Name       string   `json:"name"`
	Location   string   `json:"location"`
	City       string   `json:"city"`
	Categories []string `json:"categories"`
	PhotoURL   string   `json:"photoUrl"` // Cloudinary, empty until uploaded
	// pending until an admin looks at it, then approved or rejected with a
	// reason. Only an approved store is visible to shoppers or may hold stock.
	Status       string `json:"status"`
	RejectReason string `json:"rejectReason,omitempty"`
}

// InventoryItem is one line of a seller's stock.
type InventoryItem struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Price       float64 `json:"price"`
	MRP         float64 `json:"mrp"` // 0 when the seller is not running a discount
	// What the buyer chooses before ordering. Empty for most things — a
	// samosa has no size — so it stays out of the JSON when there is none.
	Options []ItemOption `json:"options,omitempty"`
	// Which products this one can be lined up against, and what it says for
	// that group's fields. Empty for anything not worth comparing.
	CompareGroup string            `json:"compareGroup"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Stock        int               `json:"stock"`
	// Hidden from the shop but kept whole. A product with orders against it
	// cannot be deleted — the orders are the record of a sale — so this is
	// how a seller retires one without destroying its history.
	Delisted bool `json:"delisted"`
	// Units held by orders that have not been delivered or rejected. What a
	// shopper can buy is Stock - Reserved, and the seller needs to see the
	// same arithmetic the shop does.
	Reserved int `json:"reserved"`
	// Units already delivered: what has actually sold.
	Sold int `json:"sold"`
	// Filled only by the admin listing, which spans every store. The seller's
	// own list leaves them zero: a seller knows whose shop they are looking at,
	// and counting orders per row on a screen they refresh constantly is work
	// nobody asked for.
	StoreName string   `json:"storeName,omitempty"`
	Owner     string   `json:"owner,omitempty"`
	Orders    int      `json:"orders,omitempty"`
	Status    string   `json:"status"`    // derived from stock, never stored
	ImageURLs []string `json:"imageUrls"` // Cloudinary, in upload order
}

// ItemOption is one thing a buyer picks, with the choices the shop offers.
// Kind decides how it draws: "colour" values are hex and become swatches,
// anything else is a row of labels.
type ItemOption struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	Values []string `json:"values"`
}

// LowStockAt is the threshold below which an item is flagged for restocking.
const LowStockAt = 5

func stockStatus(stock int) string {
	switch {
	case stock <= 0:
		return "sold_out"
	case stock <= LowStockAt:
		return "low"
	default:
		return "in_stock"
	}
}

type OrderStage string

const (
	StageReceived  OrderStage = "received"
	StageAccepted  OrderStage = "accepted"
	StageRejected  OrderStage = "rejected"
	StagePicked    OrderStage = "picked"
	StageDelivered OrderStage = "delivered"
)

// Order is one line bought from one store, and the whole trail behind it:
// who it is for, which store owes it, which rider has it.
//
// The receiver is copied in at the moment of ordering rather than joined from
// the address book — editing an address later must not redirect a bag that is
// already out.
type Order struct {
	ID          string     `json:"id"`
	ItemID      string     `json:"itemId"`
	ItemTitle   string     `json:"itemTitle"`
	Units       int        `json:"units"`
	Amount      float64    `json:"amount"`
	DeliveryFee float64    `json:"deliveryFee"`
	Stage       OrderStage `json:"stage"`
	Options     Choices    `json:"options"`
	PlacedAt    time.Time  `json:"placedAt"`

	StoreOwner string `json:"storeOwner,omitempty"`
	StoreName  string `json:"storeName,omitempty"`
	BuyerEmail string `json:"buyerEmail,omitempty"`

	// Where the rider collects. Read from the store rather than copied onto
	// the order: a shop that moves should move for orders already in flight,
	// which is the opposite of what we want for the delivery address.
	StoreAddress string `json:"storeAddress,omitempty"`

	// When it was handed over. Nil until it is, which is what the rider's
	// history sorts and groups on.
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`

	ReceiverName    string `json:"receiverName,omitempty"`
	ReceiverPhone   string `json:"receiverPhone,omitempty"`
	ReceiverAddress string `json:"receiverAddress,omitempty"`

	RejectReason string `json:"rejectReason,omitempty"`
	RiderPhone   string `json:"riderPhone,omitempty"`

	// Who the admin put on it, before anyone picks it up. Empty means the
	// first rider to claim it gets it.
	AssignedTo string `json:"assignedTo,omitempty"`

	// The four digits the buyer reads out at the door. Filled in only on the
	// buyer's own copy of the order; the rider is never told it, which is the
	// whole point of it.
	DeliveryCode string `json:"deliveryCode,omitempty"`
}

// DefaultOwner stands in until the app sends a real session. Callers can
// override it with ?owner= or the X-User-Email header.
const DefaultOwner = "demo@lamazon.app"
