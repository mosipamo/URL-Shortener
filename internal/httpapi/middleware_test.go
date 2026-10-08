package httpapi

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mosipamo/URL-Shortener/internal/store"
)

func TestLogging(t *testing.T) {
	var buf bytes.Buffer
	h := New(store.New(), base, log.New(&buf, "", 0))

	h.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{"url":"https://example.com/path?token=body-secret"}`)))
	h.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/nope?token=secret", nil))

	out := buf.String()
	for _, want := range []string{`"msg":"http request"`, `"method":"POST"`, `"route":"/api/shorten"`, `"status":201`, `"status_text":"Created"`, `"method":"GET"`, `"route":"/:code"`, `"status":404`, `"status_text":"Not Found"`, `"duration_ms":`} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
	for _, sensitive := range []string{"secret", "https://example.com/path"} {
		if strings.Contains(out, sensitive) {
			t.Errorf("sensitive URL data %q leaked into log:\n%s", sensitive, out)
		}
	}
}

func TestLoggingDefaultStatus200(t *testing.T) {
	var buf bytes.Buffer
	r := gin.New()
	r.Use(Logger(log.New(&buf, "", 0)))
	r.GET("/", func(c *gin.Context) {
		_, _ = c.Writer.Write([]byte("hi"))
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(buf.String(), `"status":200`) || !strings.Contains(buf.String(), `"status_text":"OK"`) {
		t.Errorf("want structured 200 status in log, got %q", buf.String())
	}
}

func TestLoggingCapturesErrorStatus(t *testing.T) {
	var buf bytes.Buffer
	r := gin.New()
	r.Use(Logger(log.New(&buf, "", 0)))
	r.GET("/boom", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	if !strings.Contains(buf.String(), `"level":"ERROR"`) || !strings.Contains(buf.String(), `"status":500`) {
		t.Errorf("want structured 500 error in log, got %q", buf.String())
	}
}

func TestLoggingDiscard(t *testing.T) {
	h := New(store.New(), base, log.New(io.Discard, "", 0))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unknown", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRateLimitShorten(t *testing.T) {
	h := NewWithRateLimit(store.New(), base, log.New(io.Discard, "", 0), 1, time.Minute)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/shorten",
			strings.NewReader(`{"url":"https://example.com/`+strconv.Itoa(i)+`"}`)))
		if i == 0 && rec.Code != http.StatusCreated {
			t.Fatalf("first request status = %d, want 201", rec.Code)
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second request status = %d, want 429", rec.Code)
		}
	}
}
