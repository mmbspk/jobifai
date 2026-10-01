#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"
require_jq

PRICE_FILE="${ROOT}/data/stripe-test-price-ids.json"
[[ -f "$PRICE_FILE" ]] || { echo "Run ensure-test-catalog.sh first" >&2; exit 1; }

TOKEN=$(admin_token)
PRICES=$(cat "$PRICE_FILE")

BODY=$(jq -n \
  --argjson packs "$(echo "$PRICES" | jq '.top_up_packs')" \
  --arg starter "$(echo "$PRICES" | jq -r .stripe_price_starter)" \
  --arg pro "$(echo "$PRICES" | jq -r .stripe_price_pro)" \
  '{
    enforcement_default: true,
    trial_credits: 500,
    trial_days: 7,
    starter_credits_monthly: 3000,
    pro_credits_monthly: 8000,
    subscriber_grace_credits: 200,
    credits_per_usd: 1000,
    service_markup: 0.5,
    per_call_fee_usd: 0.002,
    stripe_price_starter: $starter,
    stripe_price_pro: $pro,
    top_up_packs: $packs
  }')

curl -sfS -X PUT http://localhost:8081/api/admin/quota/defaults \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "$BODY" >/dev/null

echo "Quota/billing defaults updated via admin API."
