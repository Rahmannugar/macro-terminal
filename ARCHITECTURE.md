# Macro Terminal Architecture

Macro Terminal is a macroeconomic intelligence terminal. It continuously ingests information from official economic institutions, financial news sources, market-data providers, and economic-calendar providers; normalizes and deduplicates that information; maps it to relevant entities and asset relationships; and enriches it with structured AI-generated context.

Users configure the assets and economic relationships they care about, and Macro Terminal surfaces the relevant information for those assets. Users can search the information semantically, discover related stories, view economic-calendar data, request one-shot AI explanations of individual articles or calendar events using the surrounding macro context, and receive alerts for followed pairs in-app or over Slack and Discord webhooks.

It is not a trading execution platform. It generates no buy/sell signals and makes no directional trading recommendations. Its purpose is to help users understand what is happening in the macro environment, how pieces of information are connected, and why a particular piece of information matters in context.

The HTTP surface is mapped in [API.md](API.md).

## System diagram

```
                                ┌─────────────────────────────────────────────┐
                                │                  Caddy                       │
                                │        reverse proxy + static SPA            │
                                └───────────────┬─────────────┬───────────────┘
                                                │             │
                                      /api, /docs, /health   /* (static client)
                                                │             │
                          ┌─────────────────────▼──┐   ┌──────▼──────────┐
                          │   API (Go, Gin)        │   │  Client (React) │
                          │   server/cmd/api       │   │  client/        │
                          └───┬────────────┬───────┘   └─────────────────┘
                              │            │
   /auth/*, /account/* ───────┘            │ semantic search / related articles
   (reverse proxy)                         │
                              ┌────────────▼───────────────────────────────┐
                              │                PostgreSQL                   │
                              │  articles · calendar events · market data   │
                              │  entities · pairs · users · outbox · cache  │
                              └────────────▲───────────────▲───────────────┘
                                           │               │
                              ┌────────────┴───┐   ┌───────┴──────────────┐
                              │ Worker (Go)    │   │      Redis           │
                              │ server/cmd/    │   │  outbox relay →      │
                              │ worker         │   │  Streams · SSE wake  │
                              └───┬────────┬───┘   │  · short-lived AI    │
                                  │        │       │    explanation cache │
                 fetch/parse/map/enrich    │       └──────────────────────┘
                                  │        │
                    ┌─────────────▼──┐  ┌──▼────────────────────────────┐
                    │  Providers     │  │  Gemini API                    │
                    │  RSS · API ·   │  │  embeddings · explanations ·   │
                    │  web selectors │  │  enrichment tagging            │
                    │  · market APIs │  └───────────────┬────────────────┘
                    └────────────────┘                  │ external vectors
                                                    ┌──▼────────────────────┐
                                                    │  Ahnlich DB           │
                                                    │  vector index (store  │
                                                    │  "articles", cosine)  │
                                                    └───────────────────────┘

  Authlier (managed auth service) ── session cookie + /auth/* proxy
  New Relic ← OpenTelemetry collector ← API + worker traces/metrics/logs
  Slack / Discord ── signed webhook deliveries (outbox type "webhook_delivery")
```

## Stack

- **Go 1.26** — API and worker, one module (`github.com/Rahmannugar/macro-terminal`)
- **Gin** — HTTP routing; handlers wrap `net/http` handlers where the standard library already solves the job
- **React 19 + Vite + TypeScript** — single-page client (`client/`), TanStack Query, Tailwind
- **PostgreSQL** — system of record for all domain data; Tern migrations, sqlc-generated repositories
- **Redis** — durable outbox relay (Streams), live-feed wake channel, short-lived AI-explanation cache
- **Ahnlich DB** — vector index. Gemini embeds; Ahnlich stores the vectors and answers cosine nearest-neighbour queries from an in-memory index with a persistence snapshot on disk. There is no pgvector and no embedding table in PostgreSQL.
- **Gemini** — embeddings (`gemini-embedding-001`), article/event explanations, enrichment tagging
- **Authlier** — authentication; session cookie, `/auth/*` reverse-proxied
- **Caddy** — production reverse proxy and static SPA serving
- **OpenTelemetry → New Relic** — traces, metrics, structured logs via the collector
- **Docker** — local development stack (`compose.yaml`)

## Layout

