#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
nginx_image="${NGINX_TEST_IMAGE:-nginx:1.27-alpine}"
tmp_dir="$(mktemp -d)"
containers=()

cleanup() {
  local container
  for container in "${containers[@]}"; do
    docker rm -f "$container" >/dev/null 2>&1 || true
  done
  if [[ -n "$tmp_dir" && -d "$tmp_dir" ]]; then
    rm -r -- "$tmp_dir"
  fi
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || {
  echo "docker is required for the payment checkout Nginx sentinel test" >&2
  exit 1
}
command -v curl >/dev/null 2>&1 || {
  echo "curl is required for the edge TLS payment checkout Nginx sentinel test" >&2
  exit 1
}
command -v openssl >/dev/null 2>&1 || {
  echo "openssl is required for the edge TLS payment checkout Nginx sentinel test" >&2
  exit 1
}
docker info >/dev/null 2>&1 || {
  echo "docker daemon is unavailable for the payment checkout Nginx sentinel test" >&2
  exit 1
}

bearer_token_sentinel="TASK4_BEARER_TOKEN_SENTINEL_NGINX"
token_hash_sentinel="TASK4_TOKEN_HASH_SENTINEL_NGINX"
qr_url_sentinel="TASK4_QR_URL_SENTINEL_NGINX"
provider_error_sentinel="TASK4_PROVIDER_ERROR_SENTINEL_NGINX"
referer_sentinel="TASK4_REFERER_SENTINEL_NGINX"
query_sentinel="TASK4_QUERY_SENTINEL_NGINX"
ordinary_error_marker="TASK4_ORDINARY_API_ERROR_MARKER"
sensitive_query="token_hash=${token_hash_sentinel}&code_url=https%3A%2F%2Fqr.invalid%2F${qr_url_sentinel}&provider_error=${provider_error_sentinel}&trace=${query_sentinel}"
static_asset_base_url="${HMAIGC_STATIC_ASSET_BASE_URL:-https://static.hm.kunagent.com/hmaigc/web}"
if [[ ! "$static_asset_base_url" =~ ^(https://[A-Za-z0-9.-]+(:[0-9]+)?)(/[^?#]*)?$ ]]; then
  echo "HMAIGC_STATIC_ASSET_BASE_URL 不是可用于 CSP 的 HTTPS 静态资源地址：${static_asset_base_url}" >&2
  exit 1
fi
static_asset_origin="${BASH_REMATCH[1]}"

printf '%s\n' \
  '[req]' \
  'distinguished_name = subject' \
  'prompt = no' \
  '[subject]' \
  'CN = aigc.example.com' >"$tmp_dir/openssl.cnf"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -config "$tmp_dir/openssl.cnf" \
  -keyout "$tmp_dir/aigc.example.com.key" \
  -out "$tmp_dir/aigc.example.com.crt" >/dev/null 2>&1

write_fixture_config() {
  local kind="$1"
  local mode="$2"
  local target="$3"
  if [[ "$mode" != "success" ]]; then
    : >"$target"
    return
  fi
  if [[ "$kind" == "inner" ]]; then
    printf '%s\n' \
      'server {' \
      '    listen 8080;' \
      '    server_name backend;' \
      '    access_log off;' \
      '    error_log /dev/null crit;' \
      '    add_header Cache-Control "public, max-age=86400";' \
      '    add_header Pragma "cache";' \
      '    add_header Referrer-Policy "unsafe-url";' \
      '    location / { return 204; }' \
      '}' >"$target"
    return
  fi
  printf '%s\n' \
    'server {' \
    '    listen 3000;' \
    '    server_name fixture;' \
    '    access_log off;' \
    '    error_log /dev/null crit;' \
    '    add_header Cache-Control "public, max-age=86400";' \
    '    add_header Pragma "cache";' \
    '    add_header Referrer-Policy "unsafe-url";' \
    '    location / { return 204; }' \
    '}' >"$target"
}

request_edge_with_headers() {
  local endpoint="$1"
  local path="$2"
  local output="$3"
  local request_host="${4:-aigc.example.com}"
  curl --insecure --silent --show-error --http1.1 --max-redirs 0 \
    --dump-header "$output" \
    --output /dev/null \
    --header "Host: ${request_host}" \
    --header "Referer: https://referrer.invalid/${referer_sentinel}" \
    "${endpoint}${path}" >/dev/null 2>&1 || true
}

run_case() {
  local kind="$1"
  local mode="$2"
  local config_path="$3"
  local listen_port="$4"
  local label="${kind}-${mode}"
  local container="hmaigc-task4-nginx-${$}-${label}"
  local fixture_config="$tmp_dir/${label}-fixture.conf"
  local docker_ports=()

  write_fixture_config "$kind" "$mode" "$fixture_config"
  printf '%s\n' '<!doctype html><title>checkout fixture</title>' >"$tmp_dir/${label}-index.html"

  if [[ "$kind" == "edge" ]]; then
    docker_ports=(-p 127.0.0.1::80 -p 127.0.0.1::443)
  else
    docker_ports=(-p 127.0.0.1::3000)
  fi
  docker run --rm -d \
    --name "$container" \
    --add-host backend:127.0.0.1 \
    "${docker_ports[@]}" \
    --entrypoint sh \
    "$nginx_image" -c 'rm -f /etc/nginx/conf.d/default.conf && sleep 300' >/dev/null
  containers+=("$container")
  docker cp "$config_path" "$container:/etc/nginx/conf.d/default.conf" >/dev/null
  docker cp "$fixture_config" "$container:/etc/nginx/conf.d/task4-fixture.conf" >/dev/null
  docker cp "$tmp_dir/${label}-index.html" "$container:/usr/share/nginx/html/index.html" >/dev/null
  if [[ "$kind" == "inner" && "$mode" == "failure" ]]; then
    MSYS_NO_PATHCONV=1 docker exec "$container" rm -f /usr/share/nginx/html/index.html
    if MSYS_NO_PATHCONV=1 docker exec "$container" test -e /usr/share/nginx/html/index.html; then
      echo "inner failure fixture retained index.html after removal" >&2
      exit 1
    fi
  fi
  docker exec "$container" sh -c 'mkdir -p /etc/letsencrypt/live/aigc.example.com'
  docker cp "$tmp_dir/aigc.example.com.crt" "$container:/etc/letsencrypt/live/aigc.example.com/fullchain.pem" >/dev/null
  docker cp "$tmp_dir/aigc.example.com.key" "$container:/etc/letsencrypt/live/aigc.example.com/privkey.pem" >/dev/null
  docker exec "$container" sh -c \
    'rm -f /var/log/nginx/access.log /var/log/nginx/error.log && touch /var/log/nginx/access.log /var/log/nginx/error.log'
  docker exec "$container" nginx -t >/dev/null
  docker exec "$container" nginx

  if [[ "$kind" == "inner" ]]; then
    local inner_address
    inner_address="$(docker port "$container" 3000/tcp | head -n 1)"
    request_edge_with_headers \
      "http://${inner_address}" \
      "/pay/${bearer_token_sentinel}?${sensitive_query}" \
      "$tmp_dir/${label}-pay.headers"
    request_edge_with_headers \
      "http://${inner_address}" \
      "/api/payments/checkout/${bearer_token_sentinel}/transactions?${sensitive_query}" \
      "$tmp_dir/${label}-api.headers"
    if [[ "$mode" == "success" ]]; then
      curl --silent --show-error --http1.1 \
        --dump-header "$tmp_dir/${label}-assets.spa-header" \
        --output "$tmp_dir/${label}-assets.spa-body" \
        "http://${inner_address}/assets/" || true
    fi
    curl --silent --show-error --http1.1 \
      --header "Referer: https://referrer.invalid/${referer_sentinel}" \
      --output /dev/null \
      "http://${inner_address}/api/TASK4_ACCESS_LOG_MARKER_${label}" || true
    if [[ "$mode" == "failure" ]]; then
      curl --silent --show-error --http1.1 \
        --header "Referer: https://referrer.invalid/${referer_sentinel}" \
        --output /dev/null \
        "http://${inner_address}/api/${ordinary_error_marker}_${kind}" || true
    fi
  else
    local http_address
    local https_address
    http_address="$(docker port "$container" 80/tcp | head -n 1)"
    https_address="$(docker port "$container" 443/tcp | head -n 1)"
		request_edge_with_headers \
			"http://${http_address}" \
			"/pay/${bearer_token_sentinel}?${sensitive_query}" \
			"$tmp_dir/${label}-http-pay.headers"
		request_edge_with_headers \
			"http://${http_address}" \
			"/api/payments/checkout/${bearer_token_sentinel}/transactions?${sensitive_query}" \
			"$tmp_dir/${label}-http-api.headers"
		request_edge_with_headers \
			"http://${http_address}" \
			"/pay/${bearer_token_sentinel}?${sensitive_query}" \
			"$tmp_dir/${label}-http-host-pay.headers" \
			"attacker.invalid"
		request_edge_with_headers \
			"https://${https_address}" \
			"/pay/${bearer_token_sentinel}?${sensitive_query}" \
			"$tmp_dir/${label}-pay.headers"
		request_edge_with_headers \
			"https://${https_address}" \
			"/api/payments/checkout/${bearer_token_sentinel}/transactions?${sensitive_query}" \
			"$tmp_dir/${label}-api.headers"
		curl --insecure --silent --show-error --http1.1 \
			--header 'Host: aigc.example.com' \
			--header "Referer: https://referrer.invalid/${referer_sentinel}" \
			--output /dev/null \
			"https://${https_address}/api/TASK4_ACCESS_LOG_MARKER_${label}" || true
		if [[ "$mode" == "failure" ]]; then
			curl --insecure --silent --show-error --http1.1 \
				--header 'Host: aigc.example.com' \
				--header "Referer: https://referrer.invalid/${referer_sentinel}" \
				--output /dev/null \
				"https://${https_address}/api/${ordinary_error_marker}_${kind}" || true
		fi
  fi

  sleep 0.2
  docker cp "$container:/var/log/nginx/access.log" "$tmp_dir/${label}.access.log" >/dev/null
  docker cp "$container:/var/log/nginx/error.log" "$tmp_dir/${label}.error.log" >/dev/null
  docker rm -f "$container" >/dev/null
}

run_case inner success "$repo_root/nginx.conf" 3000
run_case inner failure "$repo_root/nginx.conf" 3000
run_case edge success "$repo_root/deploy/nginx/hmaigc.conf.example" 80
run_case edge failure "$repo_root/deploy/nginx/hmaigc.conf.example" 80

failed=0
checkout_csp="default-src 'self'; base-uri 'self'; connect-src 'self' https: wss: blob: data:; font-src 'self' data: ${static_asset_origin}; form-action 'self'; frame-ancestors 'self'; img-src 'self' data: blob: https:; media-src 'self' data: blob: https:; object-src 'none'; script-src 'self' 'sha256-I6LPtG0ZaWWZjaqo01/h/CYOOBc9+Ljxd5XeZLu6aEI=' 'sha256-zf//CZlNtBsdfnnVuMsQm4ACjMfCcJk7E/v9zZbYc+A=' 'wasm-unsafe-eval' ${static_asset_origin}; style-src 'self' 'unsafe-inline' ${static_asset_origin}; worker-src 'self' blob: ${static_asset_origin}"
assert_single_header() {
  local headers="$1"
  local header_name="$2"
  local expected_value="$3"
  local values=()
  mapfile -t values < <(
    awk -v target="$header_name" '
      {
        line = $0
        sub(/\r$/, "", line)
        sub(/^[[:space:]]+/, "", line)
        name = line
        sub(/:.*/, "", name)
        if (tolower(name) == tolower(target)) {
          sub(/^[^:]+:[[:space:]]*/, "", line)
          print line
        }
      }
    ' "$headers"
  )
  if [[ "${#values[@]}" -ne 1 || "${values[0],,}" != "${expected_value,,}" ]]; then
    echo "$(basename "$headers") has ${header_name} values '${values[*]-}', want one exact '${expected_value}'" >&2
    failed=1
  fi
}

for headers in "$tmp_dir"/*.headers; do
  assert_single_header "$headers" "Cache-Control" "private, no-store"
  assert_single_header "$headers" "Pragma" "no-cache"
  assert_single_header "$headers" "Referrer-Policy" "no-referrer"
done

for headers in "$tmp_dir"/inner-*.headers; do
  assert_single_header "$headers" "Content-Security-Policy" "$checkout_csp"
done

if ! grep -Eq '^HTTP/[0-9.]+ 404([[:space:]]|$)' "$tmp_dir/inner-failure-pay.headers"; then
  inner_failure_status="$(head -n 1 "$tmp_dir/inner-failure-pay.headers" | tr -d '\r')"
  echo "inner /pay/ failure fixture did not exercise its local error path: ${inner_failure_status}" >&2
  failed=1
fi

if ! grep -Eq '^HTTP/[0-9.]+ 200([[:space:]]|$)' "$tmp_dir/inner-success-assets.spa-header" || \
  ! grep -Fq 'checkout fixture' "$tmp_dir/inner-success-assets.spa-body"; then
  assets_status="$(head -n 1 "$tmp_dir/inner-success-assets.spa-header" | tr -d '\r')"
  echo "inner /assets/ did not serve the SPA shell: ${assets_status}" >&2
  failed=1
fi

for headers in "$tmp_dir"/edge-*-http-pay.headers; do
  if ! grep -Eq '^HTTP/[0-9.]+ 308([[:space:]]|$)' "$headers"; then
    echo "$(basename "$headers") did not return the production HTTPS redirect" >&2
    failed=1
  fi
  assert_single_header \
    "$headers" \
    "Location" \
    "https://aigc.example.com/pay/${bearer_token_sentinel}?${sensitive_query}"
done
for headers in "$tmp_dir"/edge-*-http-api.headers; do
  if ! grep -Eq '^HTTP/[0-9.]+ 308([[:space:]]|$)' "$headers"; then
    echo "$(basename "$headers") did not return the production HTTPS redirect" >&2
    failed=1
  fi
  assert_single_header \
    "$headers" \
    "Location" \
    "https://aigc.example.com/api/payments/checkout/${bearer_token_sentinel}/transactions?${sensitive_query}"
done
for headers in "$tmp_dir"/edge-*-http-host-pay.headers; do
  if ! grep -Eq '^HTTP/[0-9.]+ 308([[:space:]]|$)' "$headers"; then
    echo "$(basename "$headers") did not return the canonical production HTTPS redirect" >&2
    failed=1
  fi
  assert_single_header \
    "$headers" \
    "Location" \
    "https://aigc.example.com/pay/${bearer_token_sentinel}?${sensitive_query}"
done

for kind in inner edge; do
  for mode in success failure; do
    access_log="$tmp_dir/${kind}-${mode}.access.log"
    if ! grep -Fq "TASK4_ACCESS_LOG_MARKER_${kind}-${mode}" "$access_log"; then
      echo "ordinary access logging is not observable in $(basename "$access_log")" >&2
      failed=1
    fi
  done
  proxy_error_log="$tmp_dir/${kind}-failure.error.log"
  if ! grep -Fq "${ordinary_error_marker}_${kind}" "$proxy_error_log"; then
    echo "ordinary /api upstream errors are not observable in $(basename "$proxy_error_log")" >&2
    failed=1
  fi
  if ! grep -Eq 'status=502 upstream_status=502' "$proxy_error_log"; then
    echo "ordinary /api safe proxy error lacks status facts in $(basename "$proxy_error_log")" >&2
    failed=1
  fi
done

for sentinel in \
  "$bearer_token_sentinel" \
  "$token_hash_sentinel" \
  "$qr_url_sentinel" \
  "$provider_error_sentinel" \
  "$referer_sentinel" \
  "$query_sentinel"; do
  if grep -RFq --include='*.log' "$sentinel" "$tmp_dir"; then
    echo "Nginx logs leaked sentinel: $sentinel" >&2
    grep -RFn --include='*.log' "$sentinel" "$tmp_dir" >&2 || true
    failed=1
  fi
done

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi

echo "payment checkout Nginx cache and log sentinels passed"
