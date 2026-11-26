#!/usr/bin/env bash
# One build identity for native binaries, local development, and Docker images.
set -euo pipefail
JOBIFAI_BUILD_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$JOBIFAI_BUILD_ROOT"

JOBIFAI_BUILD_GIT_SHA="${JOBIFAI_BUILD_GIT_SHA:-$(git rev-parse HEAD 2>/dev/null || printf unknown)}"
JOBIFAI_BUILD_VERSION="${JOBIFAI_BUILD_VERSION:-$(git describe --tags --always --abbrev=12 2>/dev/null || printf dev)}"
JOBIFAI_BUILD_TIME="${JOBIFAI_BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
if [[ -z "${JOBIFAI_SOURCE_STATE:-}" ]]; then
    if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
        if [[ -z "$(git status --porcelain --untracked-files=normal)" ]]; then
            JOBIFAI_SOURCE_STATE=clean
        else
            JOBIFAI_SOURCE_STATE=modified
        fi
    else
        JOBIFAI_SOURCE_STATE=unknown
    fi
fi
if [[ "$JOBIFAI_SOURCE_STATE" == modified && "$JOBIFAI_BUILD_VERSION" != *-dirty ]]; then
    JOBIFAI_BUILD_VERSION+=-dirty
fi

# Go's linker flags contain whitespace-separated assignments. Reject characters
# that could split a value; all normal release tags and commit IDs are supported.
for value in "$JOBIFAI_BUILD_VERSION" "$JOBIFAI_BUILD_GIT_SHA" "$JOBIFAI_BUILD_TIME" "$JOBIFAI_SOURCE_STATE"; do
    if [[ -z "$value" || "$value" == *[!a-zA-Z0-9._+/:-]* ]]; then
        printf 'Invalid build metadata: %s\n' "$value" >&2
        exit 1
    fi
done

JOBIFAI_VERSION_PACKAGE=github.com/user/jobifai/internal/version
JOBIFAI_BUILD_LDFLAGS="-s -w -X $JOBIFAI_VERSION_PACKAGE.Version=$JOBIFAI_BUILD_VERSION -X $JOBIFAI_VERSION_PACKAGE.GitSHA=$JOBIFAI_BUILD_GIT_SHA -X $JOBIFAI_VERSION_PACKAGE.BuildTime=$JOBIFAI_BUILD_TIME -X $JOBIFAI_VERSION_PACKAGE.SourceState=$JOBIFAI_SOURCE_STATE"
JOBIFAI_DOCKER_ARGS=(
    --build-arg "BUILD_VERSION=$JOBIFAI_BUILD_VERSION"
    --build-arg "BUILD_GIT_SHA=$JOBIFAI_BUILD_GIT_SHA"
    --build-arg "BUILD_TIMESTAMP=$JOBIFAI_BUILD_TIME"
    --build-arg "BUILD_SOURCE_STATE=$JOBIFAI_SOURCE_STATE"
)

mode="${1:-metadata}"
if [[ $# -gt 0 ]]; then shift; fi
case "$mode" in
    metadata)
        printf 'Version: %s\nCommit: %s\nBuilt (UTC): %s\nSource: %s\n' "$JOBIFAI_BUILD_VERSION" "$JOBIFAI_BUILD_GIT_SHA" "$JOBIFAI_BUILD_TIME" "$JOBIFAI_SOURCE_STATE"
        ;;
    go)
        command="${1:?expected build or run}"; shift
        exec go "$command" -ldflags "$JOBIFAI_BUILD_LDFLAGS" "$@"
        ;;
    docker) exec docker build "${JOBIFAI_DOCKER_ARGS[@]}" "$@" ;;
    buildx) exec docker buildx build "${JOBIFAI_DOCKER_ARGS[@]}" "$@" ;;
    *) printf 'Usage: bash scripts/build.sh {metadata|go build|go run|docker|buildx} [arguments]\n' >&2; exit 1 ;;
esac
