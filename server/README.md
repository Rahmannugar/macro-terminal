# Macro Terminal — Server

Macro Terminal is a macroeconomic intelligence terminal that connects economic data, financial news, market data, and economic narratives into a unified information graph.

This directory contains the Go backend for Macro Terminal: the HTTP API and
the scheduled worker. The current server provides the API foundation,
configuration and environment loading, health checks, the OpenAPI document and
Scalar reference, structured observability with graceful shutdown, and the
migration workflow for the initial schema.

## Technology

- Go 1.26
- Gin
- Koanf
- PostgreSQL with pgx
- Tern
- Redis
- Ahnlich
- Gemini
- Authlier
- OpenTelemetry
- Task

## Local Development

Create your local configuration:

```bash
cp .env.example .env
```

Start the complete local environment with Docker:

```bash
task up
```

This starts PostgreSQL, Redis, and the OpenTelemetry Collector with persistent
local volumes, applies pending Tern migrations, then starts the API and
worker. Follow the logs with `task logs`, and stop everything without
deleting its data with `task down`.

To run the API directly on the host instead, start the containers, apply
migrations, and run the process:

```bash
docker compose up -d --wait postgres redis otel-collector
task migrate
task run-api
```

The API is available at [http://localhost:8081](http://localhost:8081).
The Scalar API reference is available at
[http://localhost:8081/docs](http://localhost:8081/docs), and the generated
OpenAPI document is served at `/openapi.json`.

Koanf loads `.env` first and applies process environment variables as
overrides. `MACRO_TERMINAL_ENVIRONMENT` accepts `development` or `production`
and defaults to `development`. Production configuration is supplied by the
deployment environment.

The root `Dockerfile` and `compose.yaml` are local-development tooling.
Production container definitions belong under `deploy/`.

## Validation

Run generation, formatting, tests, lint, and build checks:

```bash
task check
```

`task check` requires the `golangci-lint` version pinned in
`.golangci-lint-version`. Run `task lint` to execute that check independently.

Run the PostgreSQL integration tests with Docker available:

```bash
task test-integration
```

After changing migrations, regenerate the OpenAPI document:

```bash
task generate
```
