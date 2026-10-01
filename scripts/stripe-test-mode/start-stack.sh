#!/usr/bin/env bash
# Start stripe listen, sync whsec to local env, then Jobifai (Test Mode).
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"
load_env
export JOBIFAI_ENV="${JOBIFAI_ENV:-development}"

ROOT="$(cd "$DIR/../.." && pwd)"
LISTEN_LOG="$ROOT/data/stripe-listen.log"
SERVER_LOG="$ROOT/data/jobifai-server.log"
mkdir -p "$ROOT/data"

lsof -i :8081 -t 2>/dev/null | xargs kill -9 2>/dev/null || true
pkill -f 'stripe listen.*8081/api/billing/webhook' 2>/dev/null || true
sleep 1

EVENTS="checkout.session.completed,customer.subscription.created,customer.subscription.updated,customer.subscription.deleted,invoice.paid,invoice.payment_failed"
nohup stripe listen --api-key "$STRIPE_SECRET_KEY" --forward-to localhost:8081/api/billing/webhook --events "$EVENTS" >"$LISTEN_LOG" 2>&1 &
LISTEN_PID=$!

for _ in $(seq 1 30); do
  if grep -q 'whsec_' "$LISTEN_LOG" 2>/dev/null; then
    break
  fi
  sleep 1
done
python3 <<PY
import re
from pathlib import Path
log = Path("$LISTEN_LOG").read_text()
m = re.search(r'whsec_[A-Za-z0-9]+', log)
if not m:
    raise SystemExit('stripe listen did not print whsec')
whsec = m.group(0)
env = Path("$ROOT/data/stripe-test-mode.local.env")
lines = []
for line in env.read_text().splitlines():
    if line.strip().startswith('STRIPE_WEBHOOK_SECRET='):
        lines.append('STRIPE_WEBHOOK_SECRET=' + whsec)
    else:
        lines.append(line)
if not any(l.startswith('STRIPE_WEBHOOK_SECRET=') for l in lines):
    lines.append('STRIPE_WEBHOOK_SECRET=' + whsec)
env.write_text('\\n'.join(lines) + '\\n')
PY

# shellcheck disable=SC1090
set -a && source "$ENV_FILE" && set +a
nohup env STRIPE_SECRET_KEY="$STRIPE_SECRET_KEY" STRIPE_WEBHOOK_SECRET="$STRIPE_WEBHOOK_SECRET" APP_BASE_URL="${APP_BASE_URL:-http://localhost:8081}" JOBIFAI_ENV="$JOBIFAI_ENV" \
  "$ROOT/jobifai" -addr :8081 >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 20); do
  if curl -sfS "http://localhost:8081/api/public/plans" >/dev/null 2>&1; then
    echo "stack_ready listen_pid=$LISTEN_PID server_pid=$SERVER_PID"
    exit 0
  fi
  sleep 1
done
echo "stack failed: server did not become ready" >&2
exit 1
