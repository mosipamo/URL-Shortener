# URL Shortener

URL shortener in Go.
(Parts 1–4 complete; Parts 5–6 covered via architecture docs, rate limiting, graceful shutdown, and domain policy.)

## Run

    go run ./cmd/server                                                          # in memory: data is lost on restart
    go run ./cmd/server -addr :9000 -base http://localhost:9000 -store postgres  # in PostgreSQL: links survive restarts

| Flag          | Default                 | Meaning                                        |
| ------------- | ----------------------- | ---------------------------------------------- |
| `-addr`       | `:8080`                 | listen address                                 |
| `-base`       | `http://localhost:8080` | prefix used to build `short_url`               |
| `-store`      | `memory`                | store kind (memory or postgres)                |
| `-rate-limit` | `60`                    | maximum create requests per IP in a window     |
| `-rate-window`| `1m0s`                  | window for the shorten rate limiter            |

The shorten endpoint is rate-limited to 60 requests per minute per client IP by default, so a burst of repeated `POST /api/shorten` requests is rejected with `429 Too Many Requests`.

**Graceful shutdown:** Ctrl+C (`SIGINT`) or `SIGTERM` stops accepting new connections, drains in-flight requests for up to 10 seconds, then closes the store and exits (`server stopped cleanly`).

**Domain policy:** create rejects localhost, private/link-local/loopback IPs, and a small hostname denylist (HTTP 400). The server never fetches the long URL. Details in [DECISIONS.md](DECISIONS.md) Part 6.
## Run with PostgreSQL (links survive restarts)

1. Create a database and a user in PostgreSQL (pgAdmin or `psql`), e.g. user
   `shortener`, database `shortener`.
2. Create the config file in the project root (next to `go.mod`):

        cp .env.example .env        # then edit DATABASE_URL

    `.env` holds the one setting that contains a secret:

        DATABASE_URL=postgres://shortener:secret@localhost:5432/shortener?sslmode=disable

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

**Idempotency policy:** the same URL (after normalization, see DECISIONS.md)
always returns the same code, and `created_at` keeps the time of the first
shorten. Covered by tests in `internal/store` and `internal/httpapi`.

## Verify

    gofmt -l . && go vet ./... && go test -v -race ./...
    go test -v -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -n1
    # total: (statements) 81.5%

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
- In windows, I needed to disable my security protection(antivirus) to execute `-race` in /cmd/server!!

## Performance

Benchmarks were run on an Intel Core i5-13450HX system with 16 logical threads using Go 1.25.

```text
go test -run='^$' -bench=. -benchmem ./...
```

Selected benchmark results:

| Benchmark                      |     ns/op |  B/op | allocs/op | Notes                                              |
| ------------------------------ | --------: | ----: | --------: | -------------------------------------------------- |
| `BenchmarkRedirect-16`         |      8616 |  6500 |        30 | Full Gin redirect handler path, parallel benchmark |
| `BenchmarkShorten-16`          |     16940 |  7616 |        40 | Full shorten handler path, parallel benchmark      |
| `BenchmarkMemoryGet-16`        |     160.6 |     0 |         0 | In-memory lookup under synchronization             |
| `BenchmarkMemorySaveNew-16`    |      2580 |   646 |         5 | New in-memory link                                 |
| `BenchmarkGormGet-16`          |    194129 |  3599 |        62 | GORM + SQLite lookup                               |
| `BenchmarkGormSaveExisting-16` |    190765 |  3799 |        64 | Existing-link persistence                          |
| `BenchmarkGormSaveNew-16`      |   3340094 | 10135 |       129 | New-link persistence                               |

The in-memory lookup is extremely fast compared with the full HTTP redirect path: approximately 160 ns versus 8.6 µs in these benchmarks. This shows that most of the redirect benchmark cost comes from the HTTP/Gin request path rather than the basic in-memory map lookup.

`BenchmarkGormGet` is approximately 1200× slower than `BenchmarkMemoryGet` in this benchmark environment. This difference is expected because the GORM benchmark includes ORM processing and SQLite database I/O, while the memory benchmark performs only an in-memory lookup. In production, PostgreSQL is used instead of SQLite and connections are managed through a database connection pool.

### CPU profiling

