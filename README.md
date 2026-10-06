# Macro Terminal

Macro Terminal is a macroeconomic intelligence terminal that connects economic data, financial news, market data, and economic narratives into a unified information graph.

It continuously ingests information from official economic institutions, financial news sources, market-data providers, and economic-calendar providers; normalizes and deduplicates that information; maps it to relevant entities and asset relationships; and enriches it with structured AI-generated context.

Users configure the assets and economic relationships they care about, and Macro Terminal surfaces the relevant information for those assets. Users can search the information semantically, discover related stories, view economic-calendar data, and request one-shot AI explanations of individual articles or calendar events using the surrounding macro context. It is not a trading execution platform, does not generate buy/sell signals, and does not make directional trading recommendations. Its purpose is to help users understand what is happening in the macro environment, how pieces of information are connected, and why a particular piece of information matters in context.

https://excalidraw.com/#json=gNTtu36lxwijy3kd2t7Uo,SkG-HvFr6X2ghNG1eyBWyg

## Stack

- Go 1.26 — API and worker
- React 19, Vite, TypeScript
- PostgreSQL
- Redis
- Ahnlich — vector search and semantic retrieval
- Gemini
- Authlier — authentication
- Caddy — reverse proxy and static serving
- OpenTelemetry → New Relic
- Docker

## Web scraping

Two sources are scraped from web pages using HTML selectors: **People's Bank of China (PBOC)** (`pbc.gov.cn`) and **IEA** (`iea.org`) — their listing pages and their article bodies. Every other source is retrieved from its published feed or API.

## Layout

```
macro-terminal/
├── Taskfile.yml        # all development tasks
├── compose.yaml        # local postgres, redis, otel-collector, api, worker
├── go.mod              # module github.com/Rahmannugar/macro-terminal
├── client/             # React SPA (see client/README.md)
├── server/             # Go API and worker (see server/README.md)
├── db/migrations/      # Tern migrations
└── deploy/             # production deployment
```

## Getting started

Prerequisites: Go 1.26+, Bun, Docker, [Task](https://taskfile.dev).

```sh
cp .env.example .env
task up          # postgres, redis, otel-collector, migrations, api, worker
task logs
task down
```

The API listens on http://localhost:8081 — health at `/health/ready`, API reference at `/docs`.

To run the client:

```sh
cd client
bun install
bun run dev
```

The client runs on http://localhost:5173.

## Validation

```sh
task check               # openapi generate, formatting, tests, lint, build
cd client && bun run check   # biome, typescript
```

`task check` requires the golangci-lint version pinned in `.golangci-lint-version`. PostgreSQL integration tests run with `task test-integration` (Docker required).
