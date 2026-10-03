package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"url-shortener/internal/httpapi"
	"url-shortener/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	base := flag.String("base", "http://localhost:8080", "base URL used in short_url")
	flag.Parse()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(store.NewMemory(), *base),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("listening on %s (base %s)", *addr, *base)
	log.Fatal(srv.ListenAndServe())
}
