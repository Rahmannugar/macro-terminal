#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."
state=/var/lib/macro-terminal/deployed-sha
response=$(curl --fail --silent --show-error --max-time 15 \
  -H 'Accept: application/vnd.github+json' \
  -H 'User-Agent: macro-terminal-deploy' \
  'https://api.github.com/repos/Rahmannugar/macro-terminal/actions/workflows/ci.yml/runs?branch=main&event=push&status=success&per_page=1')
sha=$(printf '%s' "$response" | python3 -c 'import json,re,sys
runs=json.load(sys.stdin).get("workflow_runs",[])
if not runs: sys.exit("no successful main CI run")
run=runs[0]
sha=run.get("head_sha","")
if run.get("conclusion")!="success" or run.get("head_branch")!="main" or run.get("event")!="push" or not re.fullmatch(r"[0-9a-f]{40}",sha): sys.exit("invalid CI result")
print(sha)')

if [ -f "$state" ] && [ "$(cat "$state")" = "$sha" ]; then
  exit 0
fi

git fetch origin main
if [ "$(git rev-parse FETCH_HEAD)" != "$sha" ]; then
  echo "latest main has no successful CI result yet" >&2
  exit 0
fi

"$(dirname "$0")/deploy.sh" "$sha"
install -d -m 0755 /var/lib/macro-terminal
printf '%s\n' "$sha" > "$state.tmp"
mv "$state.tmp" "$state"
