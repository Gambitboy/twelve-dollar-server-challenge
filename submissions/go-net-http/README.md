# go-net-http (reference)

The Go + SQLite implementation from the video, left as a competent developer would ship it on day one.
**Nothing is optimized on purpose.**

| | |
|---|---|
| Language | Go 1.27.1 |
| Framework | standard library `net/http` (Go 1.22+ routing), `encoding/json` |
| SQLite | `database/sql` + `github.com/mattn/go-sqlite3` v1.14.52 (cgo, bundled SQLite) |
| JWT | `github.com/golang-jwt/jwt/v5` v5.3.1 |

## Setup

- One process with the default `GOMAXPROCS` and GC.
- One `database/sql` pool of 10 connections. Each connection runs `journal_mode=WAL`, `busy_timeout=5000`,
  `synchronous=NORMAL` and `foreign_keys=on`.
- One SQL statement per request, the queries shown in SPEC.md. A like on a missing post is detected from the
  foreign-key error (SQLite extended code 787), not from an extra query.
- `IdleTimeout` is 65 s, so connections outlive Nginx's keep-alive.

## Ideas it leaves on the table

Prepared statement reuse, a separate single writer connection, group commit, a faster JSON encoder or router,
`mmap_size`, `cache_size`, …
