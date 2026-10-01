#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"
load_env

EVENTS="checkout.session.completed,customer.subscription.created,customer.subscription.updated,customer.subscription.deleted,invoice.paid,invoice.payment_failed"

echo "Starting Stripe CLI listener → http://localhost:8081/api/billing/webhook"
echo "Copy the whsec_... signing secret into data/stripe-test-mode.local.env as STRIPE_WEBHOOK_SECRET (do not commit)."

exec stripe listen --api-key "$STRIPE_SECRET_KEY" --forward-to localhost:8081/api/billing/webhook --events "$EVENTS"
