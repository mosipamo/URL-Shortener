# Design Decisions

## Dependencies

There are **no** modules that require an entry here. Every module in `go.mod` is part of an explicitly allowed category:

- `github.com/gin-gonic/gin` — the HTTP router and context framework used by `internal/httpapi` (replacing the standard library `net/http` `ServeMux` that Parts 1–4 started with). 
- `gorm.io/gorm` plus the SQL drivers `gorm.io/driver/postgres` and `gorm.io/driver/sqlite` — GORM and its SQL driver(s) for Part 4 persistence. SQLite is used only in tests.
- All remaining entries are indirect (`// indirect`) or Gin's own transitive dependencies.

`.env` loading is done by `internal/envfile`, a standard-library-only parser, so no third-party env library (e.g. `godotenv`) is used.

## Part 1

- **When two URLs are considered "the same" (normalization):** Surrounding whitespace is trimmed, the scheme and host are lowercased, and an empty path is normalized to `/` (`https://Example.com` == `https://example.com/`). Other components are deliberately left unchanged because servers may treat them as different resources: path case, trailing slashes on non-empty paths (`/x` vs `/x/`), query parameter order, fragments, and default ports. The normalized URL is used both as the idempotency key and as the URL we redirect to.

- **How the same URL maps to the same code:** A second map, `byURL: longURL → code`, is maintained alongside `links: code → longURL`. `Save` checks `byURL` first and returns the existing code if the URL has already been shortened. This still returns HTTP 201, as required by the specification.

- **How codes are generated (new URLs only):** Each new URL receives a 7-character random code using characters from `[0-9a-zA-Z]` and `crypto/rand`. Random bytes ≥ 248 are rejected to avoid modulo bias, ensuring that each character is equally likely. Codes are not sequential, so they cannot be enumerated simply by counting.

- **Collision handling:** While holding the lock, if a generated code already exists in `links` and belongs to a different URL, another code is generated. This is retried up to 10 times, after which `ErrCodeExhausted` is returned (HTTP 500). With `62^7 ≈ 3.5 × 10^12` possible codes, this is practically unreachable in normal operation; the behavior is tested using an injected code generator. Therefore, distinct URLs never share the same code.

- **Mutex type and locking strategy:** A single `sync.Mutex` is used. Both `Save` and `Get` acquire the lock. `Save` performs the lookup, code generation, and insertion within the same critical section, ensuring that concurrent requests for the same URL receive the same code. The use of `RWMutex` is deferred to Part 3, where it will be evaluated using benchmarks.

- **Validation / SSRF:** A URL must be non-empty and parseable, use either the `http` or `https` scheme, and contain a host. The server never fetches the long URL; it only stores it and redirects clients to it. Part 6 adds a host policy (no localhost / private IPs / denylist) on top of this.

- **Package layout:** `cmd/server` contains flags and application wiring. `internal/model` holds the domain types (`Link`, sentinel errors, `MaxURLLength`) shared across all layers. `internal/repository` declares the `Repository` interface consumed by the HTTP handlers. `internal/store` holds the `Memory` and `GormStore` implementations plus the `Normalize` utility. `internal/httpapi` wires Gin routes and handlers.

## Part 2

- **`Repository` interface location and methods.** Declared in `internal/repository` (a dedicated contract package), implemented by `internal/store.Memory` and `internal/store.GormStore`:

```go
type Repository interface {
    Save(rawURL string) (code string, err error)
    Get(code string) (link model.Link, err error)
}
```

`model.Link{URL, CreatedAt}` is the shared domain type, defined in `internal/model` so it has no dependencies on either the store or the HTTP layer. `Save` returns only the code; handlers retrieve `created_at` through `Get`. Compile-time assertions guard that every concrete store satisfies the interface: in `internal/store/contract_test.go` (close to the implementations) and in `internal/httpapi/fake_test.go` (close to the consumer).

- **Sentinel errors.** `model.ErrNotFound`, `model.ErrInvalidURL`, and `model.ErrCodeExhausted` live in `internal/model` so that neither the store implementations nor the HTTP layer need to import each other to compare errors. The store wraps them with `%w`, e.g. `get "abc": code not found`.
- **How errors become status codes and bodies.** One method, `writeStoreError`, used by every Gin handler, switches with `errors.Is`:

    | Error                                                                                                                                                     | Status | Body                                                                                   |
    | --------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ | -------------------------------------------------------------------------------------- |
    | `ErrInvalidURL`                                                                                                                                           | 400    | `{"error":"<store message>"}` (safe, tells the client what is wrong)                   |
    | `ErrNotFound`                                                                                                                                             | 404    | `{"error":"not found"}` (fixed text, so internal wrapping and the code are not echoed) |
    | anything else                                                                                                                                             | 500    | `{"error":"internal error"}`; the real error is only logged                            |

