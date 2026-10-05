package store

import (
	"os"
	"sync"
	"testing"
)

// Runs the SAME contract against a real PostgreSQL server. Skipped unless
// TEST_DATABASE_URL is set, e.g.
//
//	TEST_DATABASE_URL='postgres://shortener:secret@localhost:5432/shortener_test?sslmode=disable' \
//	  go test -race -run Postgres ./internal/store
//
// WARNING: the test TRUNCATEs the links table. Use a throwaway database.
func TestContractPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	db, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}

	runContract(t, func(t *testing.T) linkStore {
		s, err := NewGorm(db)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("TRUNCATE TABLE links").Error; err != nil {
			t.Fatal(err)
		}
		return s
	})

	t.Run("restart", func(t *testing.T) {
		a, err := NewGorm(db)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("TRUNCATE TABLE links").Error; err != nil {
			t.Fatal(err)
		}
		code, _ := a.Save("https://go.dev/doc/")
		before, _ := a.Get(code)

		db2, err := OpenPostgres(dsn) // a new connection pool, like a restarted server
		if err != nil {
			t.Fatal(err)
		}
		b, err := NewGorm(db2)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Close() })
		after, err := b.Get(code)
		if err != nil || !after.CreatedAt.Equal(before.CreatedAt) {
			t.Fatalf("after restart: %+v, %v (before %+v)", after, err, before)
		}
		if again, _ := b.Save("https://go.dev/doc/"); again != code {
			t.Fatalf("same URL after restart gave %q, want %q", again, code)
		}
	})

	// With a real connection pool the goroutines truly race on the INSERT.
	// The unique constraints must leave exactly one row.
	t.Run("race leaves exactly one row", func(t *testing.T) {
		s, err := NewGorm(db)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("TRUNCATE TABLE links").Error; err != nil {
			t.Fatal(err)
		}
		const n = 100
		codes := make([]string, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start // release everyone at once
				c, err := s.Save("https://example.com/hot")
				if err != nil {
					t.Errorf("save: %v", err)
				}
				codes[i] = c
			}(i)
		}
		close(start)
		wg.Wait()
		for _, c := range codes {
			if c != codes[0] {
				t.Fatalf("codes differ: %q vs %q", c, codes[0])
			}
		}
		if cnt, err := s.Count(); err != nil || cnt != 1 {
			t.Fatalf("Count = %d, %v; want exactly 1 row", cnt, err)
		}
	})
}
