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
