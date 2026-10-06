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
	"github.com/mosipamo/URL-Shortener/internal/repository"
)

var (
	_ repository.Repository = (*Memory)(nil)
	_ repository.Repository = (*GormStore)(nil)
)

func runContract(t *testing.T, newStore func(t *testing.T) repository.Repository) {
	t.Helper()

	t.Run("save then get", func(t *testing.T) {
		s := newStore(t)
		code, err := s.Save("https://example.com/a")
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9]{6,8}$`).MatchString(code) {
			t.Fatalf("bad code %q", code)
		}
		l, err := s.Get(code)
		if err != nil || l.URL != "https://example.com/a" {
			t.Fatalf("Get = %+v, %v", l, err)
		}
		if _, off := l.CreatedAt.Zone(); off != 0 {
			t.Fatalf("CreatedAt must be UTC, got %v", l.CreatedAt)
		}
		if d := time.Since(l.CreatedAt); d < 0 || d > time.Minute {
			t.Fatalf("CreatedAt %v is not recent", l.CreatedAt)
		}
	})

	t.Run("same url same code", func(t *testing.T) {
		s := newStore(t)
		a, _ := s.Save("https://Example.com")
		b, _ := s.Save("  https://example.com/ ")
		if a == "" || a != b {
			t.Fatalf("codes differ: %q vs %q", a, b)
		}
		c, _ := s.Save("https://example.com/other")
		if c == a {
			t.Fatal("different URLs must not share a code")
		}
	})

	t.Run("created_at is stable", func(t *testing.T) {
		s := newStore(t)
		code, _ := s.Save("https://example.com/stable")
		first, _ := s.Get(code)
		time.Sleep(5 * time.Millisecond)
		again, _ := s.Save("https://example.com/stable")
		second, _ := s.Get(again)
		if code != again || !first.CreatedAt.Equal(second.CreatedAt) {
			t.Fatalf("repeat save changed created_at: %v → %v", first.CreatedAt, second.CreatedAt)
		}
	})

	t.Run("unknown code", func(t *testing.T) {
		if _, err := newStore(t).Get("nope123"); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid urls", func(t *testing.T) {
		s := newStore(t)
		long := "https://example.com/" + strings.Repeat("a", model.MaxURLLength)
		for _, in := range []string{"", "   ", "ftp://x.com", "example.com", "javascript:alert(1)", "https://", long} {
			if _, err := s.Save(in); !errors.Is(err, model.ErrInvalidURL) {
				t.Errorf("Save(%.30q) err = %v, want ErrInvalidURL", in, err)
			}
		}
	})

	t.Run("longest allowed url", func(t *testing.T) {
		s := newStore(t)
		prefix := "https://example.com/"
		u := prefix + strings.Repeat("a", model.MaxURLLength-len(prefix))
		code, err := s.Save(u)
		if err != nil {
			t.Fatalf("URL of exactly %d bytes must be accepted: %v", model.MaxURLLength, err)
		}
		if l, err := s.Get(code); err != nil || l.URL != u {
			t.Fatalf("round trip failed: %v", err)
		}
	})

	t.Run("concurrent duplicate saves", func(t *testing.T) {
		s := newStore(t)
		const n = 50
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
	})

	t.Run("concurrent distinct saves", func(t *testing.T) {
		s := newStore(t)
		const n = 50
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
	})
}

func TestContractMemory(t *testing.T) {
	runContract(t, func(t *testing.T) repository.Repository { return New() })
}
