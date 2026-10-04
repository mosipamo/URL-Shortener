# Design Decisions

## Dependencies

None. The project uses only the Go standard library, with `net/http` for HTTP routing.

## Part 1

- **When two URLs are considered "the same" (normalization).** Surrounding whitespace is trimmed, the scheme and host are lowercased, and an empty path is normalized to `/` (`https://Example.com` == `https://example.com/`). Other components are deliberately left unchanged because servers may treat them as different resources: path case, trailing slashes on non-empty paths (`/x` vs `/x/`), query parameter order, fragments, and default ports. The normalized URL is used both as the idempotency key and as the URL we redirect to.

- **How the same URL maps to the same code.** A second map, `byURL: longURL → code`, is maintained alongside `links: code → longURL`. `Save` checks `byURL` first and returns the existing code if the URL has already been shortened. This still returns HTTP 201, as required by the specification.

- **How codes are generated (new URLs only).** Each new URL receives a 7-character random code using characters from `[0-9a-zA-Z]` and `crypto/rand`. Random bytes ≥ 248 are rejected to avoid modulo bias, ensuring that each character is equally likely. Codes are not sequential, so they cannot be enumerated simply by counting.

- **Collision handling.** While holding the lock, if a generated code already exists in `links` and belongs to a different URL, another code is generated. This is retried up to 10 times, after which `ErrCodeExhausted` is returned (HTTP 500). With `62^7 ≈ 3.5 × 10^12` possible codes, this is practically unreachable in normal operation; the behavior is tested using an injected code generator. Therefore, distinct URLs never share the same code.

- **Mutex type and locking strategy.** A single `sync.Mutex` is used. Both `Save` and `Get` acquire the lock. `Save` performs the lookup, code generation, and insertion within the same critical section, ensuring that concurrent requests for the same URL receive the same code. The use of `RWMutex` is deferred to Part 3, where it will be evaluated using benchmarks.

- **Validation / SSRF.** A URL must be non-empty and parseable, use either the `http` or `https` scheme, and contain a host. The server never fetches the long URL; it only stores it and redirects clients to it.

- **Package layout.** `cmd/server` contains flags and application wiring, `internal/store` contains the domain logic (normalization, code generation, and maps), and `internal/httpapi` contains the HTTP handlers and declares the `Store` interface consumed by the handlers.

## Part 2

- **`Store` interface location and methods.** Declared in `internal/httpapi`
  (the consumer), not in `internal/store`:

```go
  type Store interface {
      Save(longURL string) (code string, err error)
      Get(code string) (store.Link, error)
  }
```

`store.Link{URL, CreatedAt}` is the shared data type. `Save` returns only the
code; the handlers read `created_at` through `Get`. The in-memory `Memory`
satisfies it implicitly (checked at compile time in `fake_test.go`); tests use
a `fakeStore` with function fields to force any result, including errors the
real store never produces.

- **Sentinel errors.** `store.ErrNotFound`, `store.ErrInvalidURL` (plus
  `ErrCodeExhausted` for the practically unreachable collision case). The store
  wraps them with context using `%w`, e.g. `get "abc": code not found`.
- **How errors become status codes and bodies.** One function,
  `writeStoreError`, used by every route, switches with `errors.Is`:
    | Error                                                                        | Status | Body                                                                                             |
    | ---------------------------------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------ |
    | `ErrInvalidURL`                                                              | 400    | `{"error":"<store message>"}` (safe, tells the client what is wrong)                             |
    | `ErrNotFound`                                                                | 404    | `{"error":"not found"}` (fixed text, so internal wrapping and the requested code are not echoed) |
    | anything else                                                                | 500    | `{"error":"internal error"}`; the real error is only logged                                      |
    | Malformed JSON is a handler-level 400 (`invalid JSON body`) because it never |
    | reaches the store. All error bodies share the `{"error": "..."}` shape.      |
- **`created_at`.** Set once, when the code is first created, in UTC from an
  injectable clock (`Memory.now`) so tests are deterministic. Returned as
  RFC3339 (`2026-01-15T12:00:00Z`), formatted explicitly because Go's default
  `time.Time` JSON adds fractional seconds.
- **Idempotency after the refactor.** `byURL` is unchanged and a repeated
  `Save` returns the existing code without touching the stored `Link`, so
  `created_at` stays the original. Tested in the store (advancing clock) and
  over HTTP (`TestLinkInfoViaRealStoreAndIdempotency`).
- **Routing.** `GET /api/v1/links/{code}` and `GET /{code}` never conflict in
  mux (different segment counts), so the stdlib router is enough.

## Part 3

