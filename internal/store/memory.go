package store

import (
	"crypto/rand"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mosipamo/URL-Shortener/internal/model"
)

var blockedHosts = map[string]struct{}{
	"malware.example":  {},
	"phishing.example": {},
}

const (
	alphabet   = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	codeLength = 7
	maxRetries = 10
)

type Memory struct {
	mu    sync.Mutex
	links map[string]model.Link
	byURL map[string]string
	gen   func() string
	now   func() time.Time
}

func New() *Memory {
	return &Memory{
		links: make(map[string]model.Link),
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
		m.links[code] = model.Link{URL: longURL, CreatedAt: m.now().UTC()}
		m.byURL[longURL] = code
		return code, nil
	}
	return "", model.ErrCodeExhausted
}

func (m *Memory) Get(code string) (model.Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	link, ok := m.links[code]
	if !ok {
		return model.Link{}, fmt.Errorf("get %q: %w", code, model.ErrNotFound)
	}
	return link, nil
}

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
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("%w: missing host", model.ErrInvalidURL)
	}
	if err := checkHostPolicy(host); err != nil {
		return "", err
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

func checkHostPolicy(host string) error {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: host not allowed", model.ErrInvalidURL)
	}
	if _, blocked := blockedHosts[host]; blocked {
		return fmt.Errorf("%w: host not allowed", model.ErrInvalidURL)
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("%w: host not allowed", model.ErrInvalidURL)
		}
	}
	return nil
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
