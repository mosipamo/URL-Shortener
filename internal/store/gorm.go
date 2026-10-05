package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/mosipamo/URL-Shortener/internal/model"
)

// linkRow is the GORM database model. Column names are derived from field
// names (CreatedAt → created_at); TableName sets the table name.
//
// Database constraints mirror the mutex invariants of Memory:
//   - code is the PRIMARY KEY → two links can never share a code.
//   - url has a UNIQUE index  → one URL can never get two codes.
type linkRow struct {
	Code      string    `gorm:"primaryKey;size:16"`
	URL       string    `gorm:"not null;size:2048;uniqueIndex"`
	CreatedAt time.Time `gorm:"not null"`
}

func (linkRow) TableName() string { return "links" }

// GormStore is a durable Repository backed by any database GORM supports
// (PostgreSQL in production, SQLite in tests).
type GormStore struct {
	db  *gorm.DB
	gen func() string    // swappable in tests to force collisions
	now func() time.Time // swappable in tests for deterministic created_at
}

// NewGorm creates the links table if needed (AutoMigrate) and returns a store.
// Doing this at startup is what makes data from earlier runs available again.
func NewGorm(db *gorm.DB) (*GormStore, error) {
	if err := db.AutoMigrate(&linkRow{}); err != nil {
		return nil, fmt.Errorf("migrate links table: %w", err)
	}
	return &GormStore{db: db, gen: randomCode, now: time.Now}, nil
}

// OpenPostgres connects to PostgreSQL, configures the connection pool and
// fails fast if the database is unreachable. The DSN contains the password:
// it is never logged here, and errors from the driver do not include it.
func OpenPostgres(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// Translate driver errors into gorm.ErrDuplicatedKey etc. on every dialect.
		TranslateError: true,
		Logger: logger.New(log.Default(), logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true, // "not found" is a normal outcome here
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

// Save normalises rawURL and returns its code, creating it if the URL is new.
//
// The same URL always maps to the same code, across restarts and even when
// many requests (or many servers) shorten it at the same moment, because the
// database enforces uniqueness. An INSERT is a single atomic statement that
// is committed before Save returns, so the HTTP handler only answers 201 after
// the row is durable.
func (s *GormStore) Save(rawURL string) (string, error) {
	longURL, err := Normalize(rawURL)
	if err != nil {
		return "", err
	}

	// Fast path: URL already shortened (avoids a pointless INSERT).
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
		// Not done: the random code collided with a different URL. Retry.
	}
	return "", model.ErrCodeExhausted
}

// tryInsert makes one attempt to create the link with a fresh random code.
//
//	INSERT ... ON CONFLICT DO NOTHING reports RowsAffected:
//	  1 -> inserted, code is ours.
//	  0 -> a unique constraint fired. Either another request just inserted this
//	       same URL (then we return ITS code), or our random code is already
//	       taken by a different URL (then done=false and the caller retries).
func (s *GormStore) tryInsert(longURL string) (code string, done bool, err error) {
	row := linkRow{
		Code: s.gen(),
		URL:  longURL,
		// UTC and microseconds: PostgreSQL timestamptz keeps microsecond
		// precision, so truncating makes memory and database agree exactly.
		CreatedAt: s.now().UTC().Truncate(time.Microsecond),
	}
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return "", false, res.Error
	}
	if res.RowsAffected == 1 {
		return row.Code, true, nil
	}
	existing, ok, err := s.codeForURL(longURL)
	if err != nil {
		return "", false, err
	}
	if ok {
		return existing, true, nil // lost the race for this URL: use the winner's code
	}
	return "", false, nil // code collision
}

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

// Get returns the model.Link for code, or an error wrapping model.ErrNotFound.
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

// Count returns how many links are stored (used for the startup log line).
func (s *GormStore) Count() (int64, error) {
	var n int64
	if err := s.db.Model(&linkRow{}).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count links: %w", err)
	}
	return n, nil
}

// Close releases the database connections.
func (s *GormStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
