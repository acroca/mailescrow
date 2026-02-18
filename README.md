# mailescrow

An open-source SMTP proxy with human-in-the-loop approval. Emails are received by an SMTP server, held in SQLite, then approved or rejected via a web UI. Approved emails are forwarded upstream; rejected emails are discarded. No historical data is retained.

## Why this exists

I wanted an AI agent ([OpenClaw](https://github.com/albert/openclaw)) to reach out to a restaurant on my behalf to ask about a reservation. Simple enough — but I wasn't comfortable giving the agent direct access to send email.

The risk isn't hypothetical. An agent with unrestricted outbound email could send spam, contact people I never intended, or behave in ways that are hard to predict or reverse. And inbound isn't safe either: an agent that can receive arbitrary email could get tricked into signing up for services, leaking private information, or acting on instructions smuggled in through a reply.

mailescrow sits in between. The agent talks to it like a normal SMTP server, but every message is held for my approval before anything leaves. I see exactly what the agent is trying to send, decide whether it's reasonable, and only then does it go out. Everything that crosses the boundary — in either direction — passes through a human first.

## Features

- **SMTP server** with authentication — receives and holds outgoing mail
- **SQLite storage** — pending emails are stored locally, deleted after action
- **Web UI** — list, inspect, approve, or reject pending emails in a browser
- **Upstream relay** — approved emails are forwarded via a configurable SMTP server with TLS support

## Quickstart

### Build from source

```bash
go build -o mailescrow ./cmd/mailescrow
```

### Run

```bash
# Via environment variables (recommended)
MAILESCROW_SMTP_USERNAME=mailescrow \
MAILESCROW_SMTP_PASSWORD=changeme \
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
- **SMTP** on `:2525` (receives mail)
- **Web UI** on `:8080` (approve/reject pending emails)

Open `http://localhost:8080` in a browser to review pending emails.

## Docker

```bash
docker build -t mailescrow .
docker run -p 2525:2525 -p 8080:8080 \
  -e MAILESCROW_SMTP_USERNAME=mailescrow \
  -e MAILESCROW_SMTP_PASSWORD=changeme \
  -e MAILESCROW_RELAY_HOST=smtp.example.com \
  -e MAILESCROW_RELAY_PORT=465 \
  -e MAILESCROW_RELAY_USERNAME=user \
  -e MAILESCROW_RELAY_PASSWORD=pass \
  -e MAILESCROW_RELAY_TLS=true \
  mailescrow
```

## Configuration Reference

Environment variables take precedence over config file values. A config file is optional.

| Environment variable          | Config file key    | Default          | Description                        |
|-------------------------------|--------------------|------------------|------------------------------------|
| `MAILESCROW_SMTP_LISTEN`      | `smtp.listen`      | `:2525`          | SMTP listen address                |
| `MAILESCROW_SMTP_USERNAME`    | `smtp.username`    | —                | SMTP AUTH username                 |
| `MAILESCROW_SMTP_PASSWORD`    | `smtp.password`    | —                | SMTP AUTH password                 |
| `MAILESCROW_RELAY_HOST`       | `relay.host`       | —                | Upstream SMTP host                 |
| `MAILESCROW_RELAY_PORT`       | `relay.port`       | `587`            | Upstream SMTP port                 |
| `MAILESCROW_RELAY_USERNAME`   | `relay.username`   | —                | Upstream SMTP username             |
| `MAILESCROW_RELAY_PASSWORD`   | `relay.password`   | —                | Upstream SMTP password             |
| `MAILESCROW_RELAY_TLS`        | `relay.tls`        | `false`          | Use implicit TLS (e.g. port 465)   |
| `MAILESCROW_WEB_LISTEN`       | `web.listen`       | `:8080`          | Web UI listen address              |
| `MAILESCROW_DB_PATH`          | `db.path`          | `mailescrow.db`  | SQLite database path               |

### Config file (optional)

```yaml
smtp:
  listen: ":2525"
  username: "mailescrow"
  password: "changeme"

relay:
  host: "smtp.example.com"
  port: 465
  username: "user"
  password: "pass"
  tls: true

web:
  listen: ":8080"

db:
  path: "mailescrow.db"
```

## License

MIT — see [LICENSE](LICENSE).
