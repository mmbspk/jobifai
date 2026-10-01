#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="${ROOT}/data/stripe-test-mode.local.env"
PRICE_FILE="${ROOT}/data/stripe-test-price-ids.json"

load_env() {
  if [[ -f "$ENV_FILE" ]]; then
    # shellcheck disable=SC1090
    set -a && source "$ENV_FILE" && set +a
  fi
  STRIPE_SECRET_KEY="${STRIPE_SECRET_KEY%\"}"; STRIPE_SECRET_KEY="${STRIPE_SECRET_KEY#\"}"
  STRIPE_SECRET_KEY="${STRIPE_SECRET_KEY%\'}"; STRIPE_SECRET_KEY="${STRIPE_SECRET_KEY#\'}"
  STRIPE_WEBHOOK_SECRET="${STRIPE_WEBHOOK_SECRET%\"}"; STRIPE_WEBHOOK_SECRET="${STRIPE_WEBHOOK_SECRET#\"}"
  STRIPE_WEBHOOK_SECRET="${STRIPE_WEBHOOK_SECRET%\'}"; STRIPE_WEBHOOK_SECRET="${STRIPE_WEBHOOK_SECRET#\'}"
  if [[ -z "${STRIPE_SECRET_KEY:-}" || ! "$STRIPE_SECRET_KEY" =~ ^sk_test_ ]]; then
    echo "Set STRIPE_SECRET_KEY (sk_test_...) in $ENV_FILE or the environment." >&2
    exit 1
  fi
  export STRIPE_API_KEY="$STRIPE_SECRET_KEY"
}

stripe_api() {
  stripe "$@" --api-key "$STRIPE_SECRET_KEY" 2>/dev/null
}

require_jq() {
  command -v jq >/dev/null || { echo "jq required" >&2; exit 1; }
}

jobifai_api() {
  local method=$1 path=$2 token=$3
  shift 3
  curl -sfS -X "$method" \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    "$@" \
    "http://localhost:8081${path}"
}

admin_token() {
  curl -sfS -X POST http://localhost:8081/auth/login \
    -H 'Content-Type: application/json' \
    -d '{"email":"admin@jobifai.local","password":"jobifai2024!"}' \
    | jq -r .access_token
}
