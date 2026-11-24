#!/usr/bin/env bash
# Build the two images for linux/amd64 and linux/arm64, smoke-test them with
# deploy/compose.yaml, and push them to Docker Hub. Needs `docker login` first.
# Never creates or pushes a git tag.
#
#   scripts/release.sh <semver>
#   IMAGE_REPO=you/agent-verdicts scripts/release.sh <semver>   # push elsewhere
set -euo pipefail

version="${1:-}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "usage: $0 <semver>" >&2; exit 2; }
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
# The version is written in three places; refuse to release when they disagree.
for f in apps/ui/package.json apps/api/openapi.yaml apps/api/internal/adapter/mcp/server.go; do
  grep -qE "[\" ]${version//./\\.}(\"|\$)" "$f" || { echo "$f does not carry version $version" >&2; exit 1; }
done
[[ -z "$(git status --porcelain)" ]] || { echo "git tree is not clean" >&2; exit 1; }
! git rev-parse -q --verify "refs/tags/v$version" >/dev/null || { echo "tag v$version already exists" >&2; exit 1; }

repo="${IMAGE_REPO:-nctins/agent-verdicts}"
platforms="linux/amd64,linux/arm64"
builder=agentverdicts
docker buildx inspect "$builder" >/dev/null 2>&1 || docker buildx create --name "$builder" --driver docker-container >/dev/null

# Pre-releases (a "-" in the version) never move :latest.
tags() { echo "-t $1:$version"; [[ "$version" == *-* ]] || echo "-t $1:latest"; }
build() { # extra buildx args...
  # shellcheck disable=SC2046
  docker buildx build --builder "$builder" "$@" $(tags "$repo") \
    -f docker/api/Dockerfile --target production apps/api
  # shellcheck disable=SC2046
  docker buildx build --builder "$builder" "$@" $(tags "$repo-web") \
    -f docker/ui/Dockerfile --target production --build-context dockerui=docker/ui apps/ui
}

# 1. This machine's architecture only, loaded locally, for the smoke test.
build --load

work="$(mktemp -d)"
project="avrelease$$"
cleanup() {
  docker compose -p "$project" --env-file "$work/.env" -f deploy/compose.yaml down -v >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
printf 'IMAGE_REPO=%s\nVERSION=%s\nPOSTGRES_PASSWORD=release%s\nPORT=18091\n' "$repo" "$version" "$RANDOM" >"$work/.env"
docker compose -p "$project" --env-file "$work/.env" -f deploy/compose.yaml up -d
for _ in $(seq 60); do
  curl -fsS -o /dev/null http://localhost:18091/api/v1/session && { ok=1; break; }
  sleep 2
done
[[ "${ok:-}" == 1 ]] || { docker compose -p "$project" --env-file "$work/.env" -f deploy/compose.yaml logs; echo "smoke test failed" >&2; exit 1; }
echo "smoke test ok"

# 2. Both architectures, pushed (reuses the cache of step 1).
build --platform "$platforms" --push
echo "pushed $repo:$version and $repo-web:$version ($platforms)"
echo "next: git tag v$version && git push origin v$version   (not done by this script)"
