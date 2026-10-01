#!/usr/bin/env bash
# Idempotent Stripe Test Mode catalog for Jobifai E2E (AUD).
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"
load_env
require_jq

META_KEY="jobifai_test_catalog"
META_VAL="2026-01"

find_product() {
  local slug=$1
  stripe_api products list -d limit=100 | jq -r --arg s "$slug" \
    '.data[] | select(.metadata["'"$META_KEY"'"]=="'"$META_VAL"'" and .metadata["jobifai_slug"]==$s) | .id' | head -1
}

ensure_product() {
  local slug=$1 name=$2
  local id
  id=$(find_product "$slug" || true)
  if [[ -n "$id" ]]; then
    echo "$id"
    return
  fi
  stripe_api products create \
    -d "name=$name" \
    -d "metadata[$META_KEY]=$META_VAL" \
    -d "metadata[jobifai_slug]=$slug" | jq -r .id
}

find_price() {
  local product=$1 amount=$2 recurring=${3:-}
  stripe_api prices list -d "product=$product" -d limit=100 -d active=true | jq -r --arg a "$amount" --arg r "$recurring" '
    .data[] | select(.unit_amount|tostring==$a) |
    select(if $r=="" then .type=="one_time" else (.recurring!=null and .recurring.interval==$r) end) |
    .id' | head -1
}

ensure_price() {
  local product=$1 amount_cents=$2 currency=$3 slug=$4
  local recurring=${5:-}
  local id
  id=$(find_price "$product" "$amount_cents" "$recurring" || true)
  if [[ -n "$id" ]]; then
    echo "$id"
    return
  fi
  if [[ -n "$recurring" ]]; then
    stripe_api prices create \
      -d "product=$product" \
      -d "unit_amount=$amount_cents" \
      -d "currency=$currency" \
      -d "recurring[interval]=$recurring" \
      -d "metadata[$META_KEY]=$META_VAL" \
      -d "metadata[jobifai_slug]=$slug" | jq -r .id
  else
    stripe_api prices create \
      -d "product=$product" \
      -d "unit_amount=$amount_cents" \
      -d "currency=$currency" \
      -d "metadata[$META_KEY]=$META_VAL" \
      -d "metadata[jobifai_slug]=$slug" | jq -r .id
  fi
}

mkdir -p "$(dirname "$PRICE_FILE")"

p_starter=$(ensure_product starter "Jobifai Starter")
p_pro=$(ensure_product pro "Jobifai Pro")
p_top1=$(ensure_product topup_1000 "Jobifai 1,000 Credit Top-up")
p_top25=$(ensure_product topup_2500 "Jobifai 2,500 Credit Top-up")
p_top5=$(ensure_product topup_5000 "Jobifai 5,000 Credit Top-up")

price_starter=$(ensure_price "$p_starter" 1900 aud starter month)
price_pro=$(ensure_price "$p_pro" 3900 aud pro month)
price_top_1000=$(ensure_price "$p_top1" 790 aud topup_1000)
price_top_2500=$(ensure_price "$p_top25" 1790 aud topup_2500)
price_top_5000=$(ensure_price "$p_top5" 2990 aud topup_5000)

jq -n \
  --arg starter "$price_starter" --arg pro "$price_pro" \
  --arg t1 "$price_top_1000" --arg t25 "$price_top_2500" --arg t5 "$price_top_5000" \
  '{stripe_price_starter:$starter, stripe_price_pro:$pro, top_up_packs:[
    {credits:1000, stripe_price_id:$t1, label:"1,000 credits"},
    {credits:2500, stripe_price_id:$t25, label:"2,500 credits"},
    {credits:5000, stripe_price_id:$t5, label:"5,000 credits"}
  ]}' > "$PRICE_FILE"

echo "Wrote price IDs to data/stripe-test-price-ids.json (not printed here)."
