package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mosipamo/URL-Shortener/internal/model"
)

type GormStore struct {
	db  *gorm.DB
	gen func() string
	now func() time.Time
}

func NewGorm(db *gorm.DB) (*GormStore, error) {
	if err := db.AutoMigrate(&linkRow{}); err != nil {
		return nil, fmt.Errorf("migrate links table: %w", err)
	}
	return &GormStore{db: db, gen: randomCode, now: time.Now}, nil
}

func (s *GormStore) Save(rawURL string) (string, error) {
	longURL, err := Normalize(rawURL)
	if err != nil {
		return "", err
	}

	if code, ok, err := s.codeForURL(longURL); err != nil {
		return "", fmt.Errorf("save link: %w", err)
	} else if ok {
		return code, nil
	}

	for range maxRetries {
		code, done, err := s.tryInsert(longURL)
		if err != nil {
			return "", fmt.Errorf("save link: %w", err)
		}
		if done {
			return code, nil
		}
	}
	return "", model.ErrCodeExhausted
}

func (s *GormStore) tryInsert(longURL string) (code string, done bool, err error) {
	row := linkRow{
		Code:      s.gen(),
		URL:       longURL,
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
		return existing, true, nil
	}
	return "", false, nil
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

func (s *GormStore) Count() (int64, error) {
	var n int64
	if err := s.db.Model(&linkRow{}).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count links: %w", err)
	}
	return n, nil
}

func (s *GormStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
