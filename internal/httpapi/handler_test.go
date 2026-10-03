package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"url-shortener/internal/store"
)

const base = "http://localhost:8080"

func newHandler() http.Handler { return New(store.NewMemory(), base) }

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
	h := New(store.NewMemory(), base+"/")
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
		t.Fatal("different URLs must get different codes")
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
