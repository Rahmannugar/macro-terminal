#!/usr/bin/env sh
# Deploy the latest successful main revision when given its commit SHA.
set -eu
cd "$(dirname "$0")/.."
git pull --ff-only
if [ "${1:-}" != "" ]; then
  if [ "$(git rev-parse HEAD)" != "$1" ]; then
    echo "main moved beyond the validated revision; waiting for its CI result" >&2
    exit 1
  fi
  export MACRO_TERMINAL_IMAGE_TAG="sha-$1"
fi
# let the registry finish serving the manifest that triggered this deploy
sleep 20
docker compose --env-file deploy/.env -f deploy/compose.yml pull
docker compose --env-file deploy/.env -f deploy/compose.yml up -d --wait --force-recreate
docker compose --env-file deploy/.env -f deploy/compose.yml ps
