package shop

import (
	"lamazon/website/backend"
	"net/url"
	"strings"
)

// ProductPath uses the database's random public reference for navigation.
// Older APIs can roll forward independently; carts still use internal IDs.
func ProductPath(p backend.Product) string {
	base, picked := SplitLine(p.ID)
	if p.PublicID != "" {
		_, vendor, offered := strings.Cut(base, "@")
		base = p.PublicID
		if offered {
			base += "@" + vendor
		}
	}
	path := "/p/" + url.PathEscape(base)
	if len(picked) > 0 {
		_, query, _ := strings.Cut(LineID("", picked), "?")
		path += "?" + query
	}
	return path
}
