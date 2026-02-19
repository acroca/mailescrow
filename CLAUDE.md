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

- `cmd/mailescrow/` — Service binary; starts web UI + API servers + IMAP poller
- `internal/config/` — YAML config loading (IMAP, relay, web/API ports, DB path)
- `internal/imap/` — IMAP client: `EnsureFolders`, `Poll`, `MoveMessage`
- `internal/relay/` — Upstream SMTP relay (forwards approved outbound mail)
- `internal/store/` — SQLite storage layer (direction, status, IMAP metadata)
- `internal/web/` — Two HTTP servers: web UI (`:8080`) and REST API (`:8081`)
- `internal/web/templates/` — HTML templates (embedded via `//go:embed`)
- `integration/` — End-to-end tests (no real IMAP; IMAP ops skipped via nil client)

## Architecture

```
Outbound: Service → POST /api/emails → pending in DB → human approves (web UI) → SMTP relay
Inbound:  IMAP poll → pending in DB → human approves (web UI) → GET /api/emails → Service
```

IMAP folder lifecycle: `INBOX` → `mailescrow/received` → `mailescrow/approved|rejected` → `mailescrow/read`

## Conventions

- Go 1.26+
- Pure Go SQLite via `modernc.org/sqlite` (no CGO)
- Web UI (`:8080`) and REST API (`:8081`) run on **separate ports** — keep them split
- `web.IMAPMover` interface decouples the web server from `internal/imap`; pass `nil` in tests
- Emails are deleted from the database after approve/reject/consume — no historical data
- `store.EmailStore` interface: use `SaveOutbound`/`SaveInbound`, `ListPending`/`ListApproved`, `Approve`, `UpdateIMAPMailbox`, `Delete`
- Config env vars: `MAILESCROW_IMAP_*`, `MAILESCROW_RELAY_*`, `MAILESCROW_WEB_LISTEN`, `MAILESCROW_API_LISTEN`, `MAILESCROW_DB_PATH`

## Agent checklist

When making any non-trivial change, work through this list before finishing:

1. **Tests** — update or add tests for every changed package; run `go test ./...` and confirm it passes
2. **Build** — run `go build ./...` and `go vet ./...` with no errors
3. **Config** — if adding config fields, update `config.go` defaults, `applyEnv`, `config_test.go`, `config.example.yaml`, and the README configuration table
4. **README.md** — update architecture description, port list, API examples, and configuration reference as needed
5. **CLAUDE.md** — keep the project layout, architecture summary, and conventions sections current
6. **Integration tests** — `integration/integration_test.go` covers end-to-end flows; update when API surface or behaviour changes
