# mailescrow

## Build

```bash
go build ./...                          # Build all packages
go build -o mailescrow ./cmd/mailescrow # Build service binary
```

## Test

```bash
go test ./...
go vet ./...
go tool golangci-lint run ./...
```

## Project Layout

- `cmd/mailescrow/` — Service binary (SMTP server + web UI)
- `internal/config/` — YAML config loading
- `internal/smtp/` — SMTP server (receives mail, stores in SQLite)
- `internal/relay/` — Upstream SMTP relay (forwards approved mail)
- `internal/store/` — SQLite storage layer
- `internal/web/` — HTTP web UI server
- `internal/web/templates/` — HTML templates (embedded into binary via `//go:embed`)

## Conventions

- Go 1.26+
- Pure Go SQLite via `modernc.org/sqlite` (no CGO)
- HTTP web UI for email approval (no CLI, no gRPC)
- Emails are deleted from the database after approve/reject — no historical data