Malformed JSON is a handler-level 400 (`invalid JSON body`) because it never reaches the store. All error bodies share the `{"error": "..."}` shape.

- **HTTP framework.** Gin (`github.com/gin-gonic/gin`) replaces the stdlib mux. Handlers receive a `*gin.Context` instead of `(http.ResponseWriter, *http.Request)` (I used stdlib mux in parts 1 to 4 and then refactored afterwards). `c.ShouldBindJSON` decodes the request body; `c.JSON` / `c.Redirect` write responses. Gin's radix-tree router resolves `GET /:code` vs `GET /api/v1/links/:code` without conflict because static path prefixes take priority over parameter wildcards. Gin does not derive `HEAD` from `GET`, so `HEAD /:code` is registered explicitly (`curl -I` works).
- **Logger middleware.** `httpapi.Logger(*log.Logger) gin.HandlerFunc` is a Gin middleware factory. It is injected into `New(repo, base, logger)` so tests can capture log output with `log.New(&buf, "", 0)` and production code passes `log.Default()`. The query string is omitted from log lines to prevent token/secret leakage.
- **`created_at`.** Set once, when the code is first created, in UTC from an injectable clock (`Memory.now`) so tests are deterministic. Returned as RFC3339 (`2026-01-15T12:00:00Z`), formatted explicitly because Go's default `time.Time` JSON adds fractional seconds.
- **Idempotency after the refactor.** `byURL` is unchanged and a repeated `Save` returns the existing code without touching the stored `Link`, so `created_at` stays the original. Tested in the store (advancing clock) and over HTTP (`TestLinkInfoViaRealStoreAndIdempotency`).

## Part 3

- **Locking choice: `sync.Mutex`** (one lock; `Save` and `Get` both take it;
  `Save` does lookup → generate → insert in one critical section, as in
  Part 1). A `sync.RWMutex` variant, `MemoryRW`, is kept in the repo
  (`internal/store/memory_rwmutex_test.go`, compiled only into tests, not into the server binary) purely so the comparison below is
  reproducible; it is covered by the same contract suite. `Memory` (Mutex) is
  the store the server actually uses.
