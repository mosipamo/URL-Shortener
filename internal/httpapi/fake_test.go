package httpapi

import "github.com/mosipamo/URL-Shortener.git/internal/store"

type fakeStore struct {
	saveFn func(string) (string, error)
	getFn  func(string) (store.Link, error)
}

func (f fakeStore) Save(u string) (string, error) {
	return f.saveFn(u)
}

func (f fakeStore) Get(c string) (store.Link, error) {
	return f.getFn(c)
}

var (
	_ Store = fakeStore{}
	_ Store = (*store.Memory)(nil)
)
