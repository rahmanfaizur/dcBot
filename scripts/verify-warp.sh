#!/usr/bin/env bash
# Safe WARP check for the bot VM. Uses only Docker — never installs warp-cli on the host.
# Run from the VM after: docker compose --profile warp up -d
set -euo pipefail

PROXY="${YTDLP_PROXY:-socks5://127.0.0.1:1080}"
PORT="${WARP_LOCAL_PORT:-1080}"
TEST_URL="${1:-https://www.youtube.com/watch?v=kJQP7kOUV4w}"

docker_cmd() {
  if docker "$@" 2>/dev/null; then
    return 0
  fi
  sudo docker "$@"
}

echo "=== 1) SSH sanity (you are connected — good) ==="
echo "host: $(hostname)  time: $(date -Is)"

echo
echo "=== 2) Direct egress IP (should stay your Azure IP) ==="
direct_ip=$(curl -4 -s --max-time 10 https://api.ipify.org || true)
echo "direct: ${direct_ip:-FAILED}"

echo
echo "=== 3) WARP container running? ==="
if ! docker_cmd ps --format '{{.Names}}' | grep -q '^mybot-warp$'; then
  echo "FAIL: mybot-warp container is not running"
  echo "Start it with: docker compose --profile warp up -d"
  exit 1
fi
docker_cmd ps --filter name=mybot-warp --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'

echo
echo "=== 4) Proxy egress IP (should NOT be ${direct_ip}) ==="
warp_trace=$(curl -4 -s --max-time 20 --proxy "socks5h://127.0.0.1:${PORT}" https://cloudflare.com/cdn-cgi/trace || true)
if [[ -z "$warp_trace" ]]; then
  echo "FAIL: cannot reach Cloudflare through SOCKS proxy on 127.0.0.1:${PORT}"
  echo "Check: docker logs mybot-warp --tail 50"
  exit 1
fi
echo "$warp_trace"
warp_ip=$(echo "$warp_trace" | awk -F= '/^ip=/{print $2}')
echo "proxy ip: ${warp_ip:-unknown}"

echo
echo "=== 5) yt-dlp through proxy (YouTube audio URL) ==="
YTDLP="${YTDLP_PATH:-yt-dlp}"
if ! command -v "$YTDLP" >/dev/null 2>&1; then
  YTDLP="/tmp/yt-dlp-test"
fi
if [[ ! -x "$YTDLP" ]]; then
  curl -fsSL -o /tmp/yt-dlp-test https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp
  chmod +x /tmp/yt-dlp-test
  YTDLP=/tmp/yt-dlp-test
fi

if timeout 60 "$YTDLP" --proxy "$PROXY" --no-playlist --no-warnings --force-ipv4 \
  -f "ba/b/w" --get-url "$TEST_URL" | head -c 120; then
  echo
  echo "OK: YouTube returned a media URL through WARP"
else
  echo
  echo "FAIL: YouTube still blocked even through WARP"
  exit 1
fi

echo
echo "=== Done. SSH was never touched. Only yt-dlp uses the proxy. ==="
