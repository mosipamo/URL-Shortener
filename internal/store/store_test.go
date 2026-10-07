package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mosipamo/URL-Shortener/internal/model"
)

func TestSaveAndGet(t *testing.T) {
	s := New()
	code, err := s.Save("https://example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9]{6,8}$`).MatchString(code) {
		t.Fatalf("bad code %q", code)
	}
	got, err := s.Get(code)
	if err != nil || got.URL != "https://example.com/a" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

func TestSaveInvalid(t *testing.T) {
	tests := []struct{ name, in string }{
		{"empty", ""},
		{"spaces", "   "},
		{"ftp scheme", "ftp://example.com"},
		{"no scheme", "example.com"},
		{"javascript", "javascript:alert(1)"},
		{"no host", "https://"},
		{"port only", "http://:80"},
		{"unparseable", "http://[::1"},
		{"too long", "https://example.com/" + strings.Repeat("a", model.MaxURLLength)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New().Save(tc.in); !errors.Is(err, model.ErrInvalidURL) {
				t.Fatalf("err = %v, want ErrInvalidURL", err)
			}
		})
	}
}

func TestGetNotFound(t *testing.T) {
	if _, err := New().Get("nope"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://example.com", "https://example.com/"},
		{"https://example.com/", "https://example.com/"},
		{"  https://example.com/x  ", "https://example.com/x"},
		{"HTTPS://EXAMPLE.COM/x", "https://example.com/x"},
		{"https://example.com?a=1", "https://example.com/?a=1"},
		{"https://example.com/Path", "https://example.com/Path"},
		{"https://example.com/x/", "https://example.com/x/"},
		{"https://example.com/x#frag", "https://example.com/x#frag"},
	}
	for _, tc := range tests {
		got, err := Normalize(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestSaveIdempotent(t *testing.T) {
	s := New()
	c1, _ := s.Save("https://Example.com")
	c2, _ := s.Save("https://example.com/")
	if c1 != c2 {
		t.Fatalf("same URL got different codes: %q vs %q", c1, c2)
	}
	c3, _ := s.Save("https://example.com/other")
	if c3 == c1 {
		t.Fatal("different URLs must not share a code")
	}
}

func TestCollisionRetry(t *testing.T) {
	s := New()
	seq := []string{"aaaaaaa", "aaaaaaa", "aaaaaaa", "bbbbbbb"}
	s.gen = func() string { c := seq[0]; seq = seq[1:]; return c }

	c1, _ := s.Save("https://one.example")
	c2, err := s.Save("https://two.example")
	if err != nil {
		t.Fatal(err)
	}
	if c1 != "aaaaaaa" || c2 != "bbbbbbb" {
		t.Fatalf("codes = %q, %q", c1, c2)
	}
}

func TestCollisionExhausted(t *testing.T) {
	s := New()
	s.gen = func() string { return "same123" }
	if _, err := s.Save("https://one.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("https://two.example"); !errors.Is(err, model.ErrCodeExhausted) {
		t.Fatalf("err = %v, want ErrCodeExhausted", err)
	}
	if _, err := s.Save("https://one.example"); err != nil {
		t.Fatalf("existing URL should still resolve: %v", err)
	}
}

func TestRandomCodeAlphabetAndLength(t *testing.T) {
	re := regexp.MustCompile(`^[a-zA-Z0-9]{7}$`)
	for range 1000 {
		if c := randomCode(); !re.MatchString(c) {
			t.Fatalf("bad code %q", c)
		}
	}
}

func TestConcurrentDuplicateSave(t *testing.T) {
	s := New()
	const n = 100
	codes := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := s.Save("https://example.com/same")
			if err != nil {
				t.Errorf("save: %v", err)
			}
			codes[i] = c
		}(i)
	}
	wg.Wait()

	for i, c := range codes {
		if c != codes[0] {
			t.Fatalf("goroutine %d got %q, goroutine 0 got %q", i, c, codes[0])
		}
	}
	if len(s.links) != 1 || len(s.byURL) != 1 {
		t.Fatalf("links=%d byURL=%d, want 1/1", len(s.links), len(s.byURL))
	}
}

func TestConcurrentDistinctSave(t *testing.T) {
	s := New()
	const n = 200
	codes := make(chan string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := s.Save(fmt.Sprintf("https://example.com/%d", i))
			if err != nil {
				t.Errorf("save: %v", err)
			}
			codes <- c
		}(i)
	}
	wg.Wait()
	close(codes)
	seen := map[string]bool{}
	for c := range codes {
		if seen[c] {
			t.Fatalf("duplicate code %q for distinct URLs", c)
		}
		seen[c] = true
	}
}

func TestSaveIdempotentKeepsCreatedAt(t *testing.T) {
	s := New()
	tick := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		t := tick
		tick = tick.Add(time.Hour)
		return t
	}

	c1, _ := s.Save("https://example.com/x")
	c2, _ := s.Save("https://example.com/x")
	if c1 != c2 {
		t.Fatalf("codes differ: %q vs %q", c1, c2)
	}
	l, err := s.Get(c1)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if !l.CreatedAt.Equal(want) {
		t.Fatalf("CreatedAt = %v, want %v", l.CreatedAt, want)
	}
}