```
macro-terminal/
├── Taskfile.yml          # every development task (up, check, generate, logs, …)
├── compose.yaml          # postgres, redis, ahnlich-db, otel-collector, api, worker
├── go.mod
├── client/               # React SPA
├── server/
│   ├── cmd/
│   │   ├── api/          # HTTP server
│   │   ├── worker/       # scheduled pipelines + outbox consumers
│   │   ├── seed/         # bootstrap admin + catalog seed
│   │   └── reindex/      # re-opens completed/failed index jobs (index-loss recovery)
│   └── internal/         # every domain package (see Domain map)
├── db/migrations/        # Tern migrations (up + down)
└── deploy/               # production container definitions
```

Every domain package under `server/internal/` follows the same internal layout:

```
domain/
├── models.go             # domain types (no SQL, no HTTP)
├── service.go            # business logic; depends on consumer-declared repository interfaces
├── repository.go         # interface the service needs (consumer-side)
├── handlers.go           # HTTP handlers (Gin)
├── queries/              # sqlc-authored SQL (one file per concern)
├── repositories/generated/  # sqlc output — never edited by hand
└── *_test.go
```

## Processes

Two long-running processes share one module and one database.

### API — `server/cmd/api`

Serves the whole HTTP surface: health probes, the Authlier proxy, `/api/v1/*` for users and admins, the OpenAPI document and Scalar reference at `/docs`, and the live-feed SSE endpoint. It never runs scheduled work. The searcher (Ahnlich) is constructed only when both the Gemini API key and the Ahnlich address are configured; otherwise search endpoints answer 503 `search_unavailable`.

### Worker — `server/cmd/worker`

Runs every background pipeline concurrently and shuts all of them down gracefully on SIGTERM:

| Pipeline | Cadence | What it does |
|---|---|---|
| Ingestion runner | per-source cadence (news 5m, central banks 15m, calendar/official 30m, market 1m) | Fetch → normalize → deduplicate → map → persist articles and calendar events; wake on configuration changes via PostgreSQL LISTEN/NOTIFY |
| Enrichment cycle | 30s tick + outbox wake | Batch AI tagging of stored articles (entity/entity-pair/topic/person/institution lists, trimmed and validated) |
| Hydration cycle | 30s tick + outbox wake | Fetch the full article body for feed teasers (selector → Readability → generic container), store only when longer than what is already there |
| Indexing cycle | 30s tick + outbox wake | Gemini-embed enriched articles into the Ahnlich vector store |
| Clustering cycle | 30s tick + outbox wake | Group semantically similar, date-adjacent articles into story clusters |
| Notification fan-out | outbox wake | Turn followed-pair matches into per-user in-app notifications and Slack/Discord webhook deliveries |
| Market backfill | on boot + daily | Fill missing candle history (up to five years) for tracked pairs |
| Live-feed publisher | after each persist pass | XADD a `feed` invalidation signal to Redis Streams for connected SSE clients |

## Domain map

Every package under `server/internal/` and what it owns:

| Package | Responsibility |
|---|---|
| `config` | Koanf-based environment loading and validation |
| `telemetry` | OpenTelemetry setup, graceful shutdown wiring |
| `database` | pgx pool construction, LISTEN/NOTIFY session |
| `cache` | Redis client (Streams, short-lived cache) |
| `infra/safehttp` | SSRF-hardened HTTP transport for all outbound fetches |
| `ingestion` | Source adapters (RSS, API, web selectors), fetcher, retry/backoff, circuit breaker, cadence runner |
| `normalization` | Feed/web/calendar payloads → canonical article candidates; URL canonicalization and deduplication |
| `mapping` | Immutable per-pass dictionary; longest-phrase-first word-boundary matching onto entities and pairs |
| `articles` | Article persistence (idempotent upsert), entity links, unmapped queue, feed queries |
| `calendar` | Calendar-event persistence, indicator classification, country/entity links |
| `entities` | Entities, economic indicators, knowledge terms, entity-pair catalog |
| `sources` | Source and source-configuration CRUD, schedule state |
| `hydration` | Full-body article extraction (selector → Readability → generic) |
| `ai` | Gemini client: embeddings, explanations, batch enrichment (behind an `Enricher`/`Embedder` contract) |
| `enrichment` | Outbox pipeline that batch-tags articles with AI |
| `vector` | Ahnlich client: store/search article vectors; Gemini embeds, Ahnlich stores and ranks |
| `indexing` | Outbox pipeline that embeds enriched articles into the vector store |
| `clustering` | Story-cluster pipeline and related-article queries |
| `search` | Semantic search endpoint; keyset cursor over vector rank |
| `market` | Candle storage, timeframe aggregation, provider backfill |
| `notification` | In-app notifications, per-pair alert preferences, Slack/Discord webhook channels and delivery |
| `events` | SSE live-feed endpoint (Redis Streams → browser) |
| `explanation` | One-shot AI explanations for articles and calendar events |
| `assets` | User watch list (follow/unfollow pairs) with alert sync |
| `users` / `authentication` | User records; Authlier proxy, session cookie, role gates |
| `adminconfig` / `adminjobs` | Admin: source/indicator/entity configuration; failed-job inspection and replay |
| `health` | Liveness and readiness probes |
| `openapi` | Generated OpenAPI document (swag annotations; CI fails on drift) |

