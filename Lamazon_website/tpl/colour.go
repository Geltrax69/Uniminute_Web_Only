package tpl

// Colour arithmetic the Flutter app does at paint time (Color.alphaBlend,
// withValues(alpha:), computeLuminance), done here once per render instead.

import (
	"fmt"
	"math"
)

type rgb struct{ r, g, b float64 }

func parse(hex string) (rgb, bool) {
	var r, g, b int
	if len(hex) != 7 || hex[0] != '#' {
		return rgb{}, false
	}
	if _, err := fmt.Sscanf(hex[1:], "%02x%02x%02x", &r, &g, &b); err != nil {
		return rgb{}, false
	}
	return rgb{float64(r), float64(g), float64(b)}, true
}

func (c rgb) hex() string {
	return fmt.Sprintf("#%02X%02X%02X", int(math.Round(c.r)), int(math.Round(c.g)), int(math.Round(c.b)))
}

// Blend is Color.alphaBlend(fg.withValues(alpha: a), bg).
func Blend(fg string, a float64, bg string) string {
	f, ok1 := parse(fg)
	b, ok2 := parse(bg)
	if !ok1 || !ok2 {
		return bg
	}
	return rgb{f.r*a + b.r*(1-a), f.g*a + b.g*(1-a), f.b*a + b.b*(1-a)}.hex()
}

// RGBA is hex.withValues(alpha: a) as a CSS colour.
func RGBA(hex string, a float64) string {
	c, ok := parse(hex)
	if !ok {
		return hex
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", int(c.r), int(c.g), int(c.b), a)
}

func luminance(c rgb) float64 {
	ch := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.r) + 0.7152*ch(c.g) + 0.0722*ch(c.b)
}

// Contrast is the WCAG ratio between two hex colours.
func Contrast(a, b string) float64 {
	x, _ := parse(a)
	y, _ := parse(b)
	lx, ly := luminance(x), luminance(y)
	if lx < ly {
		lx, ly = ly, lx
	}
	return (lx + 0.05) / (ly + 0.05)
}

// Readable is CampaignPalette.readable: text ink where it clears 4.5:1, else white.
func Readable(ground string) string {
	if Contrast("#17221D", ground) >= 4.5 {
		return "#17221D"
	}
	return "#FFFFFF"
}
