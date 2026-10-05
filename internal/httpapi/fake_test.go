package httpapi

import (
	"github.com/mosipamo/URL-Shortener/internal/model"
	"github.com/mosipamo/URL-Shortener/internal/repository"
	"github.com/mosipamo/URL-Shortener/internal/store"
)

// fakeStore is an in-test stub that satisfies repository.Repository.
// Individual test cases inject saveFn / getFn to force any result, including
// errors that the real store would never produce in unit tests.
type fakeStore struct {
	saveFn func(string) (string, error)
	getFn  func(string) (model.Link, error)
}

func (f fakeStore) Save(u string) (string, error) {
	return f.saveFn(u)
}

func (f fakeStore) Get(c string) (model.Link, error) {
	return f.getFn(c)
}

// Compile-time assertions: every concrete store must satisfy repository.Repository.
var (
	_ repository.Repository = fakeStore{}
	_ repository.Repository = (*store.Memory)(nil)
	_ repository.Repository = (*store.GormStore)(nil)
)
