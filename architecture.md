# Macro Terminal — Architecture

This document describes how Macro Terminal is actually built and why. It is the in-repository architecture reference for the Go API, the worker, the React client, and the infrastructure around them. The product definition lives in `README.md`; the API surface lives in `api.md`.

## 1. System at a glance

```
                        ┌──────────────────────────────┐
   Browser (React SPA)  │  Vite dev server / Caddy     │
        │  HTTPS/JSON   │  static assets + SPA fallback│
        ▼               └──────────────────────────────┘
┌─────────────────────┐   session cookie (Authlier)
│  api   (Go, Gin)    │──────────────────────────────────┐
│  REST + SSE         │                                  │
└────┬───────────┬────┘   /auth/* + /account/*           │
     │           │          proxied to Authlier ─────────┘
     │           │
     │           ▼
     │    ┌────────────┐      ┌──────────────────────────┐
     │    │ Redis      │      │ worker (Go)              │
     │    │ pub/sub +  │◄─────┤ scheduler, backfill,     │
     │    │ cache      │      │ hydration, enrichment,   │
     │    └────────────┘      │ indexing, clustering,    │
     │                        │ notifications, email     │
     │                        └───┬──────────┬───────────┘
     ▼                            │          │
┌─────────────────────┐           │          │
│ PostgreSQL 17       │◄──────────┘          ▼
│ rows + vectors +    │              Providers: RSS/web/API feeds,
│ LISTEN/NOTIFY +     │              Binance, Biquote, Gemini,
│ outbox tables       │              Resend (transactional email)
└─────────────────────┘
     │
     ▼  OTLP
┌─────────────────────┐
│ otel-collector      │ ──► New Relic
└─────────────────────┘
```

Runtime components (see `compose.yaml`):

| Component | Role |
|---|---|
| `api` | Gin HTTP server: REST API, SSE feed, session handling, Swagger UI |
| `worker` | All background work: ingestion, backfill, hydration, AI pipelines, notifications, email |
| `postgres` | System of record: articles, entities, calendar, candles, vectors, outbox, jobs |
| `redis` | Pub/sub for live-feed fan-out, response caching |
| `otel-collector` | Telemetry pipeline → New Relic |
| `migrate` | Applies Tern migrations from `db/migrations/` at startup |
| Caddy (deploy only) | Reverse proxy, TLS, static serving in production |

## 2. Stack

- **Go 1.26** — `server/cmd/api` and `server/cmd/worker`, module `github.com/Rahmannugar/macro-terminal`
- **Gin** HTTP framework; middleware for telemetry, CORS, rate limiting, safe headers
- **sqlc** (compile-checked SQL), **Tern** migrations, **swag** (OpenAPI 3.1 from Go annotations)
- **PostgreSQL 17** — rows, pgvector-style cosine over stored embeddings, `LISTEN/NOTIFY`
- **Redis 8** — pub/sub and cache
- **React 19 + Vite + TypeScript**, TanStack Query, Tailwind CSS v4, Phosphor icons, lightweight-charts
- **Authlier** — authentication service, proxied under `/auth` and `/account`
- **Gemini** — embeddings (article vectors), article/event explanations, enrichment tagging
- **OpenTelemetry → collector → New Relic**; structured `slog` JSON logs with `event` names

## 3. Server layout and layering

```
server/
├── cmd/api/main.go         # config, telemetry, pool, router
├── cmd/api/router.go       # route assembly + middleware wiring
├── cmd/worker/main.go      # job wiring + graceful shutdown
└── internal/               # one package per domain (see below)
```

Every feature follows the same shape:

```
routes.go        HTTP binding: parse, guard, validate, map status codes
    ↓
service (or job) Business rules, orchestration — no SQL here
    ↓
repositories/    sqlc-generated queries + thin domain helpers
    ↓
PostgreSQL
```

Cross-cutting packages:

- `common/httpresponse` — the single JSON writer; every error goes out through the shared envelope
- `common/paging` — keyset cursor encode/decode (`base64(unixNanos + "." + uuid)`)
- `config` — all environment parsing with defaults and validation (one `Config` struct)
- `infra/*` — database pool, Redis, telemetry, CORS, rate limiting, email delivery, authentication glue
- `openapi` — generated spec, served at `/openapi.json`, plus shared request/response types

Code generation is mandatory and reviewed: `task generate` runs `sqlc generate` and `swag init` (OpenAPI 3.1). Generated output is committed.

## 4. Domain map (`server/internal/`)

