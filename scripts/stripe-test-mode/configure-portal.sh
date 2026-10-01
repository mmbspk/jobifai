#!/usr/bin/env bash
# Best-effort Customer Portal configuration for Test Mode (Starter/Pro switch + cancel).
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"
load_env
require_jq

PRICE_FILE="${ROOT}/data/stripe-test-price-ids.json"
[[ -f "$PRICE_FILE" ]] || { echo "Run ensure-test-catalog.sh first" >&2; exit 1; }

STARTER=$(jq -r .stripe_price_starter "$PRICE_FILE")
PRO=$(jq -r .stripe_price_pro "$PRICE_FILE")

# List existing default configuration
CFG=$(stripe_api billing portal configurations list -d limit=1 | jq -r '.data[0].id // empty')
if [[ -z "$CFG" ]]; then
  echo "No portal configuration found; creating one..."
  stripe_api billing portal configurations create \
    -d "business_profile[headline]=Jobifai billing" \
    -d "features[subscription_cancel][enabled]=true" \
    -d "features[subscription_cancel][mode]=at_period_end" \
    -d "features[subscription_update][enabled]=true" \
    -d "features[subscription_update][default_allowed_updates][0]=price" \
    -d "features[subscription_update][proration_behavior]=none" \
    -d "features[payment_method_update][enabled]=true" \
    -d "features[invoice_history][enabled]=true" \
    -d "features[subscription_update][products][0][product]=$(stripe_api products list -d limit=100 | jq -r '.data[]|select(.metadata.jobifai_slug=="starter")|.id' | head -1)" \
    -d "features[subscription_update][products][0][prices][0]=$STARTER" \
    -d "features[subscription_update][products][0][prices][1]=$PRO" \
    >/dev/null
  echo "Created portal configuration."
else
  echo "Portal configuration already exists ($CFG). Verify in Dashboard: Settings → Billing → Customer portal."
  echo "Ensure Starter ↔ Pro price switch and cancel at period end are enabled."
fi
