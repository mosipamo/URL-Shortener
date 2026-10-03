# URL Shortener

In-memory URL shortener, Go 1.22+, stdlib only. (Currently: Part 1.)

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

## Verify

    gofmt -l . && go vet ./... && go test -race ./...
    go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

Design choices are in [DECISIONS.md](DECISIONS.md).
