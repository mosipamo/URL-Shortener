package store

import "time"

type linkRow struct {
	Code      string    `gorm:"primaryKey;size:16"`
	URL       string    `gorm:"not null;size:2048;uniqueIndex"`
	CreatedAt time.Time `gorm:"not null"`
}

func (linkRow) TableName() string {
	return "links"
}
