package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/mosipamo/URL-Shortener/internal/envfile"
	"github.com/mosipamo/URL-Shortener/internal/httpapi"
	"github.com/mosipamo/URL-Shortener/internal/store"
)

const (
	defaultAddr       = ":8080"
	defaultBase       = "http://localhost:8080"
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
)

func newServer(addr, base string, s httpapi.Store) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           httpapi.Logging(log.Default(), httpapi.New(s, base)),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// openStore builds the store selected by -store. It returns the store and a
// function that releases its resources. The DSN contains a password, so it is
// never printed or included in error messages here.
func openStore(kind, dsn string) (httpapi.Store, func() error, error) {
	switch kind {
	case "memory":
		return store.New(), func() error { return nil }, nil
	case "postgres":
		if dsn == "" {
			return nil, nil, errors.New("-store postgres needs -dsn or the DATABASE_URL environment variable")
		}
		db, err := store.OpenPostgres(dsn)
		if err != nil {
			return nil, nil, err
		}
		g, err := store.NewGorm(db) // creates the table; earlier links are now available
		if err != nil {
			return nil, nil, err
		}
		n, err := g.Count()
		if err != nil {
			_ = g.Close()
			return nil, nil, err
		}
		log.Printf("postgres store ready: %d existing links", n)
		return g, g.Close, nil
	default:
		return nil, nil, fmt.Errorf("unknown -store %q (use memory or postgres)", kind)
	}
}

func main() {
	if err := envfile.Load(".env"); err != nil {
		log.Fatalf("config: %v", err)
	}

	addr := flag.String("addr", defaultAddr, "listen address")
	base := flag.String("base", defaultBase, "base URL used in short_url")
	flag.Parse()

	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load .env: %v", err)
	}

	kind := os.Getenv("STORE")
	s, closeStore, err := openStore(kind, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	srv := newServer(*addr, *base, s)
	log.Printf("listening on %s (base %s, store %s)", *addr, *base, kind)
	err = srv.ListenAndServe()
	if cerr := closeStore(); cerr != nil {
		log.Printf("closing store: %v", cerr)
	}
	log.Fatal(err)
}
