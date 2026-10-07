package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/mosipamo/URL-Shortener/internal/model"
)

type MemoryRW struct {
	mu    sync.RWMutex
	links map[string]model.Link
	byURL map[string]string
	gen   func() string
	now   func() time.Time
}

func NewRW() *MemoryRW {
	return &MemoryRW{
		links: make(map[string]model.Link),
		byURL: make(map[string]string),
		gen:   randomCode,
		now:   time.Now,
	}
}

func (m *MemoryRW) Save(rawURL string) (string, error) {
	longURL, err := Normalize(rawURL)
	if err != nil {
		return "", err
	}

	m.mu.RLock()
	code, ok := m.byURL[longURL]
	m.mu.RUnlock()
	if ok {
		return code, nil
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

func (m *MemoryRW) Get(code string) (model.Link, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	link, ok := m.links[code]
	if !ok {
		return model.Link{}, fmt.Errorf("get %q: %w", code, model.ErrNotFound)
	}
	return link, nil
}
