package shop

// Presentation rules the Flutter storefront applies while drawing, ported so
// the website draws the same thing: category artwork (category_visual.dart),
// campaign palettes (campaign_palette.dart) and the home screen's copy.

import (
	"crypto/sha256"
	"encoding/base32"
	"regexp"
	"strconv"
	"strings"

	"lamazon/website/backend"
	"lamazon/website/tpl"
)

// AtlasIndex is CategoryVisual.indexFor: which cell of category-atlas-v2.webp
// (4 across, 2 down) a department is drawn with.
func AtlasIndex(name, parent string) int {
	n := strings.ToLower(name)
	for _, rule := range atlasRules {
		if rule.re.MatchString(n) {
			return rule.i
		}
	}
	if parent != "" && parent != name {
		return AtlasIndex(parent, "")
	}
	return 0
}

var atlasRules = []struct {
	re *regexp.Regexp
	i  int
}{
	{regexp.MustCompile(`groc|vegetable|fruit|rice|dairy|milk|oil`), 1},
	{regexp.MustCompile(`snack|drink|cookie|juice|beverage`), 7},
	{regexp.MustCompile(`station|game|book|paper|pen`), 6},
	{regexp.MustCompile(`house|clean|kitchen|detergent`), 5},
	{regexp.MustCompile(`beauty|skin|hair|care|cosmetic`), 4},
	{regexp.MustCompile(`gift|flower|decor`), 3},
	{regexp.MustCompile(`electro|headphone|cable|charger|phone|audio`), 2},
}

// AtlasStyle positions the atlas so only cell i shows.
func AtlasStyle(i int) string {
	x := float64(i%4) * 100 / 3
	y := float64(i / 4 * 100)
	return "background-image:url(/static/images/icons/category-atlas-v2.webp);background-size:400% 200%;background-position:" +
		strconv.FormatFloat(x, 'f', 3, 64) + "% " + strconv.FormatFloat(y, 'f', 0, 64) + "%"
}

var widthParam = regexp.MustCompile(`w=\d+`)

// ThumbWidth is thumb(url, width): the catalogue's non-Cloudinary links carry
// their size as w=, so asking for a smaller picture is a string swap.
func ThumbWidth(url string, width int) string {
	return widthParam.ReplaceAllString(url, "w="+strconv.Itoa(width))
}

// Glyph is glyphFor: the lucide icon a category plate is drawn with, chosen
// from its name, falling back to its department's icon.
func Glyph(category, fallback string) string {
	n := strings.ToLower(category)
	for _, g := range glyphs {
		if g.re.MatchString(n) {
			return g.icon
		}
	}
	return fallback
}

var glyphs = func() []struct {
	re   *regexp.Regexp
	icon string
} {
	table := [][2]string{
		{`baby|infant|diaper`, "baby"},
		{`toy|game|puzzle|play`, "gamepad-2"},
		{`glue|tape|adhesive|stapler|scissor`, "paperclip"},
		{`pen|pencil|marker|ink`, "pen-line"},
		{`notebook|book|diary|paper|register`, "book-open"},
		{`station|office|craft`, "pencil-ruler"},
		{`tea|coffee|chai`, "coffee"},
		{`juice|drink|beverage|soda|water|cola`, "cup-soda"},
		{`snack|chips|namkeen|biscuit|cookie|wafer`, "cookie"},
		{`chocolate|candy|sweet|dessert|ice ?cream`, "candy"},
		{`bread|bakery|cake|pastry|bun`, "croissant"},
		{`milk|dairy|curd|cheese|butter|paneer|yog`, "milk"},
		{`egg`, "egg"},
		{`fruit|apple|banana|mango`, "apple"},
		{`vegetable|veggie|sabzi|salad|green`, "carrot"},
		{`rice|atta|flour|grain|pulse|dal|masala|spice|oil`, "wheat"},
		{`meat|chicken|fish|egg|seafood`, "drumstick"},
		{`pizza`, "pizza"},
		{`burger|sandwich|roll|wrap`, "sandwich"},
		{`biryani|meal|thali|lunch|dinner|food|restaurant`, "utensils"},
		{`groc|kirana|essential|store cupboard`, "shopping-basket"},
		{`headphone|earphone|audio|speaker|sound`, "headphones"},
		{`batter|cell`, "battery-full"},
		{`charger|cable|power ?bank|adapter`, "cable"},
		{`mobile|phone|smartphone`, "smartphone"},
		{`laptop|computer|pc`, "laptop"},
		{`watch|wearable|band|fitness`, "watch"},
		{`camera|photo`, "camera"},
		{`electro|gadget|tech|accessor`, "plug"},
		{`skin|face|cream|lotion|moistur`, "sparkles"},
		{`hair|shampoo|oil ?hair`, "scissors"},
		{`makeup|lipstick|cosmetic|nail`, "brush"},
		{`soap|bath|hygiene|dental|tooth|razor|shav`, "droplets"},
		{`beauty|fragrance|perfume|deo`, "flower-2"},
		{`clean|detergent|wash|mop|broom|dish`, "spray-can"},
		{`kitchen|utensil|cookware|pan|bottle|container`, "cooking-pot"},
		{`house|home|furnish|decor ?home|storage`, "house"},
		{`light|bulb|lamp|candle`, "lightbulb"},
		{`gift|hamper|card|wrap ?gift`, "gift"},
		{`flower|plant|bouquet|garden`, "flower"},
		{`decor|party|balloon|festive`, "party-popper"},
		{`cloth|shirt|outfit|apparel|wear|dress|fashion`, "shirt"},
		{`shoe|footwear|sandal|slipper|sneaker`, "footprints"},
		{`bag|backpack|luggage|wallet`, "backpack"},
		{`medicine|pharma|health|first ?aid|tablet`, "pill"},
		{`pet|dog|cat`, "paw-print"},
		{`sport|gym|fitness|cricket|ball`, "dumbbell"},
		{`stap|hardware|tool|repair`, "wrench"},
	}
	out := make([]struct {
		re   *regexp.Regexp
		icon string
	}, len(table))
	for i, t := range table {
		out[i].re, out[i].icon = regexp.MustCompile(t[0]), t[1]
	}
	return out
}()

