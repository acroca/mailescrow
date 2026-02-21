# mailescrow

[![CI](https://github.com/acroca/mailescrow/actions/workflows/ci.yml/badge.svg)](https://github.com/acroca/mailescrow/actions/workflows/ci.yml)

**A human approval gate for AI agent email.** Your agent submits and receives mail through a REST API. Every message, outbound and inbound, is held until you say so.

```
Agent  →  POST /api/emails  →  [ pending ]  →  you approve  →  sent via SMTP
IMAP   →  poll your inbox   →  [ pending ]  →  you approve  →  GET /api/emails  →  Agent
```

## Why this exists

I wanted an AI agent ([OpenClaw](https://github.com/albert/openclaw)) to reach out to a restaurant on my behalf. Simple enough, but I wasn't comfortable giving the agent direct email access.

The risk isn't hypothetical. An agent with unrestricted outbound email could send spam, contact people I never intended, or behave unpredictably under adversarial conditions. And inbound isn't safe either: a reply could smuggle instructions to the agent that hijack its behaviour.

mailescrow sits in the middle. The agent talks to it over a local REST API, but nothing crosses the wire until I've read it and clicked approve. Every message passes through a human first. That's the whole point.

## How it works

mailescrow runs two local servers:

- **Web UI** on `:8080`: shows pending emails; click to approve or reject
- **REST API** on `:8081`: your agent's only interface to email

**Outbound:** the agent POSTs a message → it appears in the web UI → you approve → mailescrow relays it via SMTP.

**Inbound:** mailescrow polls your IMAP inbox → new messages appear in the web UI → you approve → the agent fetches them via GET.

IMAP folders track each message through its lifecycle:

| Stage          | Folder                        |
|----------------|-------------------------------|
| Fetched        | `INBOX` → `mailescrow/received`   |
| Approved       | `mailescrow/received` → `mailescrow/approved` |
| Rejected       | `mailescrow/received` → `mailescrow/rejected` |
| Read by agent  | `mailescrow/approved` → `mailescrow/read` |

Messages are deleted from the local database after each action. mailescrow keeps no history.

## Quickstart

### Build

```bash
go build -o mailescrow ./cmd/mailescrow
```

### Run

```bash
# Outbound only (no IMAP)
MAILESCROW_RELAY_HOST=smtp.example.com \
MAILESCROW_RELAY_PORT=465 \
MAILESCROW_RELAY_USERNAME=you@example.com \
MAILESCROW_RELAY_PASSWORD=secret \
MAILESCROW_RELAY_TLS=true \
./mailescrow

# Outbound + inbound polling
MAILESCROW_IMAP_HOST=imap.example.com \
MAILESCROW_IMAP_USERNAME=you@example.com \
MAILESCROW_IMAP_PASSWORD=secret \
MAILESCROW_RELAY_HOST=smtp.example.com \
MAILESCROW_RELAY_PORT=465 \
MAILESCROW_RELAY_USERNAME=you@example.com \
MAILESCROW_RELAY_PASSWORD=secret \
MAILESCROW_RELAY_TLS=true \
./mailescrow

# Via config file
./mailescrow --config config.yaml
```

### Docker Compose

```yaml
services:
  mailescrow:
    image: ghcr.io/acroca/mailescrow:latest
    ports:
      - "8080:8080"
      - "8081:8081"
    environment:
      MAILESCROW_IMAP_HOST: imap.example.com
      MAILESCROW_IMAP_USERNAME: you@example.com
      MAILESCROW_IMAP_PASSWORD: secret
      MAILESCROW_RELAY_HOST: smtp.example.com
      MAILESCROW_RELAY_PORT: 465
      MAILESCROW_RELAY_USERNAME: you@example.com
      MAILESCROW_RELAY_PASSWORD: secret
      MAILESCROW_RELAY_TLS: "true"
      MAILESCROW_DB_PATH: /data/mailescrow.db
    volumes:
      - mailescrow-data:/data
    restart: unless-stopped

volumes:
  mailescrow-data:
```

### Docker

```bash
docker build -t mailescrow .
docker run -p 8080:8080 -p 8081:8081 \
  -e MAILESCROW_IMAP_HOST=imap.example.com \
  -e MAILESCROW_IMAP_USERNAME=you@example.com \
  -e MAILESCROW_IMAP_PASSWORD=secret \
  -e MAILESCROW_RELAY_HOST=smtp.example.com \
  -e MAILESCROW_RELAY_PORT=465 \
  -e MAILESCROW_RELAY_USERNAME=you@example.com \
  -e MAILESCROW_RELAY_PASSWORD=secret \
  -e MAILESCROW_RELAY_TLS=true \
  mailescrow
```

## REST API

All requests are unauthenticated JSON. The API runs on `:8081` by default.

### Send an email

```
POST /api/emails
```

```json
{
  "to": ["recipient@example.com"],
  "subject": "Reservation enquiry",
  "body": "Hi, do you have a table for two on Friday?"
}
```

`to` and `subject` are required. The sender address is always `relay.username` (display name configurable via `relay.from_name`).

```json
201 Created

{"id": "550e8400-e29b-41d4-a716-446655440000"}
```

The email is now pending in the web UI. Nothing is sent until you approve it.

### Check the approval queue

```
GET /api/emails/pending/count
```

```json
200 OK

{"count": 3}
```

Read-only. Safe to poll. Use this to wait for a human to review your outbound message before sending another, or to signal that attention is needed.

### Receive approved inbound emails

```
GET /api/emails
```

```json
200 OK

[
  {
    "id": "...",
    "from": "restaurant@example.com",
    "to": ["agent@example.com"],
    "subject": "Re: Reservation enquiry",
    "body": "Yes, we have availability on Friday.",
    "received_at": "2026-02-20T10:00:00Z"
  }
]
```

**This call is destructive.** Emails are deleted from the database after being returned. Returns `[]` when nothing is waiting.

### Agent skill file

`skill.md` at the project root documents the full API in [skill.md format](https://www.mintlify.com/blog/skill-md). Drop its contents into your agent's system prompt so it knows how to use mailescrow.

## Configuration

Environment variables take precedence over config file values.

### IMAP (inbound polling)

| Environment variable            | Config key              | Default | Description                         |
|---------------------------------|-------------------------|---------|-------------------------------------|
| `MAILESCROW_IMAP_HOST`          | `imap.host`             | —       | IMAP server hostname                |
| `MAILESCROW_IMAP_PORT`          | `imap.port`             | `993`   | IMAP server port                    |
| `MAILESCROW_IMAP_USERNAME`      | `imap.username`         | —       | IMAP username                       |
| `MAILESCROW_IMAP_PASSWORD`      | `imap.password`         | —       | IMAP password                       |
| `MAILESCROW_IMAP_TLS`           | `imap.tls`              | `true`  | Use implicit TLS                    |
| `MAILESCROW_IMAP_POLL_INTERVAL` | `imap.poll_interval`    | `60s`   | How often to check for new messages |

Leave `imap.host` empty to disable inbound polling entirely.

### Relay (outbound SMTP)

| Environment variable          | Config key          | Default | Description                          |
|-------------------------------|---------------------|---------|--------------------------------------|
| `MAILESCROW_RELAY_HOST`       | `relay.host`        | —       | Upstream SMTP host                   |
| `MAILESCROW_RELAY_PORT`       | `relay.port`        | `587`   | Upstream SMTP port                   |
| `MAILESCROW_RELAY_USERNAME`   | `relay.username`    | —       | SMTP username; used as sender address |
| `MAILESCROW_RELAY_PASSWORD`   | `relay.password`    | —       | SMTP password                        |
| `MAILESCROW_RELAY_TLS`        | `relay.tls`         | `false` | Use implicit TLS (port 465)          |
| `MAILESCROW_RELAY_FROM_NAME`  | `relay.from_name`   | —       | Display name for outbound From header |

### Web / API

| Environment variable        | Config key        | Default         | Description                                      |
|-----------------------------|-------------------|-----------------|--------------------------------------------------|
| `MAILESCROW_WEB_LISTEN`     | `web.listen`      | `:8080`         | Web UI listen address                            |
| `MAILESCROW_API_LISTEN`     | `web.api_listen`  | `:8081`         | API listen address                               |
| `MAILESCROW_WEB_PASSWORD`   | `web.password`    | —               | Password for web UI HTTP Basic Auth (recommended) |
| `MAILESCROW_DB_PATH`        | `db.path`         | `mailescrow.db` | SQLite database path                             |

If `web.password` is set, browsers are prompted for credentials before any web UI page loads. The REST API on `:8081` is never gated — agents authenticate via network isolation, not passwords.

### Config file

```yaml
imap:
  host: "imap.example.com"
  port: 993
  username: "you@example.com"
  password: "secret"
  tls: true
  poll_interval: "60s"

relay:
  host: "smtp.example.com"
  port: 465
  username: "you@example.com"
  password: "secret"
  tls: true
  from_name: "My Agent"  # emails sent as: "My Agent" <you@example.com>

web:
  listen: ":8080"
  api_listen: ":8081"
  password: "your-password"  # protects the web UI with HTTP Basic Auth

db:
  path: "mailescrow.db"
```

## License

MIT. See [LICENSE](LICENSE).
