package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mosipamo/URL-Shortener/internal/model"
	"github.com/mosipamo/URL-Shortener/internal/store"
)

const base = "http://localhost:8080"

// newHandler builds a handler backed by a real in-memory store, with logging
// discarded so test output stays clean.
func newHandler() http.Handler {
	return New(store.New(), base, log.New(io.Discard, "", 0))
}

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func shorten(t testing.TB, h http.Handler, url string) shortenResponse {
	t.Helper()
	rec := post(h, fmt.Sprintf(`{"url":%q}`, url))
	if rec.Code != http.StatusCreated {
		t.Fatalf("shorten status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp shortenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestShortenThenRedirect(t *testing.T) {
	h := newHandler()
	const long = "https://example.com/path?q=1"

	resp := shorten(t, h, long)
	if resp.ShortURL != base+"/"+resp.Code {
		t.Fatalf("short_url = %q, code = %q", resp.ShortURL, resp.Code)
	}
	if n := len(resp.Code); n < 6 || n > 8 {
		t.Fatalf("code length = %d", n)
	}

	rec := get(h, "/"+resp.Code)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != long {
		t.Fatalf("Location = %q, want %q", loc, long)
	}
}

func TestShortenBadRequests(t *testing.T) {
	tests := []struct{ name, body string }{
		{"empty url", `{"url":""}`},
		{"missing url", `{}`},
		{"bad scheme", `{"url":"ftp://example.com"}`},
		{"no scheme", `{"url":"example.com"}`},
		{"not json", `not json`},
		{"empty body", ``},
		{"wrong type", `{"url":123}`},
	}
	h := newHandler()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rec := post(h, tc.body); rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
			}
		})
	}
}

func TestRedirectUnknownCode(t *testing.T) {
	h := newHandler()
	for _, path := range []string{"/doesnotexist", "/favicon.ico", "/abc123"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	h := New(store.New(), base+"/", log.New(io.Discard, "", 0))
	resp := shorten(t, h, "https://example.com")
	if resp.ShortURL != base+"/"+resp.Code {
		t.Fatalf("short_url = %q", resp.ShortURL)
	}
}

func TestConcurrentShortenAndRedirect(t *testing.T) {
	h := newHandler()
	shared := shorten(t, h, "https://example.com/shared")

	const workers = 50
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			long := fmt.Sprintf("https://example.com/item/%d", i)

			resp := shorten(t, h, long)
			rec := get(h, "/"+resp.Code)
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != long {
				t.Errorf("worker %d: status %d, Location %q", i, rec.Code, rec.Header().Get("Location"))
			}

			rec = get(h, "/"+shared.Code)
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://example.com/shared" {
				t.Errorf("worker %d: shared redirect wrong: %d", i, rec.Code)
			}
		}(i)
	}
	wg.Wait()
}

func TestShortenIdempotent(t *testing.T) {
	h := newHandler()
	a := shorten(t, h, "https://go.dev/doc/")
	b := shorten(t, h, "https://go.dev/doc/")
	if a != b {
		t.Fatalf("same URL gave %+v then %+v", a, b)
	}
	c := shorten(t, h, "https://go.dev/blog/")
	if c.Code == a.Code {
		t.Fatal("different URLs must not share a code")
	}
}

func TestShortenIdempotentAfterNormalization(t *testing.T) {
	h := newHandler()
	a := shorten(t, h, "https://Go.dev")
	b := shorten(t, h, "  https://go.dev/ ")
	if a.Code != b.Code {
		t.Fatalf("codes differ: %q vs %q", a.Code, b.Code)
	}
}

func TestConcurrentDuplicateShortenHTTP(t *testing.T) {
	h := newHandler()
	const n = 50
	codes := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = shorten(t, h, "https://example.com/dup").Code
			if rec := get(h, "/"+codes[i]); rec.Code != http.StatusFound {
				t.Errorf("redirect status = %d", rec.Code)
			}
		}(i)
	}
	wg.Wait()
	for _, c := range codes {
		if c != codes[0] {
			t.Fatalf("codes differ: %q vs %q", c, codes[0])
		}
	}
}