- **Measurements.** Intel i5-13450HX (16 logical CPUs), `-count=6`,
  `-benchtime=2s`, median ns/op, lower is better. Run the comparison with:

    ```text
    go test -run='^$' -bench=BenchmarkLockCompare -benchmem -count=6 -benchtime=2s ./internal/store/
    ```

    Read-only and create paths (`BenchmarkLockCompare{Get,SaveExisting,SaveNew}`):

    | Benchmark      | Threads | Mutex | RWMutex | Result                          |
    | -------------- | ------: | ----: | ------: | ------------------------------- |
    | `Get`          |       1 |  58.6 |    63.7 | ~equal (Mutex slightly ahead)   |
    | `Get`          |       4 | 100.0 |   156.7 | Mutex faster (~36%)             |
    | `Get`          |      16 | 161.5 |   147.5 | RWMutex ~9% faster (near noise) |
    | `SaveExisting` |      16 | 330.0 |   390.0 | ~equal, noisy (read fast path)  |
    | `SaveNew`      |      16 | ~2900 |   ~2800 | equal (noise)                   |

    Mixed read/create workloads (`BenchmarkLockCompareMixed`, one create per N ops),
    16 threads, settled values:

    | Reads | Mutex | RWMutex | Result                |
    | ----: | ----: | ------: | --------------------- |
    |   90% | 120.5 |    58.5 | RWMutex ~2× faster    |
    |   99% |  74.5 |    59.5 | RWMutex ~1.25× faster |
    | 99.9% |  70.3 |    59.5 | RWMutex ~1.2× faster  |

    Notes: (read these before trusting any single cell):
    - The whole series shows clear **thermal throttling**: absolute ns/op rise
      through a run (e.g. `Get` Mutex-16 drifts 147 → 169 ns, `Get` Mutex-4
      38 → 107 ns). Run one benchmark at a time and discard the first
      `-count` to reduce this.
    - **RWMutex needs a warm-up** in the mixed benchmark: its first iteration
      is cold at ~210–256 ns and only then settles to ~57–60 ns, while Mutex
      is stable from the start. That is why the same command produced _two
      opposite_ mixed results: an earlier run had RWMutex at ~180 ns at 90%
      reads, a later run had it at ~58 ns. The table uses the settled values.
    - `SaveExisting` and `SaveNew` are within noise of each other in both
      variants; `Normalize` (`url.Parse` + `String`) dominates and the lock
      is not the cost there.

    Conclusion: **the two locks are effectively a wash.** `RWMutex` wins pure
    reads only at high thread counts (~9% at 16, within noise) and is _behind_ at
    4 threads (~36%); it does not win the write paths. The mixed case flips with
    warm-up — one run of the same benchmark had `RWMutex` ~2× ahead, another had
    it behind — and the run-to-run spread (~58 vs ~180 ns at 90% reads) is larger
    than the difference between the two locks. Across all cases, neither lock
    wins reliably. `Mutex` is kept anyway, for reasons that do not depend on
    winning the micro-benchmark:
    1. **It does not matter end-to-end.** The store lookup is ~60–160 ns,
       while a full redirect through Gin is ~8.6 µs and a create is ~17 µs
       (see profile below); the lock is well under 1% of each request.
       Even a 2× gain on the lookup is invisible at the HTTP layer.
    2. **It is simpler and has one correct critical section.** `Save`
       takes the lock once and does lookup → generate → insert atomically.
       The RWMutex `Save` needs a read-lock probe, an unlock, then a write
       lock and a _second_ lookup (double-checked locking), which is more
       code and easier to get subtly wrong.
    3. **The results are machine-sensitive.** The same
       binary produced very different mixed numbers across runs. Choosing
       the simpler lock avoids betting on a difference that does not
       reproduce reliably.
       If the store ever became a _measured_ bottleneck in production, the
       honest next step is re-running this benchmark on the deployment's Go
       version and hardware — and then sharding the maps by code or using an
       `atomic.Pointer` to an immutable map, which beat both locks here.

- **Where the time actually goes (profile of the redirect benchmark).** A
  store `Get` is ~150–165 ns, while a full redirect through the Gin handler is
  ~8.6 µs/op with 30 allocs/op (numbers taken after the Gin refactor, which
  added JSON/context handling on the redirect path — see README "Performance";
  the lock comparison above was benchmarked separately against the store).
  In the current CPU profile, `runtime.mallocgc` is ~24% cumulative,
  `httptest.NewRequest` ~21%, `gin.(*Engine).ServeHTTP` ~21% and
  `httpapi.(*server).handleRedirect` ~14%. The largest flat entries are all
  runtime: `runtime.preemptM` (11.7%), `runtime.runqgrab` (10.3%) and
  `runtime.semasleep` (9.4%) — goroutine preemption and parking under 16-way
  concurrency. `store.(*Memory).Get` does not appear in the top nodes at all.
  Lock choice therefore barely matters for end-to-end speed; request-path
  allocation and runtime scheduling dominate.
  For `SaveExisting`, most of the time is in `Normalize` (`url.Parse` +
  `URL.String`), not in the lock. Possible future optimization (not done, not
  needed yet): skip re-parsing for inputs that are already canonical.
  Note: `ns/op` in `RunParallel` benchmarks is wall-clock time divided by
  total operations across all goroutines, so it measures throughput, not the
  latency of one request.
- **Server timeouts** (set in `cmd/server/main.go`, constants at the top):
    | Setting                                                                     | Value | Why / behavior                                                                                                                                                                                                       |
    | --------------------------------------------------------------------------- | ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
    | `ReadHeaderTimeout`                                                         | 5 s   | Headers of a normal request arrive in milliseconds. A client that trickles headers (slowloris) is cut off. Observed: a connection that never finishes its headers, or sends nothing, is closed silently after 5.0 s. |
    | `ReadTimeout`                                                               | 10 s  | Whole request including body. Bodies are tiny JSON (also capped at 1 MiB), so 10 s is generous.                                                                                                                      |
    | `WriteTimeout`                                                              | 10 s  | Responses are small; a client that stops reading is dropped.                                                                                                                                                         |
    | `IdleTimeout`                                                               | 60 s  | Keep-alive connections waiting for another request are closed after a minute, freeing file descriptors.                                                                                                              |
    
    `newServer` builds the server so a test can assert the timeouts are set and ordered sensibly.

