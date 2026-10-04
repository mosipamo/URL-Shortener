package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/mosipamo/URL-Shortener.git/internal/httpapi"
	"github.com/mosipamo/URL-Shortener.git/internal/store"
)

const (
	defaultAddr       = ":8080"
	defaultBase       = "http://localhost:8080"
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
)

func newServer(addr, base string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           httpapi.Logging(log.Default(), httpapi.New(store.New(), base)),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	base := flag.String("base", defaultBase, "base URL used in short_url")
	flag.Parse()

	srv := newServer(*addr, *base)

	log.Printf("listening on %s (base %s)", *addr, *base)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("could not start server: %v", err)
	}
}
