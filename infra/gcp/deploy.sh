#!/usr/bin/env bash
# Build linux/amd64 image, push to Artifact Registry, roll out on jobifai-vm.
# Run from repo root:  ./infra/gcp/deploy.sh
# Or:                  make deploy-gcp
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GCP_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

if [[ -f "$GCP_DIR/deploy.env" ]]; then
  # shellcheck source=/dev/null
  source "$GCP_DIR/deploy.env"
fi

: "${GCP_PROJECT:=jobifai-494014}"
: "${GCP_ZONE:=australia-southeast1-a}"
: "${GCP_VM:=jobifai-vm}"
: "${GCP_REGION:=australia-southeast1}"
: "${IMAGE:=australia-southeast1-docker.pkg.dev/jobifai-494014/jobifai/jobifai:latest}"
: "${REMOTE_DIR:=/opt/jobifai}"

SYNC_CONFIG=0
SKIP_BUILD=0
SKIP_ROLLOUT=0
ALLOW_NON_MAIN=0

: "${DEPLOY_BRANCH:=main}"
: "${DEPLOY_REMOTE:=origin}"

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

  Full deploy (default): build amd64 image, push, sync config, restart on VM.
  The VM is started automatically if it is stopped or terminated.

  Builds always use the checked-out tree on branch "$DEPLOY_BRANCH", synced with
  $DEPLOY_REMOTE/$DEPLOY_BRANCH (merge feature work to main before deploying).

Options:
  --sync-config    Copy docker-compose.yml + Caddyfile to VM (also done by default)
  --no-sync-config Skip copying compose/Caddyfile
  --build-only     Build and push image only
  --rollout-only   Pull and restart on VM only (no build; skips git checks)
  --allow-non-main Build from current branch (escape hatch; not for routine prod)
  -h, --help       Show this help

Config: $GCP_DIR/deploy.env (optional)
EOF
}

ensure_prod_git() {
  if ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "error: not a git repository" >&2
    exit 1
  fi
  if [[ -n "$(git status --porcelain)" ]]; then
    echo "error: uncommitted changes — commit or stash before prod deploy" >&2
    exit 1
  fi
  local branch
  branch="$(git rev-parse --abbrev-ref HEAD)"
  if [[ "$ALLOW_NON_MAIN" -eq 0 && "$branch" != "$DEPLOY_BRANCH" ]]; then
    echo "error: prod deploy must run on $DEPLOY_BRANCH (current: $branch)" >&2
    echo "  git checkout $DEPLOY_BRANCH && git pull $DEPLOY_REMOTE $DEPLOY_BRANCH" >&2
    exit 1
  fi
  if [[ "$ALLOW_NON_MAIN" -eq 0 ]]; then
    echo "==> Verifying $DEPLOY_BRANCH matches $DEPLOY_REMOTE/$DEPLOY_BRANCH"
    git fetch "$DEPLOY_REMOTE" "$DEPLOY_BRANCH"
    local local_sha remote_sha
    local_sha="$(git rev-parse HEAD)"
    remote_sha="$(git rev-parse "$DEPLOY_REMOTE/$DEPLOY_BRANCH")"
    if [[ "$local_sha" != "$remote_sha" ]]; then
      echo "error: $DEPLOY_BRANCH is not synced with $DEPLOY_REMOTE/$DEPLOY_BRANCH" >&2
      echo "  local:  $local_sha ($(git log -1 --format=%s HEAD))" >&2
      echo "  remote: $remote_sha ($(git log -1 --format=%s "$DEPLOY_REMOTE/$DEPLOY_BRANCH"))" >&2
      echo "  git pull $DEPLOY_REMOTE $DEPLOY_BRANCH" >&2
      exit 1
    fi
    echo "==> Deploying commit $local_sha ($(git log -1 --format=%s HEAD))"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --sync-config) SYNC_CONFIG=1; shift ;;
    --no-sync-config) SYNC_CONFIG=0; shift ;;
    --build-only) SKIP_ROLLOUT=1; shift ;;
    --rollout-only) SKIP_BUILD=1; shift ;;
    --allow-non-main) ALLOW_NON_MAIN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage; exit 1 ;;
  esac
done

# Default: sync compose files on full deploy
if [[ "$SKIP_BUILD" -eq 0 && "$SKIP_ROLLOUT" -eq 0 && "$SYNC_CONFIG" -eq 0 ]]; then
  SYNC_CONFIG=1
fi

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

need gcloud
need docker
need git

gcloud config set project "$GCP_PROJECT" >/dev/null

if [[ "$SKIP_BUILD" -eq 0 ]]; then
  ensure_prod_git
  echo "==> Building and pushing $IMAGE (linux/amd64)"
  gcloud auth configure-docker "${GCP_REGION}-docker.pkg.dev" --quiet
  if docker buildx inspect jobifai-builder >/dev/null 2>&1; then
    docker buildx use jobifai-builder
  else
    docker buildx create --name jobifai-builder --use
  fi
  docker buildx build --platform linux/amd64 -t "$IMAGE" --push .
  echo "==> Push complete"
fi

if [[ "$SKIP_ROLLOUT" -eq 0 ]]; then
  # Ensure VM is running (start it if stopped; no-op if already running).
  VM_STATUS="$(gcloud compute instances describe "$GCP_VM" --zone="$GCP_ZONE" --format="value(status)" 2>/dev/null || true)"
  if [[ "$VM_STATUS" == "TERMINATED" || "$VM_STATUS" == "STOPPED" ]]; then
    echo "==> VM is $VM_STATUS — starting $GCP_VM"
    gcloud compute instances start "$GCP_VM" --zone="$GCP_ZONE"
    echo "==> Waiting for SSH to become available..."
    for i in $(seq 1 18); do
      if gcloud compute ssh "$GCP_VM" --zone="$GCP_ZONE" \
          --ssh-flag="-o ConnectTimeout=5 -o StrictHostKeyChecking=no" \
          --command="true" 2>/dev/null; then
        echo "==> VM is ready"
        break
      fi
      echo "    attempt $i/18 — retrying in 10s..."
      sleep 10
    done
  fi

  if [[ "$SYNC_CONFIG" -eq 1 ]]; then
    echo "==> Syncing compose + Caddyfile to $GCP_VM:$REMOTE_DIR"
    gcloud compute scp --zone="$GCP_ZONE" \
      "$GCP_DIR/docker-compose.yml" \
      "$GCP_DIR/Caddyfile" \
      "${GCP_VM}:${REMOTE_DIR}/"
    echo "==> Syncing observability config to $GCP_VM:$REMOTE_DIR/observability"
    gcloud compute scp --recurse --zone="$GCP_ZONE" \
      "$GCP_DIR/observability" \
      "${GCP_VM}:${REMOTE_DIR}/"
  fi

  echo "==> Rolling out on $GCP_VM"
  TOKEN="$(gcloud auth print-access-token)"
  # Single --command string (heredoc + "bash -s" breaks gcloud flag parsing).
  gcloud compute ssh "$GCP_VM" --zone="$GCP_ZONE" --command="set -euo pipefail
echo '${TOKEN}' | docker login -u oauth2accesstoken --password-stdin https://${GCP_REGION}-docker.pkg.dev
cd '${REMOTE_DIR}'
docker compose pull
docker compose up -d
docker compose ps"
  echo "==> Done. https://jobifai.com.au"
fi
