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
