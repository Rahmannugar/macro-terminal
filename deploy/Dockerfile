FROM golang:1.26-alpine AS deps

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

FROM deps AS build

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./server/cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./server/cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seed ./server/cmd/seed && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tern github.com/jackc/tern/v2

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata aws-cli jq && \
    adduser -D -u 10001 app

COPY --from=build /out/api /out/worker /out/seed /out/tern /usr/local/bin/
COPY server/internal/infra/database/tern.conf /app/server/internal/infra/database/tern.conf
COPY db/migrations /app/db/migrations
COPY deploy/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

WORKDIR /app
USER app

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/health/live >/dev/null || exit 1

ENTRYPOINT ["/bin/sh", "/app/entrypoint.sh"]
CMD ["/usr/local/bin/api"]
