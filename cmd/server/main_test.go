package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewServerTimeouts(t *testing.T) {
	srv := newServer(":0", "http://localhost:8080")
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("all timeouts must be set: %+v", srv)
	}
	if srv.ReadHeaderTimeout > srv.ReadTimeout {
		t.Fatalf("ReadHeaderTimeout (%v) should not exceed ReadTimeout (%v)", srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
}

func TestNewServerHandler(t *testing.T) {
	srv := newServer(":0", "https://sho.rt")
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