A CPU profile was collected using:

```text
go test -run='^$' -bench=BenchmarkRedirect -cpuprofile cpu.out ./internal/httpapi
go tool pprof -top cpu.out
```

The profile shows substantial runtime overhead from synchronization, scheduling, memory allocation, and garbage collection. This is expected in a parallel HTTP benchmark (`BenchmarkRedirect-16`), where multiple goroutines execute the handler concurrently.

Application-level functions such as Gin request handling, middleware, redirect rendering, and `handleRedirect` appear in the profile, while a significant portion of cumulative CPU time is spent inside the Go runtime. The benchmark also reports 6485 B/op and 30 allocations/op for the full redirect path, indicating that allocation and garbage-collection overhead are relevant areas for further optimization if higher throughput is required.

**Profiling insight:** the CPU profile shows that most of the benchmark time is spent in Go runtime operations — scheduling, synchronization, allocation and garbage collection. In particular, `runtime.mallocgc` accounts for about 24% cumulative CPU time, `httptest.NewRequest` (benchmark harness, not server code) about 21%, `gin.(*Engine).ServeHTTP` about 21%, and `httpapi.(*server).handleRedirect` about 14%. The top flat entries are all runtime: `runtime.preemptM` (11.7%), `runtime.runqgrab` (10.3%) and `runtime.semasleep` (9.4%), i.e. goroutine preemption and parking under 16-way contention. The full redirect benchmark reports approximately 6.5 KB of allocations and 30 allocations per operation. This indicates that allocation and runtime overhead are relevant areas for further optimization, and the profile does not indicate that the store lock is the primary bottleneck.

### Mutex vs RWMutex

`Memory` uses a single `sync.Mutex`. A `sync.RWMutex` variant, `MemoryRW`
(`internal/store/memory_rwmutex.go`), is kept only so the comparison is
reproducible (it is covered by the same contract tests and is not used by the
server). Measured on an i5-13450HX with **Go 1.25**, `-count=6 -benchtime=2s`,
median ns/op, lower is better:

```text
go test -run='^$' -bench=BenchmarkLockCompare -benchmem -count=6 -benchtime=2s ./internal/store/
```

| Benchmark        | Threads | Mutex | RWMutex | Result                          |
| ---------------- | ------: | ----: | ------: | ------------------------------- |
| `Get`            |       1 |  58.6 |    63.7 | ~equal                          |
| `Get`            |       4 | 100.0 |   156.7 | Mutex faster (~36%)             |
| `Get`            |      16 | 161.5 |   147.5 | RWMutex ~9% faster (near noise) |
| `SaveExisting`   |      16 | 330.0 |   390.0 | ~equal, noisy                   |
| `SaveNew`        |      16 | ~2900 |   ~2800 | equal (noise)                   |

Mixed read/create workloads (one create per N ops), 16 threads, warm values:

| Reads | Mutex | RWMutex | Result                |
| ----: | ----: | ------: | --------------------- |
|   90% | 120.5 |    58.5 | RWMutex ~2× faster    |
|   99% |  74.5 |    59.5 | RWMutex ~1.25× faster |
| 99.9% |  70.3 |    59.5 | RWMutex ~1.2× faster  |

Caveats: the numbers show clear thermal throttling (ns/op drifts up through a
run) and `RWMutex` needs a warm-up in the mixed benchmark (first iteration
~210 ns, then ~58 ns), so the same command can produce opposite mixed results
across runs.

**Conclusion: the two locks are effectively a wash.** `RWMutex` wins pure reads
only at high thread counts (~9% at 16, within noise) and is *behind* at 4
threads (~36%); it does not win the write paths. The mixed case flips with
warm-up — one run of the same benchmark had `RWMutex` ~2× ahead, another had it
behind — and the run-to-run spread (~58 vs ~180 ns at 90% reads) is larger than
the difference between the two locks. Across all cases, neither lock wins
reliably. Since the whole lookup is ~60–160 ns against a ~8.6 µs redirect and
~17 µs create, the lock is well under 1% of a request, so the simpler `Mutex`
(one atomic critical section, no double-checked locking) is kept. The full
analysis is in `DECISIONS.md`, Part 3.

Design choices are in [DECISIONS.md](DECISIONS.md).
