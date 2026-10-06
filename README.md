# URL Shortener

URL shortener in Go.
(Currently: Parts 1–4.)

## Run

    go run ./cmd/server                                                          # in memory: data is lost on restart
    go run ./cmd/server -addr :9000 -base http://localhost:9000 -store postgres  # in PostgreSQL: links survive restarts

| Flag     | Default                 | Meaning                          |
| -------- | ----------------------- | -------------------------------- |
| `-addr`  | `:8080`                 | listen address                   |
| `-base`  | `http://localhost:8080` | prefix used to build `short_url` |
| `-store` | `memory`                | store kind (memory or postgres)  |

## Run with PostgreSQL (links survive restarts)

1. Create a database and a user in PostgreSQL (pgAdmin or `psql`), e.g. user
   `shortener`, database `shortener`.
2. Create the config file in the project root (next to `go.mod`):

        cp .env.example .env        # then edit DATABASE_URL

    `.env` holds the a single setting that may contain secrets:

        DATABASE_URL=postgres://shortener:secret@localhost:5432/shortener

3. Run from the project root (the server looks for `.env` in the current directory):

        go run ./cmd/server -store postgres

All dependencies (GORM, Gin) are already in `go.mod`; a fresh clone only
needs `go mod download` or just `go run ./cmd/server`
(the toolchain fetches them automatically).

- **Migrations:** none to run by hand. At startup the server runs GORM `AutoMigrate`,
  which creates the `links` table and its indexes if they are missing (and adds
  missing columns; it never drops or renames anything).
- **Inspect the data:** pgAdmin → Databases → shortener → Schemas → public →
  Tables → links → View/Edit Data, or `SELECT * FROM links;`.
- The database password is never logged.

## Try it

    curl -s -X POST localhost:8080/api/shorten \
      -H 'Content-Type: application/json' \
      -d '{"url":"https://go.dev/doc/"}'
    # {"code":"a1B2c3d","short_url":"http://localhost:8080/a1B2c3d"}

    curl -sI localhost:8080/a1B2c3d     # 302 Found + Location: https://go.dev/doc/

    # same URL again -> same code (still 201)
    curl -s -X POST localhost:8080/api/shorten \
      -H 'Content-Type: application/json' \
      -d '{"url":"https://go.dev/doc/"}'

## Link metadata

    curl -s localhost:8080/api/v1/links/a1B2c3d
    # {"url":"https://go.dev/doc/","created_at":"2026-01-15T12:00:00Z"}   (404 if unknown)

Errors are always JSON: `{"error":"..."}` with 400 (bad input), 404 (unknown code) or 500.

**Idempotency policy:** the same URL (after normalization, see Decisions.md)
always returns the same code, and `created_at` keeps the time of the first
shorten. Covered by tests in `internal/store` and `internal/httpapi`.

## Verify

    gofmt -l . && go vet ./... && go test -v -race ./...
    go test -v -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -n1
    # total: (statements) 85.2%

In Windows cmd there is no `tail`: run `go tool cover -func=coverage.out` and read the last line.

Tests need no database server: the GORM store is tested with SQLite files in
temp directories (same store code, different driver; the SQLite driver needs cgo,
which `-race` needs anyway). To also run the same contract tests against a real
PostgreSQL (use a throwaway database; the test TRUNCATEs `links`):

      set "TEST_DATABASE_URL=postgres://USER:PASSWORD@localhost:5432/DATABASE"    # Windows cmd
      go test -v ./internal/store/
      go test -v -race -count=1 -run Postgres ./internal/store

### Notes:

- Replace USER, PASSWORD, and DATABASE with your PostgreSQL test database credentials.
- If the password contains special URL characters such as #, URL-encode them.
- By running with your current database URL, your records in the table will be removed! You can test this with another test database url.

## Performance

Benchmarks were run on an Intel Core i5-13450HX system with 16 logical threads using Go 1.25.

```text
go test -run='^$' -bench=. -benchmem ./...
```

Selected benchmark results:

| Benchmark                      |   ns/op |  B/op | allocs/op | Notes                                              |
| ------------------------------ | ------: | ----: | --------: | -------------------------------------------------- |
| `BenchmarkRedirect-16`         |    1801 |  6498 |        30 | Full Gin redirect handler path, parallel benchmark |
| `BenchmarkShorten-16`          |    3407 |  7664 |        40 | Full shorten handler path, parallel benchmark      |
| `BenchmarkMemoryGet-16`        |   50.05 |     0 |         0 | In-memory lookup under synchronization             |
| `BenchmarkMemorySaveNew-16`    |    1199 |   675 |         5 | New in-memory link                                 |
| `BenchmarkGormGet-16`          |   41296 |  3599 |        62 | GORM + SQLite lookup                               |
| `BenchmarkGormSaveExisting-16` |   40309 |  3799 |        64 | Existing-link persistence                          |
| `BenchmarkGormSaveNew-16`      | 2044194 | 10134 |       130 | New-link persistence                               |

The in-memory lookup is extremely fast compared with the full HTTP redirect path: approximately 50 ns versus 1.8 µs in these benchmarks. This shows that most of the redirect benchmark cost comes from the HTTP/Gin request path rather than the basic in-memory map lookup.

`BenchmarkGormGet` is approximately 825× slower than `BenchmarkMemoryGet` in this benchmark environment. This difference is expected because the GORM benchmark includes ORM processing and SQLite database I/O, while the memory benchmark performs only an in-memory lookup. In production, PostgreSQL is used instead of SQLite and connections are managed through a database connection pool.

### CPU profiling

A CPU profile was collected using:

```text
go test -run='^$' -bench=BenchmarkRedirect -cpuprofile cpu.out ./internal/httpapi
go tool pprof -top cpu.out
```

The profile shows substantial runtime overhead from synchronization, scheduling, memory allocation, and garbage collection. This is expected in a parallel HTTP benchmark (`BenchmarkRedirect-16`), where multiple goroutines execute the handler concurrently.

Application-level functions such as Gin request handling, middleware, redirect rendering, and `handleRedirect` appear in the profile, while a significant portion of cumulative CPU time is spent inside the Go runtime. The benchmark also reports 6498 B/op and 30 allocations/op for the full redirect path, indicating that allocation and garbage-collection overhead are relevant areas for further optimization if higher throughput is required.

**Profiling insight:** the CPU profile shows that a significant portion of the benchmark time is spent in Go runtime operations such as scheduling, synchronization, memory allocation, and garbage collection. In particular, `runtime.mallocgc` accounts for about 0.64% flat CPU time and 20.55% cumulative CPU time in this profile. The full redirect benchmark reports approximately 6.5 KB of allocations and 30 allocations per operation. This indicates that allocation and runtime overhead are relevant areas for further optimization. The profile does not indicate that the store lock is the primary bottleneck.

I also benchmarked `Mutex` against `RWMutex` and kept `Mutex`: `RWMutex` only wins in purely read-heavy loops and loses its advantage when creates are mixed in. The details are documented in `Decisions.md`, Part 3.

Design choices are in [Decisions.md](Decisions.md).
