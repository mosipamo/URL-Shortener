// Package model contains the core domain types shared across all layers.
package model

import (
	"errors"
	"time"
)

// MaxURLLength is the maximum byte length of a normalised long URL that the
// service will accept. Kept here so that both validation logic (store) and
// tests can reference the same constant.
const MaxURLLength = 2048

// Link is the domain object stored for every shortened URL.
type Link struct {
	URL       string
	CreatedAt time.Time
}

// Sentinel errors returned by Repository implementations.
// Callers use errors.Is to distinguish them; implementations wrap them with
// %w so that context (e.g. the code value) is preserved in the error message.
var (
	ErrNotFound      = errors.New("code not found")
	ErrInvalidURL    = errors.New("invalid url")
	ErrCodeExhausted = errors.New("could not generate a unique code")
)
