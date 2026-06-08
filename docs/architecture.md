# Architecture

## Main Responsibilities

- Manage a B2B sales pipeline: clients, leads, sales processes, contracts, cashflow, and upsells
- Track marketing stages (events/webinars) and their conversion to clients
- Forecast cashflow from payment schedules attached to contracts
- Expose a REST API consumed by a frontend dashboard
- Support natural language queries via Claude AI

## Entrypoint

`main.go` — loads env, connects to DB, runs migrations, marks expired clients inactive, initialises Google Auth, wires the handler with store and config, mounts the chi router, and starts the HTTP server. Optionally starts a pprof listener if `PPROF_ADDR` is set.

## Packages

(`internal/*` — not callable by other apps)

### Data model

`internal/domain/` — shared business types (Client, Lead, SalesProcess, Contract, CashflowEntry, ContractUpsell, Stage, Comment, etc.)

- Contracts have a one-to-many relationship with CashflowEntry (payment schedule)
- SalesProcess belongs to a Client; each process can record upsell attempts linked to a new Contract
- Comments are polymorphic: they attach to any entity via an `entity_type` + `entity_id` pair

### Repository

`internal/store/` — all database queries (raw SQL, no ORM) and data transformations

- `store.go` — Store struct + constructor; wires the concrete `*sql.DB`
- One file per domain area (clients, sales, contracts, cashflow, leads, stages, etc.)
- Handles Brutto → Netto conversion: DB stores gross revenue; store converts to net on read using a configurable VAT rate pulled from `app_settings`
- Transactions used for multi-entity writes (e.g. creating a Lead + Client + SalesProcess atomically in `StartSales`)

`internal/db/`

- `connection.go` — PostgreSQL connection pool setup
- `migrations/` — `golang-migrate` SQL migrations, run automatically on startup

### Controllers

`internal/api/` — HTTP layer

- HTTP handlers using the chi router; routes defined in `router.go`
- Handlers are wired to the store via a local `AppStore` interface (`interfaces.go`) — the API package has no direct import of `internal/store`, so the store is swappable for tests
- Business orchestration lives in the handlers (validation, fan-out to multiple store calls, assembling responses)
- `handler.go` — `Handler` struct holding store, config, auth, and mailer dependencies

### Infrastructure

`internal/auth/` — Google OAuth2

- OAuth2 login flow, callback, and session cookie management (HMAC-SHA256 signed cookies)
- Email whitelist enforcement: only `ALLOWED_EMAILS` can log in
- Kept separate from any API concern — authentication and request handling are distinct responsibilities

`pkg/mailer/` — SMTP email notifications

- Sends a notification email when a new contract is created
- Configured entirely via env vars (`SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `SMTP_FROM`)

### Natural language queries

`internal/api/nlq.go` + `nlq_schema.go`

- `POST /api/nlq` accepts a free-text question and returns a structured answer
- Builds a JSON schema describing the DB shape, sends it with the question to the Claude API, and executes the resulting SQL query
- Schema is kept in `nlq_schema.go` separately from the handler logic

## Key data flows

### Starting a sales process (`POST /api/sales/start`)

1. Handler receives lead info (name, email, phone, source stage)
2. Store upserts Lead and Client (merges if email already exists), creates SalesProcess — all in one transaction
3. Optionally triggers a mailer notification

### Contract + cashflow

1. Contract is created with total revenue, duration, and payment frequency
2. Store generates `cashflow_entries` (one per payment period) with `due_date`, `amount`, and `status = pending`
3. Entries progress: `pending → confirmed → paid`
4. Dashboard forecast aggregates pending + confirmed entries by month

### Upsells (renewals)

1. A `contract_upsell` record is attached to an existing SalesProcess
2. If the upsell succeeds, a new Contract is created and linked to the upsell record
3. Analytics queries the `contract_upsells` table for conversion rate (Verlängerungsquote)

## Monetary handling

The app operates with German gross/net (Brutto/Netto) semantics:

- **DB stores Brutto** (VAT-inclusive)
- **API responses return Netto** (auto-converted via `netFromGross()` using the VAT rate from `app_settings`)
- **API request payloads are Brutto** (frontend sends gross; the store converts internally before writing)
