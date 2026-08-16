<!-- Copyright 2026 Phillip Cloud -->
<!-- Licensed under the Apache License, Version 2.0 -->

# Local Web Interface

Design spec for a browser-based alternative to the TUI, running as a local
HTTP server against the same single-file SQLite database. Not a hosted
multi-user product -- that is a separate, much larger initiative built on
`internal/relay` and is out of scope here (see "Alternatives Considered").

## Motivation

The TUI is the primary interface and stays that way. But a local web UI
lets users browse and edit their home data from a phone or tablet on the
same LAN, from a machine without a nice terminal, or side-by-side with
other windows -- without standing up any new backend service or account
system. `internal/data.Store` already contains every CRUD operation,
validation rule, and soft-delete/restore flow the TUI uses; none of that
needs to be rewritten. What's missing is purely presentation.

## Non-Goals

- No multi-user auth, accounts, or household model. One database, whoever
  can reach the port can edit it -- same trust model as running the TUI on
  that machine.
- No React/Vue/build step. No new JS dependency footprint.
- No feature parity requirement with the TUI's power tools (chat overlay,
  fuzzy column finder, pin-and-filter, mag easter egg) in v1. Ship CRUD +
  dashboard + documents first; evaluate the rest after real usage.
- Does not touch `internal/relay`, `internal/sync`, or `internal/crypto`.
  Sync continues to work exactly as it does today; the web server just
  reads/writes the same local SQLite file the TUI does.

## Architecture

```
cmd/micasa/web.go            new `micasa web` subcommand
internal/webui/              new package
  server.go                  http.Server, mux, middleware, graceful shutdown
  handlers_<entity>.go       one file per entity, mirrors store_<entity>.go
  handlers_dashboard.go      dashboard page
  handlers_house.go          house profile page
  templates.go               html/template loading (embed.FS), shared layout
  templates/                 *.html templates (embedded via go:embed)
  static/                    CSS + minimal JS (embedded via go:embed)
```

`internal/webui` depends on `internal/data`, `internal/config`, and
`internal/locale` only -- never on `internal/app` (the Bubble Tea package).
The two UIs are siblings, not layered: `internal/app` renders `data.Store`
state to a terminal, `internal/webui` renders the same `data.Store` state
to HTML. Neither depends on the other.

### Server-rendered HTML + htmx, not an SPA

Every page is rendered server-side with `html/template`. Interactivity
(inline edit, delete confirm, form submit without full reload) uses htmx
attributes (`hx-get`, `hx-post`, `hx-target`, `hx-swap`) driving partial
template swaps, plus a small amount of hand-written vanilla JS for things
htmx doesn't cover (e.g. the date picker). No client-side router, no
client-side state, no JSON API layer to keep in sync with the DB schema.

This matches the project's existing bias against unnecessary configuration
and dependency surface, and it means every entity page is a thin wrapper:
parse form -> call `Store` method -> re-render template. The validation and
error messages the TUI already produces (`data/errors.go`: `hintError`,
`FieldError`) are reused as-is on the web forms.

**Alternative considered: JSON API + SPA.** Rejected for v1. A JSON API
would duplicate every validation/error-shaping rule already expressed in
`data/errors.go` and the `TabHandler` layer, doubles the surface to test,
and requires a JS build toolchain (npm, bundler, lockfile) this project has
none of. If a future native mobile app or third-party integration needs a
real API, it can be added later without touching the htmx server -- they
are not mutually exclusive, just sequenced.

htmx itself is a single vendored `.js` file (no npm, no build step) --
add it as a static asset under `internal/webui/static/`, checked into the
repo like any other vendored asset, not fetched from a CDN at request time
(this project already treats third-party network calls as opt-in; the
Mermaid-CDN exception in the docs site does not apply here since this is
the app binary, not the public docs site).

### Command & lifecycle

```
micasa web [database-path]
  --addr string       listen address (default "127.0.0.1:8734")
  --open               open the default browser after starting (default true)
```

Binds to `127.0.0.1` by default -- not `0.0.0.0` -- so "reachable from a
phone on the LAN" is an explicit opt-in (`--addr 0.0.0.0:8734`), not a
surprise. This mirrors `runOpts`/`demoOpts`/`backupOpts` conventions in
`cmd/micasa/main.go`: a small opts struct, flags bound in `newWebCmd()`,
`RunE` resolves the DB path via the same `data.ResolveDBPath` helper the
TUI and `demo`/`backup` commands already use.

The server opens the *same* `data.Store` construction path the TUI uses
(`data.Open` + `AutoMigrate`), holding a single `*gorm.DB` behind
`Store`'s existing mutex/transaction semantics -- no second SQLite
connection pool with different locking behavior. Only one of `micasa`
(TUI) or `micasa web` should run against a given DB file at a time in
practice (SQLite's file locking will serialize them correctly if both are
open, but concurrent writers from two UIs is not a tested configuration
for v1).

### Routing

Following the relay's `net/http` + `http.ServeMux` pattern (Go 1.22+
method+path patterns, no router dependency):

