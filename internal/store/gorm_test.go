package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/mosipamo/URL-Shortener/internal/model"
	"github.com/mosipamo/URL-Shortener/internal/repository"
)

// openSQLite opens (or reopens) a SQLite file and returns a ready GormStore.
// SQLite is used as the test database only: the production GORM code is
// identical regardless of dialect. One connection (SetMaxOpenConns(1)) avoids
// "database is locked" errors under concurrent access.
//
// Accepts testing.TB so the helper can be called from both *testing.T tests
// and *testing.B benchmarks.
func openSQLite(tb testing.TB, path string) *GormStore {
	tb.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Discard,
	})
	if err != nil {
		tb.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	s, err := NewGorm(db)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = s.Close() })
	return s
}

// newTestGorm returns a GormStore backed by a fresh SQLite file in t.TempDir().
func newTestGorm(t *testing.T) *GormStore {
	return openSQLite(t, filepath.Join(t.TempDir(), "links.db"))
}

// newBenchGorm returns a GormStore backed by a fresh SQLite file in b.TempDir().
func newBenchGorm(b *testing.B) *GormStore {
	return openSQLite(b, filepath.Join(b.TempDir(), "links.db"))
}

// TestContractGorm runs the shared contract suite against the GORM+SQLite store,
// proving it is interchangeable with the in-memory store.
func TestContractGorm(t *testing.T) {
	runContract(t, func(t *testing.T) repository.Repository { return newTestGorm(t) })
}

// TestGormRestart simulates a server restart: a brand-new GormStore instance
// opened on the same SQLite file must see everything the previous instance wrote.
func TestGormRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
	const u = "https://go.dev/doc/"

	a := openSQLite(t, path)
	code, err := a.Save(u)
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.Get(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil { // "server stops"
		t.Fatal(err)
	}

	b := openSQLite(t, path) // "server starts again"
	after, err := b.Get(code)
	if err != nil {
		t.Fatalf("link lost after restart: %v", err)
	}
	if after.URL != u || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("after restart: got %+v, want %+v", after, before)
	}
	again, err := b.Save(u) // idempotency must survive the restart
	if err != nil || again != code {
		t.Fatalf("same URL after restart: code %q (err %v), want %q", again, err, code)
	}
	if n, err := b.Count(); err != nil || n != 1 {
		t.Fatalf("Count = %d, %v; want 1", n, err)
	}
}

func TestGormCreatedAtTruncatedToMicroseconds(t *testing.T) {
	s := newTestGorm(t)
	s.now = func() time.Time {
		return time.Date(2026, 1, 15, 12, 0, 0, 123456789, time.FixedZone("x", 3600))
	}

	code, _ := s.Save("https://example.com/t")
	l, err := s.Get(code)
	if err != nil {
		t.Fatal(err)
	}
	// Nanoseconds are dropped (SQLite stores µs); timezone converted to UTC.
	want := time.Date(2026, 1, 15, 11, 0, 0, 123456000, time.UTC)
	if !l.CreatedAt.Equal(want) {
		t.Fatalf("CreatedAt = %v, want %v", l.CreatedAt, want)
	}
}

func TestGormCollisionRetry(t *testing.T) {
	s := newTestGorm(t)
	seq := []string{"aaaaaaa", "aaaaaaa", "aaaaaaa", "bbbbbbb"}
	s.gen = func() string { c := seq[0]; seq = seq[1:]; return c }

	c1, err := s.Save("https://one.example")
	if err != nil {
		t.Fatal(err)
	}
	// "aaaaaaa" collides twice before "bbbbbbb" succeeds.
	c2, err := s.Save("https://two.example")
	if err != nil {
		t.Fatal(err)
	}
	if c1 != "aaaaaaa" || c2 != "bbbbbbb" {
		t.Fatalf("codes = %q, %q; want aaaaaaa, bbbbbbb", c1, c2)
	}
}

func TestGormCollisionExhausted(t *testing.T) {
	s := newTestGorm(t)
	s.gen = func() string { return "same123" }
	if _, err := s.Save("https://one.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("https://two.example"); !errors.Is(err, model.ErrCodeExhausted) {
		t.Fatalf("err = %v, want ErrCodeExhausted", err)
	}
	// The original URL must still be resolvable even after exhaustion.
	if _, err := s.Save("https://one.example"); err != nil {
		t.Fatalf("existing URL should still resolve: %v", err)
	}
}

// TestGormTryInsertLosesRaceOnURL simulates the window between the fast-path
// SELECT and the INSERT: another goroutine already inserted the same URL.
// tryInsert must detect the url unique constraint and return the winner's code.
func TestGormTryInsertLosesRaceOnURL(t *testing.T) {
	s := newTestGorm(t)
	const u = "https://example.com/race"

	first, done, err := s.tryInsert(u)
	if err != nil || !done {
		t.Fatalf("first insert: code=%q done=%v err=%v", first, done, err)
	}
	// Second call: different random code, same URL → must return first's code.
	second, done, err := s.tryInsert(u)
	if err != nil || !done || second != first {
		t.Fatalf("second insert: code=%q done=%v err=%v; want code=%q", second, done, err, first)
	}
	if n, _ := s.Count(); n != 1 {
		t.Fatalf("Count = %d; want exactly 1 row", n)
	}
}

// TestGormDatabaseErrors verifies that errors from a closed database are NOT
// misidentified as ErrNotFound or ErrInvalidURL (which would produce wrong
// HTTP status codes in the handler).
func TestGormDatabaseErrors(t *testing.T) {
	s := newTestGorm(t)
	code, _ := s.Save("https://example.com/x")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Get(code); err == nil || errors.Is(err, model.ErrNotFound) {
		t.Fatalf("Get on closed DB: err = %v; want a non-NotFound error", err)
	}
	if _, err := s.Save("https://example.com/y"); err == nil ||
		errors.Is(err, model.ErrInvalidURL) || errors.Is(err, model.ErrCodeExhausted) {
		t.Fatalf("Save on closed DB: err = %v; want a database error", err)
	}
	if _, err := s.Count(); err == nil {
		t.Fatal("Count on closed DB should fail")
	}
}

// TestOpenPostgresUnreachableDoesNotLeakPassword ensures that a failed
// connection attempt does not echo the DSN password back in the error message.
func TestOpenPostgresUnreachableDoesNotLeakPassword(t *testing.T) {
	const dsn = "postgres://shortener:supersecret@127.0.0.1:1/shortener?sslmode=disable&connect_timeout=1"
	db, err := OpenPostgres(dsn)
	if err == nil {
		t.Fatalf("expected a connection error, got db %v", db)
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("password leaked into error: %v", err)
	}
}

// TestNewGormMigrationError verifies that NewGorm surfaces errors from
// AutoMigrate rather than silently returning a broken store.
func TestNewGormMigrationError(t *testing.T) {
	s := newTestGorm(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewGorm(s.db); err == nil {
		t.Fatal("migrating on a closed database should fail")
	}
}
