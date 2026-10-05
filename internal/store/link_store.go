package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mosipamo/URL-Shortener/internal/model"
)

// GormStore is a durable Repository for the links table backed by any
// database GORM supports (PostgreSQL in production, SQLite in tests).
type GormStore struct {
	db  *gorm.DB
	gen func() string    // swappable in tests to force collisions
	now func() time.Time // swappable in tests for deterministic created_at
}

// NewGorm runs AutoMigrate (creates or updates the links table) and returns a
// ready-to-use GormStore. Running this at startup makes links written by
// previous server instances immediately available after a restart.
func NewGorm(db *gorm.DB) (*GormStore, error) {
	if err := db.AutoMigrate(&linkRow{}); err != nil {
		return nil, fmt.Errorf("migrate links table: %w", err)
	}
	return &GormStore{db: db, gen: randomCode, now: time.Now}, nil
}

// Save normalises rawURL and returns its short code, inserting a new row when
// the URL has not been seen before.
//
// Idempotency is enforced by the database: the UNIQUE constraint on url means
// concurrent requests for the same URL — even across multiple server processes
// — always converge on one code. The INSERT is committed before Save returns,
// so the HTTP handler only responds 201 after the row is durable on disk.
func (s *GormStore) Save(rawURL string) (string, error) {
	longURL, err := Normalize(rawURL)
	if err != nil {
		return "", err
	}

	// Fast path: URL already shortened — avoid a pointless INSERT.
	if code, ok, err := s.codeForURL(longURL); err != nil {
		return "", fmt.Errorf("save link: %w", err)
	} else if ok {
		return code, nil
	}

	for i := 0; i < maxRetries; i++ {
		code, done, err := s.tryInsert(longURL)
		if err != nil {
			return "", fmt.Errorf("save link: %w", err)
		}
		if done {
			return code, nil
		}
		// done=false: our random code collided with a different URL's code.
		// Generate a fresh code and retry.
	}
	return "", model.ErrCodeExhausted
}

// tryInsert makes one attempt to INSERT longURL with a fresh random code using
// ON CONFLICT DO NOTHING.
//
//   - RowsAffected == 1 → inserted; return the new code (done=true).
//   - RowsAffected == 0 → a unique constraint fired:
//   - url constraint → another request won the race for this URL; look up
//     its code and return it (done=true).
//   - code constraint → our random code belongs to a different URL; return
//     done=false so the caller retries with a new code.
func (s *GormStore) tryInsert(longURL string) (code string, done bool, err error) {
	row := linkRow{
		Code: s.gen(),
		URL:  longURL,
		// Truncate to microseconds: PostgreSQL timestamptz precision, keeping
		// Memory and GormStore byte-identical in tests.
		CreatedAt: s.now().UTC().Truncate(time.Microsecond),
	}
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return "", false, res.Error
	}
	if res.RowsAffected == 1 {
		return row.Code, true, nil
	}
	// Zero rows affected — determine which constraint fired.
	existing, ok, err := s.codeForURL(longURL)
	if err != nil {
		return "", false, err
	}
	if ok {
		return existing, true, nil // lost the URL race → use winner's code
	}
	return "", false, nil // code collision → caller retries
}

// codeForURL returns the code already assigned to longURL, if any.
func (s *GormStore) codeForURL(longURL string) (code string, ok bool, err error) {
	var row linkRow
	err = s.db.Where("url = ?", longURL).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.Code, true, nil
}

// Get returns the model.Link for a known code, or an error wrapping
// model.ErrNotFound.
func (s *GormStore) Get(code string) (model.Link, error) {
	var row linkRow
	err := s.db.Where("code = ?", code).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Link{}, fmt.Errorf("get %q: %w", code, model.ErrNotFound)
	}
	if err != nil {
		return model.Link{}, fmt.Errorf("get link: %w", err)
	}
	return model.Link{URL: row.URL, CreatedAt: row.CreatedAt.UTC()}, nil
}

// Count returns the total number of stored links (used for the startup log line).
func (s *GormStore) Count() (int64, error) {
	var n int64
	if err := s.db.Model(&linkRow{}).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count links: %w", err)
	}
	return n, nil
}

// Close releases all database connections held by this store.
func (s *GormStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
