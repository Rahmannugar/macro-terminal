#!/usr/bin/env sh
# Deploy the latest main to this server: run from anywhere on the box.
set -eu
cd "$(dirname "$0")/.."
git pull --ff-only
docker compose --env-file deploy/.env -f deploy/compose.yml pull
docker compose --env-file deploy/.env -f deploy/compose.yml up -d --wait
docker compose --env-file deploy/.env -f deploy/compose.yml ps