## Data flow

### Ingestion

1. The cadence runner lists due source configurations (cadence compared against `last_run_at`, persisted so a restart never refetches work that just ran). Configuration changes wake it through LISTEN/NOTIFY; the minute tick is the recovery path.
2. The matching adapter (RSS, API, or web selectors) fetches through the SSRF-hardened transport: browser user-agent for web article pages, retries with backoff and `Retry-After`, a per-host circuit breaker so one failing provider never blocks the rest of the pass.
3. Normalization turns the payload into canonical candidates: tracking parameters stripped, relative links resolved, in-batch duplicates collapsed, invalid and homepage-only URLs rejected.
4. Mapping runs a single immutable dictionary per pass (entities + knowledge terms + pairs; case-insensitive, word-boundary, longest-phrase-first). Mapped candidates are persisted; unmapped ones go to a queue without blocking the pass.
5. Persistence is idempotent: a refetch is a no-op on identity, refreshes metadata, and keeps the original publication date.

Calendar sources parse through the same normalize/deduplicate path, classify rows by indicator, and store events idempotently on `(source, indicator, scheduled_at)`; non-calendar rows (speeches, reports, holidays) flow to news only with a valid deep link.

### Hydration

Feed entries arrive as teasers. The hydration job re-fetches the article page and extracts the body in layers — the source's configured content selector first, then Readability, then a generic densest-container heuristic — storing only when the new body is longer than what is already stored, so a paywall or consent wall can never destroy a good body.

### Enrichment and indexing

A stored article with no enrichment is discovered by the enrichment cycle, claimed with skip-locked semantics, and sent to Gemini in batches of up to ten. Only known entity codes are kept; every list is trimmed, deduplicated, and capped. AI never writes links or touches the unmapped queue.

