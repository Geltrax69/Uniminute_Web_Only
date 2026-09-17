package handler

import (
	"net/http"

	site "lamazon/website"
)

// Vercel routes every request here (see vercel.json); the site is built once per instance.
var app = site.New(site.APIBase())

func Handler(w http.ResponseWriter, r *http.Request) {
	app.ServeHTTP(w, r)
}