- **Eviction cap: none.** For a cache, evicting is harmless; for a URL
  shortener it is data loss: every place the short link was shared would start
  returning 404. Memory use grows ~0.5 KB per new link (upper bound, from
  `B/op` of `BenchmarkMemorySaveNew`), so ~1M links is roughly 0.5 GB.

## Part 4

- **Storage choice: GORM + PostgreSQL 16.** Links must survive restarts and the database enforces the 
  uniqueness rules, which a single mutex-protected map or file cannot do once there is more than one 
  server process. The in-memory `Memory` store stays the default (`-store memory`), so `go run ./cmd/server` 
  works with nothing installed. `GormStore` implements the same
  `repository.Repository` interface (`Save`, `Get`); the handlers did not change.
  A compile-time assertion in `fake_test.go` guards that.
- **Choosing the store and configuration.** `DATABASE_URL` is read from the environment, which can be
  filled from a `.env` file in the working directory (see `.env.example`).
  Only `-addr` and `-base` are command-line flags. Go cannot read `.env` by itself, so `internal/envfile` is a
  standard-library loader instead of a third-party module (no new
  entry in Dependencies). Real environment variables win over the file; a
  missing file is fine. The DSN holds the password, so it is never logged or
  put in error messages (tests check both the loader errors and a failed
  connection). At startup the server connects, pings (5 s timeout) and fails
  fast if the database is unreachable.
- **Startup load.** The store keeps no in-memory copy: every read goes to
  PostgreSQL, so nothing needs "loading". Startup connects, runs
  `AutoMigrate` and logs how many links already exist (`postgres store
ready: N existing links`). Restart tests prove earlier links resolve.
- **Schema and migrations.** One table, model `linkRow`:

    | Column       | Type            | Constraint                               |
    | ------------ | --------------- | ---------------------------------------- |
    | `code`       | `varchar(16)`   | PRIMARY KEY                              |
    | `url`        | `varchar(2048)` | NOT NULL, UNIQUE index (`idx_links_url`) |
    | `created_at` | `timestamptz`   | NOT NULL                                 |

Migration is GORM `AutoMigrate` at startup: creates the table and indexes if
missing and adds missing columns, never drops or renames. That is enough for
one table; with schema changes over time I would switch to versioned
migration files. Known limit: several replicas starting at the very same
moment on an empty database could race to create the table.

- **URL length limit.** Normalized URLs longer than 2048 bytes are rejected as
  `ErrInvalidURL` (HTTP 400), in `Normalize`, for both stores. PostgreSQL
  cannot index values over ~2.7 KB, so without a limit a very long URL would
  fail the INSERT with a 500.
- **Idempotency with persistence.** The URL is normalized first (same rules as
  Part 1), then `url` is looked up. If it exists, its code is returned (fast
  path). Otherwise `INSERT ... ON CONFLICT DO NOTHING` runs and `RowsAffected`
  decides: 1 means inserted; 0 means a unique constraint fired, so the URL is
  looked up again: found means another request won the race and we return its
  code, not found means our random code collided with a different URL and we
  retry with a new code (10 tries, then `ErrCodeExhausted`, HTTP 500). The
  database serializes conflicting inserts, so no Go mutex or transaction is
  needed, and it also works across several server processes. Verified: 100
  goroutines released at once against real PostgreSQL leave exactly one row
  (test run 5 times). After a restart the same URL returns the same code
  (`TestGormRestart`, and manually with the real binary).
- **Persist before 201.** The handler answers only after `Save` returns, and
  `Save` returns only after its single INSERT statement has been committed
  (autocommit; no code path responds earlier).
- **Crash safety and atomicity.** A single INSERT is one atomic transaction, so
  there are no partial rows. PostgreSQL's default `synchronous_commit = on`
  flushes the write-ahead log before COMMIT returns, so an acknowledged link
  survives a crash of either process. If the server dies before the commit
  there is no row and no 201. If it dies after the commit but before the
  response, the client retries and idempotency returns the same code. The
  compose file keeps data in a named volume.
- **How `created_at` is stored.** `timestamptz`, set once by the application
  (not a database default) in UTC, truncated to microseconds, which is
  PostgreSQL's precision, so `Memory` and `GormStore` return identical values
  and round trips compare equal. The injectable clock keeps tests
  deterministic. A repeated `Save` never updates the row (DO NOTHING), so
  `created_at` is always the first creation time. Returned as RFC3339 in UTC.
