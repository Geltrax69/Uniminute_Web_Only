package tpl

import (
	"strconv"
	"time"
)

// AssetVersion busts the month-long static cache on every server start, so a
// new build's CSS and JS are never shadowed by the last one's.
var AssetVersion = strconv.FormatInt(time.Now().Unix(), 36)

// AppVersion is the app's own version, pubspec.yaml's `version:` without the
// build number. Account, Settings and Help show it; bump it with the app.
const AppVersion = "1.0.1"
