package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"lamazon/website/site"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8100"
	}
	apiBase := site.APIBase()
	log.Printf("Uniminute website on :%s, calling backend at %s", port, apiBase)
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: site.New(apiBase), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}
