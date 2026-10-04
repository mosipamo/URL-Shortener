package httpapi

import (
	"net/http"
	"testing"

	"github.com/mosipamo/URL-Shortener.git/internal/store"
)

func BenchmarkShorten(b *testing.B) {
	h := newHandler()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if rec := post(h, `{"url":"https://example.com/x"}`); rec.Code != http.StatusCreated {
				b.Fatalf("status %d", rec.Code)
			}
		}
	})
}

func BenchmarkRedirect(b *testing.B) {
	h := newHandler()
	code := shorten(b, h, "https://example.com/x").Code
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if rec := get(h, "/"+code); rec.Code != http.StatusFound {
				b.Fatalf("status %d", rec.Code)
			}
		}
	})
}

func BenchmarkShortenHandlerOnly(b *testing.B) {
	h := New(fakeStore{
		saveFn: func(string) (string, error) { return "abc1234", nil },
	}, base)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			post(h, `{"url":"https://example.com/x"}`)
		}
	})
}

func BenchmarkRedirectHandlerOnly(b *testing.B) {
	h := New(fakeStore{
		getFn: func(string) (store.Link, error) {
			return store.Link{URL: "https://example.org/target"}, nil
		},
	}, base)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			get(h, "/whatever")
		}
	})
}
