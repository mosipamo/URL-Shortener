package httpapi

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mosipamo/URL-Shortener.git/internal/store"
)

func TestLogging(t *testing.T) {
	var buf bytes.Buffer
	h := Logging(log.New(&buf, "", 0), New(store.New(), base))

	h.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{"url":"https://example.com"}`)))
	h.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/nope?token=secret", nil))

	out := buf.String()
	for _, want := range []string{"201 Created", "POST /api/shorten", "404 Not Found", "GET  /nope"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret") {
		t.Errorf("query string leaked into log:\n%s", out)
	}
}

func TestLoggingDefaultStatus200(t *testing.T) {
	var buf bytes.Buffer
	h := Logging(log.New(&buf, "", 0), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hi"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(buf.String(), "200 OK") {
		t.Errorf("want 200 OK in %q", buf.String())
	}
}
