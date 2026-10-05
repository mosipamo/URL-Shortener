package store

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound      = errors.New("code not found")
	ErrInvalidURL    = errors.New("invalid url")
	ErrCodeExhausted = errors.New("could not generate a unique code")
)

const (
	alphabet     = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	codeLength   = 7
	maxRetries   = 10
	MaxURLLength = 2048
)

type Link struct {
	URL       string
	CreatedAt time.Time
}

type Memory struct {
	mu    sync.Mutex
	links map[string]Link
	byURL map[string]string
	gen   func() string
	now   func() time.Time
}

func New() *Memory {
	return &Memory{
		links: make(map[string]Link),
		byURL: make(map[string]string),
		gen:   randomCode,
		now:   time.Now,
	}
}

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
		m.links[code] = Link{URL: longURL, CreatedAt: m.now().UTC()}
		m.byURL[longURL] = code
		return code, nil
	}
	return "", ErrCodeExhausted
}

func (m *Memory) Get(code string) (Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	link, ok := m.links[code]
	if !ok {
		return Link{}, fmt.Errorf("get %q: %w", code, ErrNotFound)
	}
	return link, nil
}

func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	out := u.String()
	if len(out) > MaxURLLength {
		return "", fmt.Errorf("%w: longer than %d bytes", ErrInvalidURL, MaxURLLength)
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
