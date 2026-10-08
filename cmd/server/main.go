package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mosipamo/URL-Shortener/internal/envfile"
	"github.com/mosipamo/URL-Shortener/internal/httpapi"
	"github.com/mosipamo/URL-Shortener/internal/repository"
	"github.com/mosipamo/URL-Shortener/internal/store"
)

const (
	defaultAddr       = ":8080"
	defaultBase       = "http://localhost:8080"
	defaultRateLimit  = 60
	defaultRateWindow = time.Minute
	shutdownTimeout   = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
)

func newServer(addr, base string, repo repository.Repository) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(repo, base, log.Default()),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func openStore(kind, dsn string) (repository.Repository, func() error, error) {
	switch kind {
	case "memory":
		return store.New(), func() error { return nil }, nil
	case "postgres":
		if dsn == "" {
			return nil, nil, errors.New("-store postgres requires DATABASE_URL to be set in .env")
		}
		db, err := store.OpenPostgres(dsn)
		if err != nil {
			return nil, nil, err
		}
		g, err := store.NewGorm(db)
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
	store := flag.String("store", "memory", "store kind (memory or postgres)")
	rateLimit := flag.Int("rate-limit", defaultRateLimit, "max shorten requests per client per rate-window")
	rateWindow := flag.Duration("rate-window", defaultRateWindow, "window for the shorten rate limiter")
	flag.Parse()

	repo, closeStore, err := openStore(*store, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer func() {
		if cerr := closeStore(); cerr != nil {
			log.Printf("closing store: %v", cerr)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := newServer(*addr, *base, repo)
	httpapiHandler := httpapi.NewWithRateLimit(repo, *base, log.Default(), *rateLimit, *rateWindow)
	srv.Handler = httpapiHandler
	log.Printf("listening on %s (base %s, store %s, rate-limit %d/%s)", *addr, *base, *store, *rateLimit, *rateWindow)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	log.Println("server stopped cleanly")
}