// DepartmentIcon is departmentIcon(name, key) from categories.dart.
func DepartmentIcon(name, key string) string {
	switch name {
	case "Snacks & Drinks":
		return "cookie"
	case "Stationery & Games":
		return "book-open"
	}
	if icon, ok := departmentIcons[key]; ok {
		return icon
	}
	return "tag"
}

var departmentIcons = map[string]string{
	"headphones": "headphones", "carrot": "carrot", "utensils": "utensils",
	"gift": "gift", "brush": "brush", "shirt": "shirt", "house": "house",
	"book": "book-open", "dumbbell": "dumbbell", "baby": "baby", "pill": "pill",
	"wrench": "wrench", "cookie": "cookie", "sprayCan": "spray-can",
	"cable": "cable", "tag": "tag",
}

// DepartmentHeadline is _departmentHeadline in home_screen.dart.
func DepartmentHeadline(name string) string {
	return [...]string{
		"A little delight, close by.",
		"A fresher kind of everyday.",
		"Small upgrades, beautifully useful.",
		"Something thoughtful, waiting nearby.",
		"Your everyday feel-good edit.",
		"Make home feel more like home.",
		"A little play goes a long way.",
		"Snack, sip, repeat.",
	}[AtlasIndex(name, "")]
}

// MixesStores is mixesStores: the store line only earns its place when a list
// holds more than one shop.
func MixesStores(items []backend.Product) bool {
	seen := ""
	for i, p := range items {
		if i == 0 {
			seen = p.Store
		} else if p.Store != seen {
			return true
		}
	}
	return false
}

// IsVideo is bannerKind(url) == BannerKind.video.
func IsVideo(url string) bool {
	path := strings.ToLower(strings.SplitN(url, "?", 2)[0])
	for _, ext := range []string{".mp4", ".webm", ".mov", ".m4v"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return strings.Contains(url, "/video/upload/")
}

// Palette is CampaignPalette: every colour a banner is drawn with.
type Palette struct{ Background, Foreground, Action, ActionForeground string }

var presets = map[string]Palette{
	"#143E32": {"#143E32", "#FFFFFF", "#C6EE63", "#143E32"},
	"#0E3B45": {"#0E3B45", "#FFFFFF", "#7FE3D4", "#0E3B45"},
	"#263244": {"#263244", "#FFFFFF", "#C8DBEE", "#263244"},
	"#2A2160": {"#2A2160", "#FFFFFF", "#C3B6FF", "#2A2160"},
	"#5B1D3D": {"#5B1D3D", "#FFFFFF", "#F7B7D2", "#5B1D3D"},
	"#7A1F12": {"#7A1F12", "#FFFFFF", "#F2B441", "#4A1008"},
	"#7A2E12": {"#7A2E12", "#FFFFFF", "#FFC46B", "#4A1B08"},
	"#4A3029": {"#4A3029", "#FFFFFF", "#F7A38E", "#4A3029"},
}

// ResolvePalette is CampaignPalette.resolve. seasonGround is the live
// season's ground, or "" outside a season.
func ResolvePalette(hex, seasonGround string) Palette {
	p, forest := resolvePalette(hex)
	if forest && seasonGround != "" {
		p, _ = resolvePalette(seasonGround)
	}
	return p
}

func resolvePalette(hex string) (Palette, bool) {
	n := strings.ToUpper(strings.TrimSpace(hex))
	if p, ok := presets[n]; ok {
		return p, n == "#143E32"
	}
	switch n {
	case "#F2E8CE", "#DCEACD", "#1D4A3C":
		return presets["#143E32"], true
	case "#F7DCCB", "#E7DFF3":
		return presets["#4A3029"], false
	case "#DCE9F5":
		return presets["#263244"], false
	}
	if len(n) != 7 || tpl.Contrast(n, n) == 0 || !isHex(n) {
		return presets["#143E32"], true
	}
	if tpl.Contrast("#17221D", n) >= 4.5 {
		return Palette{n, "#17221D", "#143E32", "#FFFFFF"}, false
	}
	bg := n
	for i := 0; i < 24 && tpl.Contrast("#FFFFFF", bg) < 4.5; i++ {
		bg = tpl.Blend("#000000", .08, bg)
	}
	return Palette{bg, "#FFFFFF", "#C6EE63", "#1D4939"}, false
}

func isHex(s string) bool {
	for _, c := range s[1:] {
		if !strings.ContainsRune("0123456789ABCDEF", c) {
			return false
		}
	}
	return s[0] == '#'
}

// DeliveryETA is catalog.dart's deliveryEta, which the app shows as a constant.
const DeliveryETA = "12 mins"

// OrderRef turns the internal database id into a stable, opaque reference.
// Sequence numbers reveal order volume and make neighbouring orders guessable;
// this display value deliberately has no visible relationship to that sequence.
func OrderRef(id string) string {
	sum := sha256.Sum256([]byte("uniminute-order-reference-v1:" + id))
	alphabet := base32.NewEncoding("23456789ABCDEFGHJKLMNPQRSTUVWXYZ").WithPadding(base32.NoPadding)
	return "UM-" + alphabet.EncodeToString(sum[:5])
}

// TitleCase is Dart's String.title the address label uses ("home" -> "Home").
func TitleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
