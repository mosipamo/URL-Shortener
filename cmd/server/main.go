package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"time"

	"url-shortener/internal/httpapi"
	"url-shortener/internal/store"
)

const (
	defaultAddr = ":8080"
	defaultBase = "http://localhost:8080"
)

func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	base := flag.String("base", defaultBase, "base URL used in short_url")
	flag.Parse()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(store.New(), *base),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("listening on %s (base %s)", *addr, *base)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("could not start server: %v", err)
	}
}
