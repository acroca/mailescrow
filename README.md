# mailescrow

A human-in-the-loop email proxy for AI agents. Services submit and receive emails through a REST API; every message is held for human approval before anything is sent or delivered. mailescrow handles SMTP relay (outbound) and IMAP polling (inbound) against your upstream provider — your agent never touches email infrastructure directly.

## Why this exists

I wanted an AI agent ([OpenClaw](https://github.com/albert/openclaw)) to reach out to a restaurant on my behalf. Simple enough — but I wasn't comfortable giving the agent direct access to send or receive email.

The risk isn't hypothetical. An agent with unrestricted outbound email could send spam, contact people I never intended, or behave unpredictably. And inbound isn't safe either: an agent that can receive arbitrary email could get tricked into acting on instructions smuggled in through a reply.

mailescrow sits in between. The agent talks to it via a REST API, but every message — in both directions — is held for my approval before anything happens. I see exactly what the agent is trying to send and what has arrived for it, decide whether it's reasonable, and only then does it proceed. Everything that crosses the boundary passes through a human first.

## Architecture

```
Outbound: Service → POST /api/emails → pending → human approves → SMTP relay
Inbound:  IMAP poll → pending → human approves → GET /api/emails → Service
```

IMAP folder lifecycle for inbound messages:

| Step          | Folder                  |
|---------------|-------------------------|
| On fetch      | `INBOX` → `mailescrow/received`   |
| On approve    | `mailescrow/received` → `mailescrow/approved` |
| On reject     | `mailescrow/received` → `mailescrow/rejected` |
| On service read | `mailescrow/approved` → `mailescrow/read` |

## Features

- **REST API** — services submit outbound mail and poll for approved inbound mail
- **IMAP polling** — fetches inbound messages from your mailbox on a configurable interval
- **Web UI** — inspect and approve or reject pending emails in a browser
- **SMTP relay** — approved outbound emails are forwarded via a configurable upstream SMTP server
- **SQLite storage** — pending emails held locally; deleted after action (no historical data)
- **Two separate ports** — web UI and REST API run on independent listeners

## Quickstart

### Build from source

```bash
go build -o mailescrow ./cmd/mailescrow
```

### Run

```bash
# Minimal: outbound only (no IMAP configured)
MAILESCROW_RELAY_HOST=smtp.example.com \
MAILESCROW_RELAY_PORT=465 \
MAILESCROW_RELAY_USERNAME=user \
MAILESCROW_RELAY_PASSWORD=pass \
MAILESCROW_RELAY_TLS=true \
./mailescrow

# Full: outbound + inbound IMAP polling
MAILESCROW_IMAP_HOST=imap.example.com \
MAILESCROW_IMAP_USERNAME=user \
MAILESCROW_IMAP_PASSWORD=pass \
MAILESCROW_RELAY_HOST=smtp.example.com \
MAILESCROW_RELAY_PORT=465 \
MAILESCROW_RELAY_USERNAME=user \
MAILESCROW_RELAY_PASSWORD=pass \
MAILESCROW_RELAY_TLS=true \
./mailescrow

# Or via a config file
./mailescrow --config config.yaml
```

The service starts two listeners:
- **Web UI** on `:8080` — approve/reject pending emails
- **REST API** on `:8081` — used by your service to submit and receive emails

## REST API

### Submit outbound email

```
POST http://localhost:8081/api/emails
Content-Type: application/json

{
  "from": "agent@example.com",
  "to": ["recipient@example.com"],
  "subject": "Reservation enquiry",
  "body": "Hi, do you have a table for two on Friday?"
}
```

Response `201 Created`:
```json
{"id": "550e8400-e29b-41d4-a716-446655440000"}
```

The email appears in the web UI as **outbound pending**. Approving it sends it via SMTP relay; rejecting it discards it.

### Receive approved inbound emails

```
GET http://localhost:8081/api/emails
```

Response `200 OK`:
```json
[
  {
    "id": "...",
    "from": "restaurant@example.com",
    "to": ["agent@example.com"],
    "subject": "Re: Reservation enquiry",
    "body": "Yes, we have availability...",
    "received_at": "2026-02-20T10:00:00Z"
  }
]
```

Each call **consumes** the returned emails — they are deleted from the database after being returned (and moved to `mailescrow/read` in IMAP). Returns `[]` when nothing is waiting.

## Docker

```bash
docker build -t mailescrow .
docker run -p 8080:8080 -p 8081:8081 \
  -e MAILESCROW_IMAP_HOST=imap.example.com \
  -e MAILESCROW_IMAP_USERNAME=user \
  -e MAILESCROW_IMAP_PASSWORD=pass \
  -e MAILESCROW_RELAY_HOST=smtp.example.com \
  -e MAILESCROW_RELAY_PORT=465 \
  -e MAILESCROW_RELAY_USERNAME=user \
  -e MAILESCROW_RELAY_PASSWORD=pass \
  -e MAILESCROW_RELAY_TLS=true \
  mailescrow
```

## Configuration Reference

Environment variables take precedence over config file values. A config file is optional.

### IMAP (inbound polling)

| Environment variable              | Config key                | Default  | Description                              |
|-----------------------------------|---------------------------|----------|------------------------------------------|
| `MAILESCROW_IMAP_HOST`            | `imap.host`               | —        | IMAP server hostname                     |
| `MAILESCROW_IMAP_PORT`            | `imap.port`               | `993`    | IMAP server port                         |
| `MAILESCROW_IMAP_USERNAME`        | `imap.username`           | —        | IMAP username                            |
| `MAILESCROW_IMAP_PASSWORD`        | `imap.password`           | —        | IMAP password                            |
| `MAILESCROW_IMAP_TLS`             | `imap.tls`                | `true`   | Use implicit TLS                         |
| `MAILESCROW_IMAP_POLL_INTERVAL`   | `imap.poll_interval`      | `60s`    | How often to check for new messages      |

If `imap.host` is empty, inbound polling is disabled.

### Relay (outbound SMTP)

| Environment variable              | Config key                | Default  | Description                              |
|-----------------------------------|---------------------------|----------|------------------------------------------|
| `MAILESCROW_RELAY_HOST`           | `relay.host`              | —        | Upstream SMTP host                       |
| `MAILESCROW_RELAY_PORT`           | `relay.port`              | `587`    | Upstream SMTP port                       |
| `MAILESCROW_RELAY_USERNAME`       | `relay.username`          | —        | Upstream SMTP username                   |
| `MAILESCROW_RELAY_PASSWORD`       | `relay.password`          | —        | Upstream SMTP password                   |
| `MAILESCROW_RELAY_TLS`            | `relay.tls`               | `false`  | Use implicit TLS (e.g. port 465)         |

### Web / API

| Environment variable              | Config key                | Default          | Description                              |
|-----------------------------------|---------------------------|------------------|------------------------------------------|
| `MAILESCROW_WEB_LISTEN`           | `web.listen`              | `:8080`          | Web UI listen address                    |
| `MAILESCROW_API_LISTEN`           | `web.api_listen`          | `:8081`          | REST API listen address                  |
| `MAILESCROW_DB_PATH`              | `db.path`                 | `mailescrow.db`  | SQLite database path                     |

### Config file (optional)

```yaml
imap:
  host: "imap.example.com"
  port: 993
  username: "user@example.com"
  password: "secret"
  tls: true
  poll_interval: "60s"

relay:
  host: "smtp.example.com"
  port: 465
  username: "user@example.com"
  password: "secret"
  tls: true

web:
  listen: ":8080"
  api_listen: ":8081"

db:
  path: "mailescrow.db"
```

## License

MIT — see [LICENSE](LICENSE).
