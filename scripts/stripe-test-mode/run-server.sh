#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"
load_env
export JOBIFAI_ENV="${JOBIFAI_ENV:-development}"
export APP_BASE_URL="${APP_BASE_URL:-http://localhost:8081}"
exec "$ROOT/jobifai" -addr :8081
