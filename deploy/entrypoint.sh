#!/bin/sh
set -eu

if [ -n "${MACRO_TERMINAL_SECRETS_ARN:-}" ]; then
  echo "loading secrets from AWS Secrets Manager"
  secrets="$(aws secretsmanager get-secret-value \
    --secret-id "$MACRO_TERMINAL_SECRETS_ARN" \
    --query SecretString \
    --output text)"
  eval "$(printf '%s' "$secrets" | jq -r 'to_entries[] | "export \(.key)=\(.value | @sh)"')"
fi

exec "$@"