- **Errors.** `gorm.ErrRecordNotFound` is translated to `ErrNotFound` with
  `%w`; every other database error is wrapped with context, is not
  `ErrNotFound` or `ErrInvalidURL`, and therefore becomes a 500 whose real
  text is only logged (Part 2 mapping, unchanged). A test closes the database
  and checks exactly that.
- **Test strategy.**
    1. One shared contract suite (`runContract` in `contract_test.go`) takes a
       `func(t *testing.T) repository.Repository` factory and runs the same
       behavioural assertions against every implementation. `TestContractMemory`,
       `TestContractGorm` (SQLite), and `TestContractPostgres` (real PostgreSQL,
       skipped unless `TEST_DATABASE_URL` is set) all call it, proving the
       stores are interchangeable. The local `linkStore` interface that
       previously duplicated `repository.Repository` has been removed.
    2. **Compile-time assertions** (`var _ repository.Repository = (*Memory)(nil)`
       etc.) live in `contract_test.go` (close to the implementations) and
       in `httpapi/fake_test.go` (close to the consumer). A missing or
       wrong-signature method is a build failure, not a runtime panic.
    3. SQLite (`gorm.io/driver/sqlite`, an allowed GORM SQL driver, test-only) is
       used so the GORM code runs in `go test` without a database server. One
       connection (`SetMaxOpenConns(1)`) avoids "database is locked". The driver
       needs cgo, which `go test -race` needs anyway.
    4. `openSQLite(tb testing.TB, path string)` accepts the `testing.TB`
       interface so the same helper is used by both `*testing.T` tests
       (`newTestGorm`) and `*testing.B` benchmarks (`newBenchGorm`) without
       code duplication.
    5. Restart test: a new store instance on the same database file sees the
       link, the same `created_at`, and the same code for a repeated save.
    6. Collision, exhaustion and race-on-URL paths are tested with an injected
       code generator and by calling `tryInsert` twice for the same URL.
    7. **GormStore benchmarks** (`BenchmarkGormGet`, `BenchmarkGormSaveExisting`,
       `BenchmarkGormSaveNew` in `bench_test.go`) quantify the ORM + SQLite
       overhead versus the in-memory store. They run single-goroutine
       (`b.Loop()`) to avoid SQLite's serialised connection masking other costs.
       Representative numbers on this machine (Go 1.25, `-count=3`, median):
       `BenchmarkGormGet` ~194 µs vs `BenchmarkMemoryGet` ~161 ns — the ORM +
       file I/O is ~1200× slower than the mutex-protected map, confirming that
       the persistent store is suitable for durability, not for latency-critical
       hot paths without a cache.
- **Limitations (accepted).** `Store` methods take no `context.Context`, so a
  slow query is not cancelled when the HTTP client disconnects (the pool and
  server timeouts bound it). Pool limits are fixed (25 open, 5 idle, 30 min
  lifetime).

## Part 5

It would have been possible to implement a global cache (like the one I created) that works across the entire project, but I wasn't sure if it would fully meet your requirements, so I didn't proceed with that approach.

This was an optional in-process read-through cache, mainly to reduce repeated lookups for hot codes while keeping the underlying store as the source of truth:

```go
// internal/store/cached.go

type CachedStore struct {
    inner repository.Repository
    mu    sync.RWMutex
    links map[string]model.Link
}

func NewCachedStore(inner repository.Repository) *CachedStore {
    return &CachedStore{
        inner: inner,
        links: make(map[string]model.Link),
    }
}

func (c *CachedStore) GetWithMeta(code string) (model.Link, bool, error) {
    c.mu.RLock()
    link, ok := c.links[code]
    c.mu.RUnlock()
    if ok {
        return link, true, nil
    }

    link, err := c.inner.Get(code)
    if err != nil {
        return model.Link{}, false, err
    }

    c.mu.Lock()
    c.links[code] = link
    c.mu.Unlock()
    return link, false, nil
}
```

```go
// cmd/server/main.go

cache := flag.Bool("cache", defaultCacheType, "wrap the store in an in-memory read-through cache")

if *cache {
    repo = store.NewCachedStore(repo)
    log.Printf("using in-memory read-through cache")
}
```

- **Stateless app behind a load balancer.** The service is designed to be
  horizontal: one binary behind a load balancer, N identical replicas, each
  speaking the same `repository.Repository` contract. No replica keeps
  node-local truth beyond the store, so adding capacity does not reshuffle
  existing short links.
