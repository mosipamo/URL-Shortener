package model

import (
	"errors"
	"time"
)

const MaxURLLength = 2048

type Link struct {
	URL       string
	CreatedAt time.Time
}

var (
	ErrNotFound      = errors.New("code not found")
	ErrInvalidURL    = errors.New("invalid url")
	ErrCodeExhausted = errors.New("could not generate a unique code")
)
