#!/usr/bin/env bash
# Non-destructive API smoke checks (server must be running with Stripe env).
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"
require_jq

echo "=== GET /api/public/plans ==="
curl -sfS http://localhost:8081/api/public/plans | jq .

TOKEN=$(admin_token)
echo "=== GET /api/admin/billing/summary ==="
curl -sfS -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/admin/billing/summary | jq .

echo "=== Register fresh trial user ==="
EMAIL="stripe-e2e-$(date +%s)@example.com"
REG=$(curl -sfS -X POST http://localhost:8081/auth/register \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"password123\",\"display_name\":\"E2E\"}")
USER_TOKEN=$(echo "$REG" | jq -r .access_token)
echo "email=$EMAIL"

echo "=== GET /api/quota/status ==="
curl -sfS -H "Authorization: Bearer $USER_TOKEN" http://localhost:8081/api/quota/status | jq .

echo "=== POST /api/billing/topup (expect 409 trial) ==="
curl -sS -o /tmp/topup.json -w "HTTP %{http_code}\n" -X POST http://localhost:8081/api/billing/topup \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"credits":1000}'
cat /tmp/topup.json | jq .

echo "Smoke complete. Use USER_TOKEN for checkout E2E: export E2E_USER_TOKEN=$USER_TOKEN"