- **Shared store.** Production uses PostgreSQL (`-store postgres`). A
  per-pod in-memory map would split-brain redirects across replicas; the
  shared database is the source of truth for every create and lookup.
- **CDN / edge caching for redirects (read path).** Hot `GET /{code}`
  responses are cacheable at the edge as **302** with a short TTL
  (suggested **60–300 s**) and **stale-while-revalidate**. Tradeoffs:
  - **Cost / load:** most redirect traffic never hits the origin.
  - **Consistency:** if a link were ever deleted or retargeted, clients
    can see a **stale Location** until the TTL expires. Acceptable for a
    shortener where codes are immutable after create (this project never
    updates a stored URL). If deletion were added later, the CDN would
    need purge-by-key or a very short TTL.
  - **Origin headers:** a production edge config would set
    `Cache-Control: public, max-age=60, stale-while-revalidate=30` on
    successful redirects and `Cache-Control: no-store` on 404s so unknown
    codes are not cached as empty.
- **Write-path scaling.** Create traffic is guarded by the in-process rate
  limiter on `POST /api/shorten` (see Part 6). If creates became the
  bottleneck, the next steps would be an async queue (accept → enqueue →
  worker persists) or a pre-generated code pool; neither is needed yet
  because redirect volume dominates and the DB already serializes
  idempotent inserts.
- **Sharding / partitioning strategy.** Not implemented: a single Postgres
  instance is enough for the current code space (`62^7`) and expected load.
  When one database or table becomes the throughput bottleneck, partition
  by **hash of the short code** (e.g. first character or
  `crc32(code) % N`) into N shards, each a separate Postgres (or schema).
  Routing: the redirect path hashes the code and talks to one shard; the
  create path picks a shard the same way after generating the code (or
  generates until the code lands on a preferred shard). Tradeoffs:
  - **Pros:** linear write/read capacity; hot codes stay on one shard so
    per-shard caches stay effective.
  - **Cons:** cross-shard idempotency (same long URL on two shards) needs
    either a global URL index or accepting rare duplicate codes for one
    URL; rebalancing shards later is operationally expensive. Until that
    pain appears, vertical scale + read replicas / CDN is cheaper.

## Part 6

- **Graceful shutdown.** `cmd/server/main.go` uses
  `signal.NotifyContext` for `SIGINT` / `SIGTERM`, then
  `Server.Shutdown` with a **10 s** drain timeout. The listener stops
  accepting new connections; in-flight requests finish (or are cut when
  the timeout fires). After `ListenAndServe` returns, `defer` closes the
  store. Manual check: start the server, `curl` a slow client or a normal
  request, send Ctrl+C — logs show `server stopped cleanly`.
- **Rate limit parameters.** `POST /api/shorten` only: default
  **60 requests per client IP per 1 minute** (`-rate-limit`,
  `-rate-window`). Excess returns **429** with
  `{"error":"too many requests"}`. Redirects and metadata are not limited.
  Implementation is an in-process sliding window (no extra dependency);
  behind a load balancer each replica counts separately unless a shared
  store (Redis) is added later.
- **Domain policy (blocklist).** Enforced in `store.Normalize` /
  `checkHostPolicy` before any insert (HTTP **400**, `ErrInvalidURL`):
  - **Allow:** `http` / `https` only, non-empty host, length ≤ 2048,
    public hosts (no DNS lookup of the target — the server never fetches
    the long URL).
  - **Block:** `localhost` and `*.localhost`; loopback, private
    (RFC 1918 / ULA), link-local, and unspecified IPs (covers cloud
    metadata `169.254.169.254`); plus a small hostname denylist
    (`malware.example`, `phishing.example`) extensible in
    `blockedHosts`.
  - **Not covered:** a public hostname that later resolves to a private
    IP (DNS rebinding). Mitigating that would need resolve-at-create or
    egress controls, which is out of scope here.
- **What is logged and never logged.** Each HTTP request produces one JSON
  `slog` record with method, Gin route template, status code/text, and
  fractional duration in milliseconds. Statuses 4xx are `WARN`; 5xx are
  `ERROR`.
  The raw URL, query string, request body, redirect target, and database
  password are never logged. Using the route template also avoids logging
  user-provided path values; unmatched routes are recorded as `unmatched`.
- **Observability.** Structured request events go to the configured logger
  output (stderr in the server). Their timestamps support deriving RPS by
  counting requests over time windows, while status fields support error
  counts and rates in a log aggregator. No metrics or pprof endpoint is
  exposed, so there is no diagnostic endpoint to secure.