func linkInfo(h http.Handler, code string) *httptest.ResponseRecorder {
	return get(h, "/api/v1/links/"+code)
}

func TestLinkInfoOK(t *testing.T) {
	created := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	h := New(fakeStore{
		getFn: func(code string) (model.Link, error) {
			return model.Link{URL: "https://go.dev/doc/", CreatedAt: created}, nil
		},
	}, base, log.New(io.Discard, "", 0))

	rec := linkInfo(h, "abc1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"url": "https://go.dev/doc/", "created_at": "2026-01-15T12:00:00Z"}
	if len(got) != 2 || got["url"] != want["url"] || got["created_at"] != want["created_at"] {
		t.Fatalf("body = %v, want %v", got, want)
	}
}

func TestLinkInfoNotFound(t *testing.T) {
	rec := linkInfo(newHandler(), "doesnotexist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestLinkInfoViaRealStoreAndIdempotency(t *testing.T) {
	h := newHandler()
	a := shorten(t, h, "https://go.dev/doc/")

	rec := linkInfo(h, a.Code)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var first linkResponse
	_ = json.NewDecoder(rec.Body).Decode(&first)
	if first.URL != "https://go.dev/doc/" {
		t.Fatalf("url = %q", first.URL)
	}
	if _, err := time.Parse(time.RFC3339, first.CreatedAt); err != nil {
		t.Fatalf("created_at %q is not RFC3339: %v", first.CreatedAt, err)
	}
	b := shorten(t, h, "https://go.dev/doc/")
	if a != b {
		t.Fatalf("idempotency broken: %+v vs %+v", a, b)
	}
	var second linkResponse
	_ = json.NewDecoder(linkInfo(h, b.Code).Body).Decode(&second)
	if first != second {
		t.Fatalf("metadata changed after repeat shorten: %+v vs %+v", first, second)
	}
}

func TestErrorMapping(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("layer2: %w", fmt.Errorf("layer1: %w", err)) }

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"invalid url", model.ErrInvalidURL, http.StatusBadRequest},
		{"wrapped invalid url", wrap(model.ErrInvalidURL), http.StatusBadRequest},
		{"not found", model.ErrNotFound, http.StatusNotFound},
		{"wrapped not found", wrap(model.ErrNotFound), http.StatusNotFound},
		{"code exhausted", model.ErrCodeExhausted, http.StatusInternalServerError},
		{"unknown error", errors.New("db is down"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := New(fakeStore{
				saveFn: func(string) (string, error) { return "", tc.err },
				getFn:  func(string) (model.Link, error) { return model.Link{}, tc.err },
			}, base, log.New(io.Discard, "", 0))

			routes := []*httptest.ResponseRecorder{
				post(h, `{"url":"https://example.com"}`),
				get(h, "/abc1234"),
				linkInfo(h, "abc1234"),
			}
			for i, rec := range routes {
				if rec.Code != tc.wantStatus {
					t.Errorf("route %d: status = %d, want %d", i, rec.Code, tc.wantStatus)
				}
				var body map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body["error"] == "" {
					t.Errorf("route %d: body should be {\"error\":...}, got %q (%v)", i, rec.Body, err)
				}
				if tc.wantStatus == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "db is down") {
					t.Errorf("route %d: internal error text leaked to client", i)
				}
			}
		})
	}
}

func TestRedirectUsesLinkURL(t *testing.T) {
	h := New(fakeStore{
		getFn: func(string) (model.Link, error) {
			return model.Link{URL: "https://example.org/target"}, nil
		},
	}, base, log.New(io.Discard, "", 0))
	rec := get(h, "/whatever")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://example.org/target" {
		t.Fatalf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestShortenURLTooLong(t *testing.T) {
	h := newHandler()
	long := "https://example.com/" + strings.Repeat("a", model.MaxURLLength)
	if rec := post(h, fmt.Sprintf(`{"url":%q}`, long)); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
