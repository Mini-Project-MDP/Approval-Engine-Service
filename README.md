# Approval Engine Service

Go Fiber backend for the Approval Engine.

## Structure

```
cmd/api/         entrypoint (main.go)
internal/config/     env config loading
internal/database/   Turso (libsql) connection
internal/domain/     core entities, framework-independent
internal/handler/    HTTP handlers (Fiber ctx in/out)
internal/middleware/ cross-cutting Fiber middleware (cors, logger, recover)
internal/repository/ data-access layer (database/sql queries against Turso)
internal/router/     route registration
internal/service/    business logic, called by handlers
pkg/response/        shared JSON response envelope
```

Request flow for a new feature: `router` -> `handler` -> `service` -> `repository` -> `domain`.

## Run locally

```bash
cp .env.example .env
go run ./cmd/api
```

Server starts on `http://localhost:8000`. Health check: `GET /api/v1/health`.

The service runs without a database until `DATABASE_URL` (and `DATABASE_AUTH_TOKEN` for a remote Turso database) are set in `.env`. Never commit `.env` — it holds the live database credential.

Database: [Turso](https://turso.tech) (libsql), accessed via `database/sql` with the pure-Go [`libsql-client-go`](https://github.com/tursodatabase/libsql-client-go) driver — no cgo/C compiler required, so it builds the same on Windows/macOS/Linux.

Try it end to end against your own Turso database: `go run ./cmd/smoketest` seeds a demo application, a participant chain, and a 2-step workflow, then runs one request through approval to completion.

## Testing

```bash
go test ./...
```

Tests use [testify](https://github.com/stretchr/testify) (`require`/`assert`) against fakes (`internal/service/*_test.go`) — no database needed, runs in well under a second. They cover the resolver (including the cross-function hop), the condition evaluator, the full request state machine (approve/reject/all-vs-any/version pinning), and step validation.

## API docs (Swagger)

```bash
go run ./cmd/api
```

then open `http://localhost:8000/swagger/index.html`. The spec is generated from doc-comments on each handler via [swaggo/swag](https://github.com/swaggo/swag); after changing a handler's annotations, regenerate with:

```bash
go install github.com/swaggo/swag/cmd/swag@latest
swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal
```

`docs/` is committed so the Swagger UI works without anyone running `swag` first.

## API

See `/swagger/index.html` (above) for the interactive, authoritative reference — request/response schemas, try-it-out, everything. This table is just an index.

Auth is deliberately light for a 3-day MVP: only *creating* a request requires proof of which application is calling (`X-API-Key`). Everything else is reached by the centralized approver portal or a scanned document link and relies on unguessable UUIDs plus the engine's own "are you the assigned approver" check.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/requests` | `X-API-Key` | Start a request. Idempotent on `(app, doc_type, resource_id)`. |
| GET | `/api/v1/requests/:id` | — | Full request with steps + assignments. |
| POST | `/api/v1/requests/:id/decision` | — | `{"user_id","decision":"approved"|"rejected","comment"}`. |
| GET | `/api/v1/inbox/:userID` | — | Everything pending this approver, across every consuming app. |
| GET | `/api/v1/assignments/:id/qr` | — | PNG stamp for a printed document (see below). |
| GET | `/api/v1/verify/:id?sig=...` | HMAC in URL | Public: confirms the stamp is genuine and reports the assignment's live status. |
| POST | `/api/v1/applications` | — | Register a consuming app, returns its `api_key` **once**. |
| GET | `/api/v1/applications` | — | List apps (never includes `api_key`). |
| POST | `/api/v1/workflows` | — | Publish a workflow: create if `(app_id, doc_type)` is new, "edit" if it already exists — same call either way (see below). |
| GET | `/api/v1/workflows?app_id=` | — | List every version of every workflow. |
| GET | `/api/v1/workflows/:id` | — | One version with its steps. |
| POST | `/api/v1/workflows/:id/deactivate` | — | Stop a version from being used for new requests (never a hard delete). |

Workflow/application management has no role gate yet either — reachable by anyone who can reach the portal. Accepted gap for the 3-day demo scope, not an oversight; a real deployment needs an `is_admin` check on these routes.

### Editing a workflow = publishing a new version

There is no in-place edit. `POST /workflows` always inserts a brand new `workflow_definitions` row and atomically deactivates every other version of that `(app_id, doc_type)`, so exactly one version is ever active. This is what lets system-support add, change, or retire an approval flow **without a code change**, while an approval already in flight keeps running against the exact version it started on (it stored that `definition_id` at creation time) — a policy edit can never rewrite history out from under a pending request. Deactivating a version doesn't delete its row either, for the same reason: completed requests must stay readable.

Each step's `resolver_rule` and optional `condition` are validated at publish time (`internal/service/validate.go`) — an unknown resolver type, a missing field, a bad operator all fail the `POST` immediately with a clear message, instead of silently breaking an approver chain the first time a request actually reaches that step.

### Paper signature (QR stamp)

Asset management's next step after approval is still paper-based, so each assignment can produce a printable "signature" stamp: a QR code encoding a link back to `GET /verify/:id`, signed with an HMAC (`VERIFICATION_SECRET`) so the code can't be forged. This is **not** a certified/legally-binding e-signature (no PSrE, no non-repudiation) — it's a tamper-evident proof that an approval genuinely happened in this engine, for a downstream process that is still paper-based. The consuming app owns the printed page's layout (letterhead, form fields); it only ever asks the engine for the stamp image.
