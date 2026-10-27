# Repository Guidelines

## Project Structure

- `cmd/m365/`: CLI entrypoint (`main.go`).
- `internal/cli/`: command wiring (one file per domain: `mail`, `calendar`,
  `contacts`, `drive`, `sp`, `doctor`). Thin glue — no business logic; commands
  talk only to the `internal/backend` seam, never to a transport directly.
- `internal/backend/`: the transport-agnostic seam — per-domain service
  interfaces (`MailService`, `CalendarService`, ...) the CLI depends on. The
  `backend` config field selects an implementation (`graph` default, `ews` for
  on-premise Exchange). Unimplemented operations return `backend.ErrUnsupported`.
- `internal/backend/graph/`: the Microsoft Graph implementation of the seam
  (REST suffixes, `$select`, the `{value}` unwrap) — composes `internal/graph`.
- `internal/backend/ews/`: the on-premise Exchange (EWS) implementation —
  composes `internal/ews` and maps EWS items to the same Graph-shaped JSON the
  graph backend emits. Covers mail (incl. reply/attachments), calendar CRUD and
  `mail watch` (SyncFolderItems rendered as a Graph delta); freebusy/find-times,
  contacts and drive/SharePoint are out of scope (see docs/ews-setup.md).
- `internal/config/`: config loading + the fail-closed allowlist matcher.
- `internal/auth/`: app-only certificate auth (MSAL) + 0600 token cache.
- `internal/graph/`: the scoped Graph REST client — the Graph-side choke point
  that enforces `allowed_mailboxes` / `allowed_sites` before any network call.
- `internal/ews/`: the NTLM EWS SOAP client — the EWS-side choke point; reaches
  each mailbox via ExchangeImpersonation and enforces the same allowlist.
- `internal/{mail,calendar,contacts}/`: HTTP-independent domain logic and Graph
  payload builders (the send guardrail lives in `mail`).
- `docs/`: setup and reference docs. `.agents/skills/m365/`: the agent skill.

## Build, Test, Development

- `make build` — build `bin/m365`.
- `make test` — run all unit tests.
- `make vet` / `make fmt` — vet and format.
- `make check` — fmt + vet + test (run before committing).

## Conventions

- **TDD.** Write the test first for any logic in `internal/*` (config, auth,
  graph, and the domain builders). CLI glue is exercised via the domain packages.
- **Output discipline.** Data → stdout (the JSON envelope via `internal/output`);
  human hints/progress → stderr. Keep stdout parseable.
- **Security is centralized.** All mailbox/site access goes through the
  transport choke point — `internal/graph` (Graph) or `internal/ews` (EWS) — so
  the allowlist cannot be bypassed. Don't add transport calls that skip it (the
  one documented exception is Graph `SearchSites`, read-only discovery). Never
  put secrets on the command line; mail/event bodies come from `--body-file`,
  and the EWS NTLM password is read from `ews_password_file` (0600).
- **Fail closed.** Empty allowlists deny everything; new guardrails should keep
  that property.

## Testing Notes

- Unit tests use stdlib `testing` + `httptest` (no live tenant needed).
- Live end-to-end requires a real Azure app + tenant; see `docs/azure-setup.md`
  and verify with `m365 doctor --live`.

## Branches and local verification

- Stay on the current branch. Switch branches only when explicitly instructed or when other in-progress work requires it.
- Running applications locally and interacting with them is always safe. Before reporting work complete, confirm each affected application builds, starts, and runs; for anything with a browser UI, test the change in a browser.
