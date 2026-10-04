# URL Shortener

In-memory URL shortener. (Currently: Parts 1–3.)

## Run

    go run ./cmd/server -addr :8080 -base http://localhost:8080

| Flag    | Default                 | Meaning                          |
| ------- | ----------------------- | -------------------------------- |
| `-addr` | `:8080`                 | listen address                   |
| `-base` | `http://localhost:8080` | prefix used to build `short_url` |

## Try it

    curl -s -X POST localhost:8080/api/shorten \
      -H 'Content-Type: application/json' -d '{"url":"https://go.dev/doc/"}'
    # {"code":"a1B2c3d","short_url":"http://localhost:8080/a1B2c3d"}

    curl -sI localhost:8080/a1B2c3d     # 302 Found + Location: https://go.dev/doc/

    # same URL again -> same code (still 201)
    curl -s -X POST localhost:8080/api/shorten -d '{"url":"https://go.dev/doc/"}'

## Link metadata

    curl -s localhost:8080/api/v1/links/a1B2c3d
    # {"url":"https://go.dev/doc/","created_at":"2026-01-15T12:00:00Z"}   (404 if unknown)

Errors are always JSON: `{"error":"..."}` with 400 (bad input), 404 (unknown code) or 500.

**Idempotency policy:** the same URL (after normalization, see DECISIONS.md)
always returns the same code, and `created_at` keeps the time of the first
shorten. Covered by tests in `internal/store` and `internal/httpapi`.

## Verify

    gofmt -l . && go vet ./... && go test -race ./...
    go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -n1

## Performance

    go test -run='^$' -bench=. -benchmem ./...                     # benchmarks only
    go test -run='^$' -bench=BenchmarkRedirect -cpuprofile cpu.out ./internal/httpapi
    go tool pprof -top cpu.out

**Benchmark line** (Intel i5-13450HX, 16 threads, Go 1.22):
`BenchmarkRedirect-16  1385 ns/op  6033 B/op  22 allocs/op` (full handler path,
parallel throughput; measured before switching back to `Mutex`, the store is
~2% of it so the change is within noise). Store lookup alone with the final
`Mutex`: `BenchmarkGet-16  ~53 ns/op  0 B/op  0 allocs/op`.

**Profiling insight:** in the redirect CPU profile the store lookup plus its
lock is only ~2% of CPU, while `runtime.mallocgc` (allocation) is ~42%, mostly
from request/response handling at ~6 KB and 22 allocations per request. The
lock is not the bottleneck; allocations are. I benchmarked `Mutex` against
`RWMutex` and kept `Mutex`: `RWMutex` only wins on 100%-read loops and loses
as soon as creates are mixed in (details in Decisions.md, Part 3).

Design choices are in [Decisions.md](Decisions.md).