Once enrichment is stored, the indexing cycle embeds the article (title + body, truncated to the model's window) with Gemini and writes the vector into the Ahnlich store with `article_id` and `published_at` predicates. A stale vector for the same article is deleted before the new one is stored, so a rebuild can never duplicate. Index-loss recovery (`task reindex`) reopens completed and failed jobs.

### Search, related articles, and relevance

`GET /api/v1/search/articles` embeds the query with the same Gemini model, asks Ahnlich for the nearest vectors (cosine), and hydrates authoritative rows from PostgreSQL in vector-rank order. The cursor is an opaque rank offset — keyset semantics over a similarity ranking, never offset pagination over SQL. Related articles work the same way, excluding the source article. The home feed's "Relevant" sort embeds the user's followed pair symbols as the query; with no pairs it falls back to latest.

### Notifications

Following a pair subscribes the user to `new_article` and `new_calendar_event` alerts on every connected channel. The in-app rows are the source of truth; Slack/Discord channel rows mirror them and are re-synced on every toggle. A stored article or calendar event that matches a followed pair fans out per-user notifications and enqueues webhook deliveries on the shared outbox. Webhook URLs are validated against a host allowlist, encrypted at rest (AES-256-GCM), and never returned to the client — only a `host/…last4` hint. The worker decrypts and POSTs on delivery: 2xx completes, 429/5xx backs off, other 4xx fails permanently.

### Live feed

After a persist pass that stored anything, the worker XADDs a `{"type":"feed"}` signal to a Redis Stream. The SSE endpoint reads it per-connection and pushes an invalidation event; the client shows a "N new articles" pill and refetches its current query on click. No consumer groups — every instance sees every event. No article payloads, no persistence. The same stream carries `{"type":"notification"}` for the bell.

### Market data

Candles are stored at `1min` and `1day` resolution from OANDA (FX, commodities, indices) and Binance (crypto). Larger timeframes (5min–4h, 1week, 1month) are aggregated server-side from stored candles in bounded windowed scans — no provider calls, no schema change. A backfill job fills missing history (up to five years) on every boot and daily thereafter.

## Authentication and authorization

Authlier issues an HttpOnly session cookie. The API proxies `/auth/*` and `/account/*` to Authlier unchanged; the only local account endpoints are `GET/PATCH /api/v1/account`. Roles are `user`, `admin`, and `super_admin`; admin routes accept either elevated role, and only a super admin can change another super admin's role.

## Data model

Twenty-plus tables; the core graph:

- **sources** and **source_configurations** — providers, their fetch configuration (URL, type, selectors), cadence, and last-run state
- **articles** — identity `(source_id, url)`, title, body (teaser upgraded by hydration), publication date, image
- **article_entities**, **unmapped_articles** — mapping outcomes and the review queue
- **calendar_events** — indicator, schedule, actual/forecast/previous values, country/entity links
- **entities**, **entity_pairs**, **economic_indicators**, **knowledge_terms** — the information graph
- **article_enrichments** — AI tags per article (unique per article; a replay can never store twice)
- **story_clusters**, **article_story_clusters** — semantic grouping
- **market_candles** — `(pair, timeframe, open_time)` unique
- **users**, **user_assets**, **user_alerts**, **user_notifications**, **user_notification_channels** — accounts, watch list, alert preferences, notification inbox, webhook channels
- **outbox** — the single durable job queue (types: enrichment, hydration, index, cluster, notification, webhook delivery); skip-locked claims, exponential backoff, permanent failure, reclaim of abandoned rows

Migrations are forward-and-back Tern files; every migration is verified up and down before it lands.

## API conventions

Everything app-facing is under `/api/v1`; health probes are at `/health/live` and `/health/ready`. Pagination is keyset-only — `limit` and an opaque `cursor`, never offset. Errors are `{"error":{"code":"…","message":"…"}}`. The machine-readable contract is the generated OpenAPI document (swag annotations; CI fails on drift); [API.md](API.md) is the human-readable map.

## Frontend

Single-page React app. TanStack Query owns all server state; the only imperative data path is the SSE hook, which never carries rows — it invalidates and the client refetches. The shell is a fixed sidebar plus header; dark mode is class-toggled with a localStorage preference and a pre-paint script to avoid a flash. Charts are TradingView lightweight-charts.

## Observability

OpenTelemetry traces and metrics flow to the collector and onward to New Relic; no vendor SDK is embedded in the services. Every pipeline logs structured events (`ingestion.fetch.succeeded`, `hydration.completed`, `indexing.cycle`, and so on) with counts and failure reasons. Health endpoints are process- and database-liveness only — they never make outbound calls.

## Configuration

Koanf loads `.env` first and lets process environment override. The important keys:

| Key | Purpose |
|---|---|
| `MACRO_TERMINAL_DATABASE_*` | PostgreSQL connection; separate API/worker pool caps |
| `MACRO_TERMINAL_REDIS_URL` | Redis |
| `MACRO_TERMINAL_AHNLICH_DB_ADDR` | Ahnlich DB gRPC address |
| `MACRO_TERMINAL_AUTH_*` | Authlier base URL, client URL, trusted origins/proxies, OTP HMAC secret |
| `MACRO_TERMINAL_AI_API_KEY` | Gemini key; vector work is skipped entirely without it |
| `MACRO_TERMINAL_AI_MODEL` / `_EXPLANATION_MODEL` / `_EMBEDDING_MODEL` | Model overrides |
| `MACRO_TERMINAL_RESEND_*` | Transactional email (verification, password reset) |
| `MACRO_TERMINAL_BACKFILL_*` | Market backfill windows |
| `MACRO_TERMINAL_CLIENT_BASE_URL` | Public client origin for links in notifications |

`.env.example` documents every key; `.env` is never committed.

## Development workflow

```sh
cp .env.example .env
task up          # postgres, redis, ahnlich-db, otel-collector, migrations, api, worker
task check       # openapi generate, formatting, tests, lint, build
cd client && bun run check
task down
```

Integration tests run against real PostgreSQL in Docker (`task test-integration`). After changing migrations or route annotations, `task generate` refreshes sqlc output and the OpenAPI document; `task check` fails on any drift.

## Invariants

- Pagination is keyset everywhere. Offset pagination does not exist.
- The outbox is the only path for background work. Nothing important is fire-and-forget.
- Hydration never overwrites stored content with something shorter.
- AI never writes links, never touches the unmapped queue, and never runs on a user's hot path — explanations are one-shot requests, tagging and embedding are background jobs.
- Webhook URLs are encrypted at rest and never leave the server in plaintext.
- Ahnlich is the vector layer. Embeddings are computed by Gemini and stored in Ahnlich; PostgreSQL holds no vectors.
- Every outbound fetch goes through the SSRF-hardened transport.
- Refetching a source is always idempotent — identity is stable, publication dates never move.
