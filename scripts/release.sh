#!/usr/bin/env bash
# Build the three images for this machine's architecture, smoke-test them with
# deploy/compose.yaml, and pack them for `docker load`. Never tags or pushes.
set -euo pipefail

version="${1:-}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "usage: $0 <semver>" >&2; exit 2; }
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
# The version is written in three places; refuse to pack a release where they disagree.
for f in apps/ui/package.json apps/api/openapi.yaml apps/api/internal/adapter/mcp/server.go; do
  grep -qE "[\" ]${version//./\\.}(\"|\$)" "$f" || { echo "$f does not carry version $version" >&2; exit 1; }
done
[[ -z "$(git status --porcelain)" ]] || { echo "git tree is not clean" >&2; exit 1; }
! git rev-parse -q --verify "refs/tags/v$version" >/dev/null || { echo "tag v$version already exists" >&2; exit 1; }

prefix="${IMAGE_PREFIX:-}"
arch="$(docker version --format '{{.Server.Arch}}')"
build() { # name, target-args...
  local name="$1"; shift
  docker build "$@" -t "${prefix}agentverdicts-$name:$version" -t "${prefix}agentverdicts-$name:latest"
}
build api -f docker/api/Dockerfile --target production apps/api
build ui -f docker/ui/Dockerfile --target production --build-context dockerui=docker/ui apps/ui
build db docker/db

project="avrelease$$"
work="$(mktemp -d)"
cleanup() {
  docker compose -p "$project" --env-file "$work/.env" -f deploy/compose.yaml down -v >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
port=18$((RANDOM % 900 + 100))
printf 'IMAGE_PREFIX=%s\nVERSION=%s\nPOSTGRES_PASSWORD=smoke%s\nPORT=%s\n' "$prefix" "$version" "$RANDOM" "$port" >"$work/.env"
docker compose -p "$project" --env-file "$work/.env" -f deploy/compose.yaml up -d
for _ in $(seq 1 60); do
  if curl -fsS "http://localhost:$port/api/v1/session" >/dev/null 2>&1; then ok=1; break; fi
  sleep 2
done
[[ "${ok:-}" == 1 ]] || { docker compose -p "$project" --env-file "$work/.env" -f deploy/compose.yaml logs; echo "smoke test failed" >&2; exit 1; }
echo "smoke test ok"

mkdir -p dist
out="dist/agentverdicts-$version-$arch"
rm -rf "$out" && mkdir -p "$out"
docker save -o "$out/images.tar" "${prefix}agentverdicts-api:$version" "${prefix}agentverdicts-ui:$version" "${prefix}agentverdicts-db:$version"
cp deploy/compose.yaml deploy/README.md "$out/"
# The packed .env.example names the prefix these images were tagged with.
sed "s|^IMAGE_PREFIX=.*|IMAGE_PREFIX=$prefix|" deploy/.env.example >"$out/.env.example"
tar -C dist -czf "$out.tar.gz" "$(basename "$out")"
rm -rf "$out"
echo "wrote $out.tar.gz"
echo "next: git tag v$version && git push origin v$version   (not done by this script)"
