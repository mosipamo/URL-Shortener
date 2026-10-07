package repository

import "github.com/mosipamo/URL-Shortener/internal/model"

type Repository interface {
	Save(rawURL string) (code string, err error)
	Get(code string) (link model.Link, err error)
}
