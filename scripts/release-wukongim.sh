#!/usr/bin/env bash
set -Eeuo pipefail

image=""
allow_production=false
dry_run=false
compose_file="${RELEASE_WUKONGIM_COMPOSE_FILE:-/opt/mall/docker-compose-im-server2.yml}"
service="${RELEASE_WUKONGIM_SERVICE:-wukongim}"
container="${RELEASE_WUKONGIM_CONTAINER:-mall-wukongim}"
health_url="${RELEASE_WUKONGIM_HEALTH_URL:-http://127.0.0.1:5001/health}"
release_dir="${RELEASE_WUKONGIM_RELEASE_DIR:-/opt/mall/releases/wukongim}"
health_timeout_sec="${RELEASE_WUKONGIM_HEALTH_TIMEOUT_SEC:-90}"

usage() {
  cat <<'USAGE'
Usage: scripts/release-wukongim.sh --image <repo:sha-tag> [options]

Options:
  --allow-production       Required before changing the production container
  --dry-run                Validate and print the resolved release contract
  --compose-file <path>    Base production compose file
  --service <name>         Compose service name
  --container <name>       Runtime container name
  --health-url <url>       Host-local health endpoint
  --release-dir <path>     Override and status directory
USAGE
}

fail() {
  printf 'release-wukongim: %s\n' "$*" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --image)
      image="${2:-}"
      shift 2
      ;;
    --allow-production)
      allow_production=true
      shift
      ;;
    --dry-run)
      dry_run=true
      shift
      ;;
    --compose-file)
      compose_file="${2:-}"
      shift 2
      ;;
    --service)
      service="${2:-}"
      shift 2
      ;;
    --container)
      container="${2:-}"
      shift 2
      ;;
    --health-url)
      health_url="${2:-}"
      shift 2
      ;;
    --release-dir)
      release_dir="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[[ "$image" =~ ^[a-zA-Z0-9._/-]+:sha-[0-9a-f]{12,40}$ ]] ||
  fail "image must use an immutable sha-* tag"
[[ "$compose_file" == /* ]] || fail "compose file must be an absolute path"
[[ "$release_dir" == /* ]] || fail "release directory must be an absolute path"
[[ "$service" =~ ^[a-zA-Z0-9._-]+$ ]] || fail "invalid compose service"
[[ "$container" =~ ^[a-zA-Z0-9._-]+$ ]] || fail "invalid container name"
[[ "$health_url" == http://127.0.0.1:*/* ]] ||
  fail "health URL must target host-local 127.0.0.1"
[[ "$health_timeout_sec" =~ ^[0-9]+$ ]] || fail "health timeout must be numeric"

printf 'image=%s\ncompose_file=%s\nservice=%s\ncontainer=%s\nhealth_url=%s\nrelease_dir=%s\n' \
  "$image" "$compose_file" "$service" "$container" "$health_url" "$release_dir"

if [[ "$dry_run" == true ]]; then
  exit 0
fi

[[ "$allow_production" == true ]] ||
  fail "production deployment is blocked; pass --allow-production explicitly"
[[ -r "$compose_file" ]] || fail "compose file is not readable: $compose_file"
command -v docker >/dev/null 2>&1 || fail "docker is required"
docker compose version >/dev/null 2>&1 || fail "docker compose v2 is required"
docker image inspect "$image" >/dev/null 2>&1 || fail "image is not loaded: $image"

previous_image="$(
  docker inspect "$container" --format '{{.Config.Image}}' 2>/dev/null
)" || fail "cannot inspect current container: $container"
[[ -n "$previous_image" ]] || fail "current container image is empty"

if [[ "$previous_image" == "$image" ]]; then
  printf 'already deployed: %s\n' "$image"
  exit 0
fi

mkdir -p "$release_dir"
override_file="$release_dir/docker-compose.release.yml"
status_file="$release_dir/status.env"
override_tmp="$(mktemp "$release_dir/docker-compose.release.yml.XXXXXX")"

printf 'services:\n  %s:\n    image: ${RELEASE_WUKONGIM_IMAGE:?RELEASE_WUKONGIM_IMAGE is required}\n' \
  "$service" > "$override_tmp"
mv "$override_tmp" "$override_file"

compose=(docker compose -f "$compose_file" -f "$override_file")

write_status() {
  local status="$1"
  local active_image="$2"
  local failed_phase="${3:-}"
  local status_tmp
  status_tmp="$(mktemp "$release_dir/status.env.XXXXXX")"
  {
    printf 'STATUS=%q\n' "$status"
    printf 'IMAGE=%q\n' "$active_image"
    printf 'PREVIOUS_IMAGE=%q\n' "$previous_image"
    printf 'FAILED_PHASE=%q\n' "$failed_phase"
    printf 'UPDATED_AT=%q\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$status_tmp"
  mv "$status_tmp" "$status_file"
}

rollback() {
  printf 'rolling back to %s\n' "$previous_image" >&2
  if RELEASE_WUKONGIM_IMAGE="$previous_image" \
    "${compose[@]}" up -d --no-deps --force-recreate "$service"; then
    write_status rollback "$previous_image" health
  else
    write_status rollback_failed "$previous_image" rollback
  fi
}

write_status deploying "$image"
if ! RELEASE_WUKONGIM_IMAGE="$image" \
  "${compose[@]}" up -d --no-deps --force-recreate "$service"; then
  write_status failed "$previous_image" deploy
  rollback
  fail "compose deployment failed"
fi

deadline=$((SECONDS + health_timeout_sec))
while ((SECONDS < deadline)); do
  running="$(docker inspect "$container" --format '{{.State.Running}}' 2>/dev/null || true)"
  if [[ "$running" == true ]] && curl -fsS --max-time 3 "$health_url" >/dev/null; then
    active_image="$(docker inspect "$container" --format '{{.Config.Image}}')"
    if [[ "$active_image" != "$image" ]]; then
      rollback
      fail "healthy container uses unexpected image: $active_image"
    fi
    write_status success "$active_image"
    printf 'release succeeded: %s\n' "$active_image"
    exit 0
  fi
  sleep 3
done

write_status failed "$image" health
rollback
fail "health check timed out after ${health_timeout_sec}s"
