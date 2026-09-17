package viewdata

// What every page carries: the chrome the shop layout draws (title, session,
// season skin, cart badge, delivery address) so a handler builds one struct
// and every template below it reads the same fields.

import (
	"lamazon/website/backend"
	"lamazon/website/tpl"
)

type Page struct {
	Title       string
	Description string
	Canonical   string

	User        *backend.User    // nil when browsing as a guest
	Season      *backend.Season
	Address     *backend.Address // the default delivery address, when signed in
	AccessToken string           // the live JWT, for server-side API calls

	CartCount   int
	WishlistLen int
	Wishlist    map[string]bool  // product IDs the shopper has saved
	Query       string
	ActiveTab   string // "" means All
}

// SeasonCSS turns the live season into CSS custom properties; the no-season
// defaults are the shop's own forest chrome (_ServiceHeader's gradient).
//
// --season-ground-shade is the header's second stop (forest→strong outside a
// season); --season-plate-shade is SeasonSkin.groundShade, the ground under
// 18% black, which the category plates use.
func (p Page) SeasonCSS() string {
	ground, header2, accent, ink := "#143E32", "#1D4939", "#C6EE63", "#FFFFFF"
	if p.Season != nil && p.Season.Ground != "" {
		ground = p.Season.Ground
		header2 = Shade(ground)
		if p.Season.Accent != "" {
			accent = p.Season.Accent
		}
		if p.Season.Ink != "" {
			ink = p.Season.Ink
		}
	}
	return "--season-ground:" + ground + ";--season-ground-shade:" + header2 +
		";--season-plate-shade:" + Shade(ground) + ";--season-accent:" + accent +
		";--season-on-accent:" + tpl.Readable(accent) + ";--season-ink:" + ink
}

// Shade is SeasonSkin.groundShade: the ground under 18% black.
func Shade(hex string) string { return tpl.Blend("#000000", .18, hex) }
