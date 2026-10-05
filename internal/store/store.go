package store

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mosipamo/URL-Shortener/internal/model"
)

const (
	alphabet   = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	codeLength = 7
	maxRetries = 10
)

// Memory is a thread-safe in-memory Repository backed by two maps:
//   - links: code  → model.Link
//   - byURL: longURL → code  (idempotency index)
type Memory struct {
	mu    sync.Mutex
	links map[string]model.Link
	byURL map[string]string
	gen   func() string
	now   func() time.Time
}

// New returns an empty in-memory store ready for use.
func New() *Memory {
	return &Memory{
		links: make(map[string]model.Link),
		byURL: make(map[string]string),
		gen:   randomCode,
		now:   time.Now,
	}
}

// Save normalises rawURL and returns its code, creating one for new URLs.
// The entire lookup-generate-insert sequence is held under a single mutex so
// concurrent requests for the same URL always get the same code.
func (m *Memory) Save(rawURL string) (string, error) {
	longURL, err := Normalize(rawURL)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if code, ok := m.byURL[longURL]; ok {
		return code, nil
	}

	for range maxRetries {
		code := m.gen()
		if _, taken := m.links[code]; taken {
			continue
		}
		m.links[code] = model.Link{URL: longURL, CreatedAt: m.now().UTC()}
		m.byURL[longURL] = code
		return code, nil
	}
	return "", model.ErrCodeExhausted
}

// Get returns the Link for a known code, or an error wrapping model.ErrNotFound.
func (m *Memory) Get(code string) (model.Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	link, ok := m.links[code]
	if !ok {
		return model.Link{}, fmt.Errorf("get %q: %w", code, model.ErrNotFound)
	}
	return link, nil
}

// Normalize canonicalises rawURL so that the same logical URL always maps to
// the same string. It trims surrounding whitespace, lowercases the scheme and
// host, normalises an empty path to "/", and enforces the http/https scheme
// and a non-empty host. Path case, trailing slashes on non-empty paths, query
// parameters, and fragments are left unchanged. URLs longer than
// model.MaxURLLength bytes are rejected.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: empty", model.ErrInvalidURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", model.ErrInvalidURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", model.ErrInvalidURL)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("%w: missing host", model.ErrInvalidURL)
	}
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	out := u.String()
	if len(out) > model.MaxURLLength {
		return "", fmt.Errorf("%w: longer than %d bytes", model.ErrInvalidURL, model.MaxURLLength)
	}
	return out, nil
}

func randomCode() string {
	out := make([]byte, 0, codeLength)
	buf := make([]byte, codeLength*2)
	for len(out) < codeLength {
		if _, err := rand.Read(buf); err != nil {
			panic(err)
		}
		for _, b := range buf {
			if b >= 248 {
				continue
			}
			out = append(out, alphabet[int(b)%len(alphabet)])
			if len(out) == codeLength {
				break
			}
		}
	}
	return string(out)
}
