package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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

func newServer(addr, base string, repo repository.Repository, rateLimit int, rateWindow time.Duration) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           httpapi.NewWithRateLimit(repo, base, log.Default(), rateLimit, rateWindow),
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

func run(ctx context.Context, args []string, listenAndServe func(*http.Server) error) error {
	if err := envfile.Load(".env"); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	flags := flag.NewFlagSet("server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", defaultAddr, "listen address")
	base := flags.String("base", defaultBase, "base URL used in short_url")
	storeKind := flags.String("store", "memory", "store kind (memory or postgres)")
	rateLimit := flags.Int("rate-limit", defaultRateLimit, "max shorten requests per client per rate-window")
	rateWindow := flags.Duration("rate-window", defaultRateWindow, "window for the shorten rate limiter")
	if err := flags.Parse(args); err != nil {
		return err
	}

	repo, closeStore, err := openStore(*storeKind, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer func() {
		if cerr := closeStore(); cerr != nil {
			log.Printf("closing store: %v", cerr)
		}
	}()

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	srv := newServer(*addr, *base, repo, *rateLimit, *rateWindow)
	log.Printf("listening on %s (base %s, store %s, rate-limit %d/%s)", *addr, *base, *storeKind, *rateLimit, *rateWindow)

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	err = listenAndServe(srv)
	stop()
	<-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Println("server stopped cleanly")
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], func(srv *http.Server) error {
		return srv.ListenAndServe()
	}); err != nil {
		log.Fatal(err)
	}
}