- **Locking choice: `sync.Mutex`** (one lock; `Save` and `Get` both take it;
  `Save` does lookup → generate → insert in one critical section, as in
  Part 1). I also built an `RWMutex` version (read lock for `Get` and for the
  `Save` lookup fast path, write lock plus a second lookup before creating a
  code) and benchmarked both on the same machine. It was correct (race tests
  passed) but not faster for a realistic workload, so I reverted to `Mutex`.
- **Measurements** (Intel i5-13450HX, 16 threads, Go 1.22, `-count=5`, median
  ns/op, lower is better, identical benchmarks run against both versions).
  Read-only and create paths:
    | Benchmark                                                                    |    Threads |              Mutex |            RWMutex | Result                            |
    | ---------------------------------------------------------------------------- | ---------: | -----------------: | -----------------: | --------------------------------- |
    | `Get`                                                                        |          1 |               14.4 |               15.5 | ~equal                            |
    | `Get`                                                                        |          4 |               30.9 |               48.2 | Mutex faster                      |
    | `Get`                                                                        |         16 |               53.4 |               38.8 | RWMutex 27% faster                |
    | `SaveExisting`                                                               |          4 |                109 |                 92 | RWMutex 15% faster                |
    | `SaveExisting`                                                               |         16 |                165 |               84.5 | RWMutex 49% faster                |
    | `SaveNew`                                                                    | 1 / 4 / 16 | 1954 / 1782 / 1811 | 2052 / 1870 / 1887 | equal (noise)                     |
    | Mixed read/create workloads (`BenchmarkMixed`, one create per N operations): |
    | Reads                                                                        |    Threads |              Mutex |            RWMutex | Result                            |
    | ------:                                                                      |  --------: |            ------: |          --------: | --------                          |
    | 90%                                                                          |          4 |                185 |                305 | Mutex faster (RW +65%)            |
    | 90%                                                                          |         16 |                191 |                292 | Mutex faster (RW +53%)            |
    | 99%                                                                          |          4 |               53.1 |               54.0 | tie                               |
    | 99%                                                                          |         16 |               63.2 |               77.0 | Mutex faster (RW +22%)            |
    | 99.9%                                                                        |          4 |               40.2 |               47.7 | Mutex faster (RW +19%)            |
    | 99.9%                                                                        |         16 |               65.7 |               69.9 | Mutex faster (RW +6%, near noise) |
    | Conclusion:                                                                  |
    - `RWMutex` only wins when the workload is **100% reads** and many cores run
      at once (`Get` and `SaveExisting` at 16 threads). As soon as even 1 in
      1,000 operations is a create, the advantage disappears and `Mutex` is
      equal or faster. A URL shortener always has some creates, so that
      pure-read case does not describe a running service.
    - A likely cause (not verified by profiling): a waiting writer makes new
      readers park and wake, which costs more than a plain `Mutex` queue; and
      `RWMutex.RLock` still updates a shared counter, so readers contend on a
      cache line anyway.
    - Caveat: `BenchmarkMixed` uses one shared atomic counter, which adds
      contention to both versions, so absolute numbers are inflated. The
      comparison between the two is still fair.
    - All differences are at most ~100 ns, against a full redirect of 1,100+ ns
      (see profile below), so end-to-end the choice is within a few percent.
      With equal performance, the simpler lock wins.
    - If the store ever became a measured bottleneck, the next steps would be
      sharding the maps by code or an `atomic.Pointer` to an immutable map for
      reads, not `RWMutex`.
- **Where the time actually goes (profile of the redirect benchmark).** A
  store `Get` is ~15–55 ns, while a full redirect through the handler is
  ~1.1–1.4 µs/op with 22 allocs/op. In the CPU profile (taken with the RWMutex build, which
  is the heavier lock), `store.(*Memory).Get` is 0.7% and `RWMutex.RLock` 1.3%,
  so lookup plus lock is about 2%, while `runtime.mallocgc` is 42%.
  `httptest.NewRequest` alone is 37%, which is benchmark harness overhead and
  not server code; `handleRedirect` (including `http.Redirect`) is 24%.
  About 18% is the Go runtime (`futex`, `procyield`, `lock2`): 16 goroutines
  contending on the allocator and GC because each request allocates ~6 KB.
  Lock choice therefore barely matters for end-to-end speed; allocation does.
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
    | `newServer` builds the server so a test can assert the timeouts are set and |
    | ordered sensibly.                                                           |
- **Eviction cap: none.** For a cache, evicting is harmless; for a URL
  shortener it is data loss: every place the short link was shared would start
  returning 404. Memory use grows ~0.5 KB per new link (upper bound, from
  `B/op` of `BenchmarkSaveNew`), so ~1M links is roughly 0.5 GB. Bounding
  memory is the job of the persistent store in Part 4, not of deleting links.
