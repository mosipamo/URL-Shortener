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
)

// openSQLite opens (or reopens) a SQLite file. SQLite is only the test
// database: the store code under test is the same GORM code used with
// PostgreSQL. One connection avoids "database is locked" under concurrency.
func openSQLite(t *testing.T, path string) *GormStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	s, err := NewGorm(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() }) // closing twice is harmless
	return s
}

func newTestGorm(t *testing.T) *GormStore {
	return openSQLite(t, filepath.Join(t.TempDir(), "links.db"))
}

func TestContractGorm(t *testing.T) {
	runContract(t, func(t *testing.T) linkStore { return newTestGorm(t) })
}

// The restart simulation: a brand new store instance on the same database
// file sees what the previous instance wrote.
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
		t.Fatalf("after restart %+v, before %+v", after, before)
	}
	again, err := b.Save(u) // idempotency survives the restart
	if err != nil || again != code {
		t.Fatalf("same URL after restart: code %q (err %v), want %q", again, err, code)
	}
	if n, err := b.Count(); err != nil || n != 1 {
		t.Fatalf("Count = %d, %v; want 1", n, err)
	}
}

func TestGormCreatedAtTruncatedToMicroseconds(t *testing.T) {
	s := newTestGorm(t)
	s.now = func() time.Time { return time.Date(2026, 1, 15, 12, 0, 0, 123456789, time.FixedZone("x", 3600)) }

	code, _ := s.Save("https://example.com/t")
	l, err := s.Get(code)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 15, 11, 0, 0, 123456000, time.UTC) // UTC, ns dropped
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
	c2, err := s.Save("https://two.example") // "aaaaaaa" is taken twice, then "bbbbbbb"
	if err != nil {
		t.Fatal(err)
	}
	if c1 != "aaaaaaa" || c2 != "bbbbbbb" {
		t.Fatalf("codes = %q, %q", c1, c2)
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
	if _, err := s.Save("https://one.example"); err != nil { // existing URL still works
		t.Fatalf("existing URL should still resolve: %v", err)
	}
}

// Simulates losing the race: the URL already exists but our fast-path lookup
// did not see it. tryInsert must hit the unique constraint on url and return
// the existing code instead of creating a second one.
func TestGormTryInsertLosesRaceOnURL(t *testing.T) {
	s := newTestGorm(t)
	const u = "https://example.com/race"
	first, done, err := s.tryInsert(u)
	if err != nil || !done {
		t.Fatalf("first insert: %q %v %v", first, done, err)
	}
	second, done, err := s.tryInsert(u) // new random code, same URL
	if err != nil || !done || second != first {
		t.Fatalf("second insert: code %q done %v err %v; want code %q", second, done, err, first)
	}
	if n, _ := s.Count(); n != 1 {
		t.Fatalf("Count = %d, want exactly 1 row", n)
	}
}

// Database failures must NOT look like "not found" or "invalid URL": the
// handler maps those to 404/400 and everything else to 500.
func TestGormDatabaseErrors(t *testing.T) {
	s := newTestGorm(t)
	code, _ := s.Save("https://example.com/x")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Get(code); err == nil || errors.Is(err, model.ErrNotFound) {
		t.Fatalf("Get on closed DB: err = %v, want a non-NotFound error", err)
	}
	if _, err := s.Save("https://example.com/y"); err == nil ||
		errors.Is(err, model.ErrInvalidURL) || errors.Is(err, model.ErrCodeExhausted) {
		t.Fatalf("Save on closed DB: err = %v, want a database error", err)
	}
	if _, err := s.Count(); err == nil {
		t.Fatal("Count on closed DB should fail")
	}
}

// An unreachable database must fail fast with an error that does not contain
// the password from the DSN.
func TestOpenPostgresUnreachableDoesNotLeakPassword(t *testing.T) {
	const dsn = "postgres://shortener:supersecret@127.0.0.1:1/shortener?sslmode=disable&connect_timeout=1"
	db, err := OpenPostgres(dsn)
	if err == nil {
		t.Fatalf("expected an error, got db %v", db)
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("password leaked into error: %v", err)
	}
}

func TestNewGormMigrationError(t *testing.T) {
	s := newTestGorm(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewGorm(s.db); err == nil {
		t.Fatal("migrating on a closed database should fail")
	}
}
