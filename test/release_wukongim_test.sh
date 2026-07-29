#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_script="$repo_root/scripts/release-wukongim.sh"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

fail() {
  printf 'release_wukongim_test: %s\n' "$*" >&2
  exit 1
}

mkdir -p "$test_root/bin" "$test_root/release"
compose_file="$test_root/docker-compose.yml"
active_image_file="$test_root/active-image"
printf 'services:\n  wukongim:\n    image: xigfor/wukongim:sha-aaaaaaaaaaaa\n' > "$compose_file"
printf 'xigfor/wukongim:sha-aaaaaaaaaaaa\n' > "$active_image_file"

cat > "$test_root/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  compose)
    if [[ "${2:-}" == version ]]; then
      exit 0
    fi
    printf '%s\n' "$RELEASE_WUKONGIM_IMAGE" > "$FAKE_ACTIVE_IMAGE_FILE"
    exit 0
    ;;
  image)
    [[ "${2:-}" == inspect ]]
    exit 0
    ;;
  inspect)
    if [[ "$*" == *'{{.Config.Image}}'* ]]; then
      cat "$FAKE_ACTIVE_IMAGE_FILE"
    elif [[ "$*" == *'{{.State.Running}}'* ]]; then
      printf 'true\n'
    fi
    exit 0
    ;;
esac

exit 1
DOCKER

cat > "$test_root/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
[[ "${FAKE_CURL_FAIL:-false}" != true ]]
CURL

chmod 755 "$test_root/bin/docker" "$test_root/bin/curl"
export PATH="$test_root/bin:$PATH"
export FAKE_ACTIVE_IMAGE_FILE="$active_image_file"

if "$release_script" \
  --image xigfor/wukongim:latest \
  --dry-run >/dev/null 2>&1; then
  fail "mutable image tag was accepted"
fi

"$release_script" \
  --image xigfor/wukongim:sha-bbbbbbbbbbbb \
  --allow-production \
  --compose-file "$compose_file" \
  --service wukongim \
  --container mall-wukongim \
  --health-url http://127.0.0.1:5001/health \
  --release-dir "$test_root/release"

grep -q '^STATUS=success$' "$test_root/release/status.env" ||
  fail "success status was not recorded"
grep -q '^xigfor/wukongim:sha-bbbbbbbbbbbb$' "$active_image_file" ||
  fail "new image was not activated"

export FAKE_CURL_FAIL=true
export RELEASE_WUKONGIM_HEALTH_TIMEOUT_SEC=0
if "$release_script" \
  --image xigfor/wukongim:sha-cccccccccccc \
  --allow-production \
  --compose-file "$compose_file" \
  --service wukongim \
  --container mall-wukongim \
  --health-url http://127.0.0.1:5001/health \
  --release-dir "$test_root/release"; then
  fail "failed health check unexpectedly succeeded"
fi

grep -q '^STATUS=rollback$' "$test_root/release/status.env" ||
  fail "rollback status was not recorded"
grep -q '^xigfor/wukongim:sha-bbbbbbbbbbbb$' "$active_image_file" ||
  fail "previous image was not restored"

printf 'release_wukongim_test: PASS\n'