| Package | Responsibility |
|---|---|
| `authentication` | Session resolution, `RequireUser` / `RequireAdmin` guards, local account endpoints |
| `users` | Admin user management (list, promote, demote, suspend, reactivate) |
| `articles` | Feed listing (latest / relevance sorts), article detail, related stories |
| `assets` | Watch-list: follow/unfollow entity pairs, pair catalog |
| `calendar` | Economic calendar queries with scope/from/to/country/importance/watch filters |
| `market` | Candle queries, timeframe validation, server-side aggregation (5m–1M) |
| `search` | Semantic search: query embed → vector rank → hydrate in rank order |
| `clustering` | Related-article computation over shared embeddings/entities |
| `events` | SSE endpoint; streams feed invalidation signals over Redis pub/sub |
| `explanation` | One-shot AI explanations of articles and calendar events with macro context |
| `enrichment` | Tag enrichment of stored articles (never rewrites article bodies) |
| `indexing` | Embedding writes to `article_embeddings` (Gemini), batch + throttled |
| `notification` | In-app notification rows for articles/calendar events, outbox-style delivery |
| `ingestion` | Source scheduling (cadence, due-check, pg_notify wake), adapters (RSS/web/API), candle backfill |
| `hydration` | Article body extraction: selectors → Readability → generic container |
| `normalization` | Provider payloads → canonical shapes (candles, calendar, web) |
| `mapping` | Maps fetched items to entities/pairs (followed pairs only) |
| `entities` | Entity and entity-pair catalogs |
| `sources`, `adminconfig` | Source registry and source configuration CRUD |
| `adminjobs` | Failed-job listing and replay |
| `ai` | Gemini client: embeddings + generation, retries, throttling |
| `vector` | Vector storage seam over PostgreSQL (`article_embeddings`) |
| `health` | Liveness and readiness probes |

## 5. Worker architecture

One process, ten goroutines sharing one root context, each with `Run(ctx)` + `Wake()` (one-slot coalescing channel). Shutdown drains all results gracefully.

| Goroutine | What it does |
|---|---|
| `ingestion-runner` | Fetches due sources on cadence; due-check reads persisted `last_run_at`; batch is marked *before* fetch so a slow provider cannot become due early |
| `schedule-listener` | `LISTEN` on source-config changes → wakes the runner (~1s reaction; lost notifications cost at most one tick) |
| `outbox-listener` | `LISTEN` on outbox inserts → wakes consumer jobs immediately |
| `backfill-job` | Fills gaps in stored candles within the retention window (see §6.2) |
| `hydration-job` | Extracts article bodies from stored URLs (outbox-driven, claim with reclaim) |
| `enrichment-job` | Batches articles for AI tagging (per-run cap, fails as a unit) |
| `indexing-job` | Computes Gemini embeddings, writes `article_embeddings`, throttled (batches per 30s tick) |
| `clustering-job` | Maintains related-article links |
| `notification-job` | Enqueues in-app notifications for new articles/calendar events (30s cycle) |
| `email-delivery-job` | Sends templated **transactional** email via Resend (verification + password reset only) |

Outbox pattern everywhere: work is written as a row, claimed with `FOR UPDATE SKIP LOCKED`, retried with backoff, reclaimed after a visibility timeout. Failed attempts land in a failed-jobs table replayable from the admin UI.

## 6. Data flows

### 6.1 Content pipeline

```
sources (news / official / calendar / candles)
   │  scheduled by cadence (runner)          adapters: rss, web (HTML+selectors), api
   ▼
raw fetch → normalization → dedupe → articles / calendar_events rows
   │
   ├─► outbox(article_content)  → hydration → body (selector → Readability → generic)
   ├─► enrichment (tags only; batched; never touches bodies)
   ├─► indexing → Gemini embed → article_embeddings (cosine over pgvector)
   ├─► clustering → related links
   └─► notification → in-app rows → SSE wakes clients
```

Rules: hydrated content is written only when `content IS NULL` (never overwrite); enrichment is tags only; followed-pairs gating applies to mapping, not enrichment.

### 6.2 Market data

- Providers: **Binance** (crypto: BTC, ETH, SOL, BNB) and **Biquote** (FX, indices, commodities — 25 pairs); calendar providers Biquote/FinanceCalendar/Xoomar.
- Stored granularities: `1min` (live + rolling 30-day backfill window) and `1day` (5-year backfill window). Both are env-configurable: `MACRO_TERMINAL_BACKFILL_MINUTE_DAYS` (default 30, max 365), `MACRO_TERMINAL_BACKFILL_DAILY_DAYS` (default 1825, max 3650).
- API serves `1min 5min 15min 1h 4h 1day 1week 1month`. 5m/15m/1h/4h are aggregated from stored 1min candles; 1W/1M from stored 1day — bucket-aligned (epoch boundaries, ISO weeks, first-of-month), no provider calls, no extra tables.
- Candles live in `market_candles` with `UNIQUE(source_id, entity_pair_id, timeframe, timestamp)`; dedupe is upsert-based.

### 6.3 Live feed (SSE)

```
PostgreSQL change → Redis pub/sub → GET /api/v1/events → `event: feed`
                                                     → client refetches current view
```

The SSE payload is an invalidation signal (`{"type":"feed"}`), not the data itself; the client refetches its current query so the displayed sort/filter stays authoritative.

## 7. Authentication and authorization