```go
mux.HandleFunc("GET /", h.handleDashboard)
mux.HandleFunc("GET /house", h.handleHouseView)
mux.HandleFunc("GET /house/edit", h.handleHouseEditForm)
mux.HandleFunc("POST /house", h.handleHouseSubmit)

mux.HandleFunc("GET /vendors", h.handleVendorList)
mux.HandleFunc("GET /vendors/new", h.handleVendorNewForm)
mux.HandleFunc("POST /vendors", h.handleVendorCreate)
mux.HandleFunc("GET /vendors/{id}/edit", h.handleVendorEditForm)
mux.HandleFunc("POST /vendors/{id}", h.handleVendorUpdate)
mux.HandleFunc("POST /vendors/{id}/delete", h.handleVendorDelete)
mux.HandleFunc("POST /vendors/{id}/restore", h.handleVendorRestore)
// ... same eight handlers, repeated per entity
```

Same shape for `projects`, `quotes`, `maintenance`, `incidents`,
`appliances`, `service-logs`, `documents` (the same eight entities listed
in `plans/920-cli-entity-crud.md`'s inventory, which already enumerates
soft-deletable vs. singleton entities and can be reused directly instead
of re-deriving it here).

### Documents

`Document` rows store BLOBs directly (`data/models.go`). The web UI serves
them via `GET /documents/{id}/download` (streams the BLOB with the stored
MIME type) and accepts uploads via a standard multipart form POST --
simpler than the TUI's file-picker overlay (`form_filepicker.go`), since
the browser's native `<input type=file>` already handles selection.

### Dashboard

`internal/data/dashboard.go` already exposes the queries the TUI's
dashboard overlay (`dashboard.go`) uses (overdue, upcoming, spending). The
web dashboard page calls the same `Store` methods and renders them as the
landing page (`GET /`) instead of an overlay.

### What's explicitly deferred past v1

- Chat / NL-to-SQL (`chat.go` equivalent) -- LLM is opt-in and this adds
  real complexity (streaming to a page); revisit once CRUD is proven out.
- Document extraction pipeline UI (`extraction.go` equivalent) -- same
  reasoning; the CLI (`micasa <entity> add --data`) already covers
  scripted extraction workflows per `920-cli-entity-crud.md`.
- Pin-and-filter, multi-column sort, fuzzy column finder -- table lists
  start with simple sort-by-column-header + basic text search (SQLite
  FTS5, already available via `data/fts.go`) and grow from there based on
  actual usage.
- Undo/redo (`undo.go` equivalent) -- soft-delete + restore already
  provides a safety net; a full undo stack for the web UI is a v2 question.

## Testing

Per this repo's testing rules, tests must drive behavior through real user
interaction, not internal API calls. For a web server "real user
interaction" means HTTP requests through `httptest.Server` (or
`httptest.NewRecorder` against the mux) exercising full request/response
cycles -- GET a form, POST it, assert the resulting redirect/HTML reflects
the change in `Store` -- mirroring how `sendKey`/`openAddForm` drive the
TUI tests today rather than calling `Store` methods directly from the web
package's tests. Every entity's create/edit/delete/restore path needs at
least one such test, plus error-path tests for validation failures
(duplicate vendor name, missing required field, FK-in-use delete
rejection) surfaced as rendered error HTML, matching `hintError`/
`FieldError` messages.

## Rollout

1. Skeleton: `internal/webui` package, `micasa web` command, dashboard +
   house profile pages only (proves the server/template/htmx plumbing).
2. One full entity (Vendor -- simplest, no FK dependents) end-to-end:
   list, add, edit, delete, restore, tested per above.
3. Remaining seven entities, same pattern, mechanical once (2) is settled.
4. Document upload/download.
5. Polish pass: styling (reuse the Wong-palette color intent from
   `styles.go`, translated to CSS custom properties -- not a literal port,
   since lipgloss `AdaptiveColor` and CSS light/dark media queries are
   different mechanisms), mobile layout, `/record-demo` + `/capture-ui`
   captures for the PR.

Each step is independently shippable and reviewable as its own PR; no step
depends on later ones being designed up front.

## Alternatives Considered

### Wrap the TUI in a browser terminal (wetty/ttyd/xterm.js style)

Fastest possible "web access" -- zero new UI code, just pipe the existing
Bubble Tea program's stdio through a websocket-backed terminal emulator in
the browser. Rejected as the primary path: it's still a terminal (tiny
touch targets, no native mobile keyboard affordances, no copy/paste
parity, mouse zones tuned for a real terminal's click semantics don't
translate cleanly through a websocket relay), so it doesn't actually solve
"use this comfortably from a phone." Worth keeping in mind as a near-zero-
effort fallback if the htmx build stalls, but not the target design.

### Full hosted multi-user web app on `internal/relay`

`internal/relay` already has Postgres, row-level security, and a
household/device model, but it's a zero-knowledge sync backend by design
-- the relay never sees plaintext (`internal/crypto`: NaCl secretbox/box,
`HouseholdKey` never leaves devices). A browser client on this path is not
"add a web frontend to the relay"; it's "make the browser a new class of
sync device," which means porting `internal/crypto`'s key generation/
box-seal/secretbox logic to WebCrypto or a WASM build of the same Go code,
plus real session/auth handling the relay currently delegates entirely to
bearer device tokens issued during pairing. Substantially larger scope,
orthogonal to this doc, and only worth pursuing if the goal is genuine
multi-user cloud access rather than a nicer local UI. Tracked as a future,
separate design doc if/when there's a concrete need.
