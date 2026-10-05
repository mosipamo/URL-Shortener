package store

import "time"

// linkRow is the GORM model for the "links" table.
//
// GORM derives column names from field names (CreatedAt → created_at).
// TableName overrides the default plural form ("link_rows").
//
// Database constraints enforce the same invariants the mutex protects in Memory:
//   - code  PRIMARY KEY  → two links can never share a code
//   - url   UNIQUE index → one URL can never receive two codes
//
// Adding a new table: define its row struct here (e.g. userRow) and include
// it in the AutoMigrate call inside its corresponding store constructor.
type linkRow struct {
	Code      string    `gorm:"primaryKey;size:16"`
	URL       string    `gorm:"not null;size:2048;uniqueIndex"`
	CreatedAt time.Time `gorm:"not null"`
}

func (linkRow) TableName() string { return "links" }
