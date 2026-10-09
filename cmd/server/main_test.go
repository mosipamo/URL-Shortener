package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mosipamo/URL-Shortener/internal/store"
)

func TestNewServerTimeouts(t *testing.T) {
	srv := newServer(":0", "http://localhost:8080", store.New(), defaultRateLimit, defaultRateWindow)
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("all timeouts must be set: %+v", srv)
	}
	if srv.ReadHeaderTimeout > srv.ReadTimeout {
		t.Fatalf("ReadHeaderTimeout (%v) should not exceed ReadTimeout (%v)", srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
}

func TestNewServerHandler(t *testing.T) {
	srv := newServer(":0", "https://sho.rt", store.New(), defaultRateLimit, defaultRateWindow)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/shorten",
		strings.NewReader(`{"url":"https://go.dev"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"short_url":"https://sho.rt/`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestRunCleanShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := run(ctx, []string{"-addr", ":9000", "-rate-limit", "3", "-rate-window", "2s"}, func(srv *http.Server) error {
		if srv.Addr != ":9000" {
			t.Errorf("Addr = %q", srv.Addr)
		}
		cancel()
		return http.ErrServerClosed
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunListenError(t *testing.T) {
	wantErr := errors.New("listen failed")
	err := run(context.Background(), nil, func(*http.Server) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("run() error = %v, want %v", err, wantErr)
	}
}

func TestRunInvalidFlag(t *testing.T) {
	err := run(context.Background(), []string{"-unknown"}, func(*http.Server) error {
		t.Fatal("listener should not be called")
		return nil
	})
	if err == nil {
		t.Fatal("expected flag parsing error")
	}
}

func TestOpenStoreMemory(t *testing.T) {
	s, closeFn, err := openStore("memory", "")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil store")
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenStoreErrors(t *testing.T) {
	if _, _, err := openStore("mongo", ""); err == nil || !strings.Contains(err.Error(), "unknown -store") {
		t.Errorf("unknown kind: err = %v", err)
	}
	if _, _, err := openStore("postgres", ""); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("missing dsn: err = %v", err)
	}
}

func TestOpenStorePostgresUnreachableDoesNotLeakPassword(t *testing.T) {
	const dsn = "postgres://shortener:supersecret@127.0.0.1:1/shortener?sslmode=disable&connect_timeout=1"
	_, _, err := openStore("postgres", dsn)
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("password leaked into error: %v", err)
	}
}
