package httpapi

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mosipamo/URL-Shortener/internal/store"
)

func TestLogging(t *testing.T) {
	var buf bytes.Buffer
	// The logger is injected via New; no wrapper needed.
	h := New(store.New(), base, log.New(&buf, "", 0))

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
	// Build a minimal engine with only the Logger middleware and one route that
	// writes bytes without calling WriteHeader, so the default 200 must appear.
	r := gin.New()
	r.Use(Logger(log.New(&buf, "", 0)))
	r.GET("/", func(c *gin.Context) {
		_, _ = c.Writer.Write([]byte("hi"))
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(buf.String(), "200 OK") {
		t.Errorf("want 200 OK in log, got %q", buf.String())
	}
}

// Ensure the Logger middleware does not suppress errors set by c.Next().
func TestLoggingCapturesErrorStatus(t *testing.T) {
	var buf bytes.Buffer
	r := gin.New()
	r.Use(Logger(log.New(&buf, "", 0)))
	r.GET("/boom", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	if !strings.Contains(buf.String(), "500 Internal Server Error") {
		t.Errorf("want 500 in log, got %q", buf.String())
	}
}

// Discarded logger is used in other tests; verify io.Discard works as a sink.
func TestLoggingDiscard(t *testing.T) {
	h := New(store.New(), base, log.New(io.Discard, "", 0))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unknown", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
