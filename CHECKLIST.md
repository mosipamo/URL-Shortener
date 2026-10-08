# Checklist

## Part 1 — MVP (25)
- [x] 4 `POST /api/shorten` returns 201 with `code` and `short_url`
- [x] 3 Idempotency: same URL returns the same `code` (tested)
- [x] 4 `GET /{code}` returns 302 with the correct `Location`
- [x] 3 Unknown code → 404; bad or missing URL → 400
- [x] 3 URL validation, no server-side fetch of the long URL
- [x] 2 Codes 6–8 characters; collision strategy for new codes only
- [x] 2 `-base` flag used for `short_url`
- [x] 2 httptest: shorten and redirect; table tests for bad URL and unknown code
- [x] 2 Concurrent test (including duplicate shorten); `go test -race ./...` passes

## Part 2 — API and errors (25)
- [x] 5 Metadata route `GET /api/v1/links/{code}` returns 200 / 404 with correct JSON
- [x] 4 `ErrNotFound`, `ErrInvalidURL` from store/domain
- [x] 4 `%w` + `errors.Is` in the HTTP mapping
- [x] 5 `Store` interface declared in `internal/httpapi` (the consumer) + fake used in tests
- [x] 4 Tests for the metadata route and error mapping
- [x] 3 Idempotency still tested after the Part 2 changes

## Part 3 — Performance (25)
- [x] 5 Server timeouts configured
- [x] 5 `Mutex` vs `RWMutex` matches the measurements and `DECISIONS.md`
- [x] 5 Benchmarks for shorten and redirect
- [x] 5 README: one benchmark line and one profiling insight
- [x] 5 Tests and `-race` still green

## Part 4 — Persistence (25)
- [x] 6 Persistent store (GORM + PostgreSQL)
- [x] 5 Startup load (connect, migrate, count existing links)
- [x] 5 Create persisted before the 201 response
- [x] 4 Restart test (temp SQLite file; optional real PostgreSQL)
- [x] 3 `-store` flag selects memory or persistent store
- [x] 2 `-race` clean with the persistent store

## Part 5 — Millions of requests (+10 max)
- [x] 4 `DECISIONS.md`: LB → N apps → shared store
- [x] 3 CDN / edge caching for redirects
- [x] 3 Write-path scaling (rate limit, queue, or code pool)
- [x] 3 Sharding / partitioning strategy
- [ ] 4 Bonus code: cache layer, load-test script + README numbers

## Part 6 — Production habits (+10 max)
- [x] 3 Graceful shutdown working + documented
- [x] 3 Rate limit on create
- [x] 2 Domain policy in `DECISIONS.md`
- [x] 2 What you log vs never log
- [x] 4 Bonus: structured request logging with Go `slog`
