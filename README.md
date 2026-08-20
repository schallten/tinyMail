# tinyMail

A mail server written in Go. Implements [SMTP](https://datatracker.ietf.org/doc/html/rfc5321) for receiving mail and [POP3](https://datatracker.ietf.org/doc/html/rfc1939) for retrieving it. Messages are stored on the local filesystem. No database, no external dependencies.

## Build and Run

```
go build -o mailserver ./cmd/server
./mailserver
```

Default ports:
- SMTP: 2525
- POP3: 1100

Override with flags:

```
./mailserver -smtp-port 25 -pop3-port 110 -storage /path/to/storage
```

## LAN Usage

The server binds to all interfaces (`0.0.0.0`) by default. Other machines on the same network can connect using the host's IP address. For example, if the server runs on `192.168.1.50`, configure clients to use that IP with the same ports.

## Connecting a Client

Configure an email client (e.g., Thunderbird) with:

**SMTP (sending):**
- Server: `localhost`
- Port: `2525`
- Connection security: None
- Authentication: Normal password

**POP3 (receiving):**
- Server: `localhost`
- Port: `1100`
- Connection security: None
- Authentication: Normal password

## Users

Credentials are stored in `storage/.config/users.db` as `username:password` per line.

Pre-configured users:
- `alice` / `secret123`
- `bob` / `password`

## Storage

Messages are stored per-user under `storage/<username>/inbox/` as `.eml` files. Writes are atomic (tmp → fsync → rename), so partial messages are not committed on crash.

## Project Structure

```
cmd/server/main.go          Entry point, listener setup, graceful shutdown
internal/smtp/session.go    SMTP protocol engine (EHLO, MAIL, RCPT, DATA, etc.)
internal/pop3/session.go    POP3 protocol engine (USER, PASS, LIST, RETR, DELE, etc.)
internal/storage/backend.go Storage interface
internal/storage/posix.go   Filesystem storage implementation
internal/log/logger.go      Structured logging
storage/                    Runtime data (user mailboxes, credentials)
```

## Supported Commands

**SMTP:** EHLO, HELO, MAIL FROM, RCPT TO, DATA, RSET, NOOP, QUIT

**POP3:** USER, PASS, STAT, LIST, RETR, DELE, RSET, NOOP, QUIT
