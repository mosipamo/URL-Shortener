// Package repository defines the persistence contract used by the HTTP layer.
// Concrete implementations live in internal/store.
package repository

import "github.com/mosipamo/URL-Shortener/internal/model"

// Repository is the interface that the HTTP handlers depend on.
//
// Save normalises rawURL and returns its short code. If the URL was already
// shortened the existing code is returned unchanged (idempotent). Errors are
// wrapped with %w so callers can use errors.Is against model.ErrInvalidURL,
// model.ErrNotFound, and model.ErrCodeExhausted.
//
// Get returns the Link for a known code, or an error wrapping model.ErrNotFound.
type Repository interface {
	Save(rawURL string) (code string, err error)
	Get(code string) (link model.Link, err error)
}
