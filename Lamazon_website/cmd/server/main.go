package main

import (
	"log"
	"net/http"
	"os"

	"lamazon/website/site"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8100"
	}
	apiBase := site.APIBase()
	log.Printf("Uniminute website on :%s, calling backend at %s", port, apiBase)
	log.Fatal(http.ListenAndServe(":"+port, site.New(apiBase)))
}
