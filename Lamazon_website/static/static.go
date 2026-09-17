// Package static bundles the site's CSS, JS, fonts and images into the binary.
package static

import "embed"

//go:embed css fonts images js
var Files embed.FS