- **Authlier** is the identity provider. `/auth/*` (sign-up, sign-in, verify-email, resend-verification, forgot-password, reset-password, sign-out) and `/account/*` are reverse-proxied to it; only `GET/PATCH /api/v1/account` are local.
- Session = HttpOnly cookie issued by Authlier; guards resolve it per request.
- Roles: `user` → `admin` → `super admin`. `RequireUser` gates `/api/v1` user routes; `RequireAdmin` gates `/api/v1/admin` (super admin satisfies admin checks).
- Transactional email (verification code, password reset) via Resend templates; **product notifications are in-app only**.

## 8. Data model highlights

| Table | Purpose |
|---|---|
| `articles`, `article_contents`/outbox | Feed items and their hydrated bodies |
| `article_enrichments`, `article_embeddings` | AI tags; Gemini vectors (cosine search) |
| `calendar_events` (+ entities links) | Economic calendar with `country_code` (ISO-2) |
| `entities`, `entity_pairs`, `watch_list` | The information graph and user follows |
| `market_candles` | OHLCV with source/pair/timeframe/timestamp uniqueness |
| `sources`, `source_configurations` | Registry + per-source config (cadence, selectors, keys) with `last_run_at` |
| `outbox` (typed payloads), failed jobs | Reliable async work + admin replay |
| `users` | Local account state (role, status) mirroring Authlier identity |

Migrations are append-only Tern files in `db/migrations/`.

## 9. API conventions

Full reference in `api.md`. Invariants:

- Everything app-facing under `/api/v1`; health probes at `/health/live`, `/health/ready`.
- **Keyset cursor pagination only** — `limit` + opaque `cursor`, never offset.
- Every failure is `{"error":{"code":"...","message":"..."}}` with the route's documented code.
- Spec-first surface: Swagger JSON generated from route annotations (`task generate`), served at `/openapi.json`.

## 10. Frontend

```
client/src/
├── routes/        # route modules: home, search, watchlist, calendar, market,
│                  # article detail, auth flows, admin/*
├── components/    # app shell (sidebar/topbar), theme, chart, domain widgets
├── ui/            # primitives (Button, Skeleton, Select, …)
└── lib/           # api client, query hooks, auth, market/timeframe helpers
```

- Routing with React Router; server state with TanStack Query (keyset pagination via `useInfiniteQuery`).
- Styling: Tailwind v4 tokens (`bg-card`, `text-muted-foreground`, brand `#087CEC`); typography Onest + Bricolage Grotesque.
- Theme: system preference by default; explicit light/dark once chosen; pre-hydration script in `index.html` prevents flash; chart re-themes via MutationObserver.
- Market chart: TradingView lightweight-charts (candles, crosshair OHLC legend, autoSize).
- Live behavior: SSE pill counts new items for the *current* tab and clears on tab switch.

## 11. Observability

- `slog` JSON logs with stable `event` names (`ingestion.backfill.persisted`, `hydration.*`, …) plus `operation`, `source`, `duration_ms` fields — greppable, consistent across api and worker.
- OpenTelemetry traces/metrics from the API → `otel-collector` → New Relic.
- Liveness/readiness probes separate process health from database health.

## 12. Configuration

- One `config.Config` parsed from environment in `server/internal/config`; every variable has a default, validation, and an entry in the root `.env.example` (`.env` mirrors it for non-secrets).
- Key knobs: backfill windows (§6.2), Gemini API key (AI features degrade gracefully when absent), Authlier address, Redis address, telemetry endpoints.

## 13. Development workflow

Taskfile is the entry point for everything:

| Task | Purpose |
|---|---|
| `task up` / `task down` / `task logs` | Compose stack lifecycle |
| `task run-api` / `task run-worker` | Local runs outside containers |
| `task generate` | `sqlc generate` + `swag init` (committed output) |
| `task migrate` / `task seed` | Schema + seed data |
| `task test` / `task test-integration` | Go unit / integration (real Postgres) |
| `task lint` / `task check` | golangci-lint + format + tests + spec diff gates |
| `task client-check` / `task client-build` | Biome + tsc, production bundle |

Verification bar for any change: `go test ./...`, `go build ./...`, `golangci-lint run ./...`, `gofmt` clean, `bun run check`, `bun run build`.

## 14. Invariants (do not break without a spec change)

1. Keyset cursors everywhere — never introduce offset pagination.
2. Hydrated article bodies are written once (`content IS NULL` guard); enrichment never rewrites bodies.
3. AI calls are batched, capped, and throttled; the Gemini key gates all AI features.
4. Every async job is outbox-based: claim → attempt → backoff → failed-jobs (replayable), never fire-and-forget.
5. Errors always use the shared envelope; success shapes come from the Swagger spec.
6. Product notifications are in-app; email is transactional only (verification, password reset).
7. `.env.example` and `.env` move together for non-secret variables.
8. Generated code (sqlc, swagger) is regenerated and committed in the same change.
