#!/usr/bin/env bash
# Azure VM bootstrap (Ubuntu 22.04). Run once on a fresh B1s instance.
#
# Recommended Azure setup:
#   1. Create "Virtual machine" → Ubuntu 22.04 LTS → Size B1s
#   2. Networking: only SSH (22) inbound — do NOT open 8080 publicly
#   3. SSH in, clone repo, run this script, configure .env, start stack
#
# Why a VM (not App Service): bot + Linkdave + yt-dlp/ffmpeg need host networking
# and long-running voice. B1s free tier = 750 hrs/month ≈ 24/7 for one VM.

set -euo pipefail

sudo apt-get update
sudo apt-get install -y ca-certificates curl git

curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"

echo ""
echo "=== Next steps (after logging out and back in) ==="
echo "  git clone https://github.com/rahmanfaizur/dcBot.git && cd dcBot"
echo "  cp .env.example .env"
echo "  # Edit .env locally on the VM only — never commit .env or SSH keys"
echo "  # Required: DISCORD_TOKEN, LINKDAVE_PASSWORD"
echo "  docker compose --profile docker up -d --build"
echo ""
echo "Check logs:"
echo "  docker compose logs -f bot"
echo "  docker compose logs -f linkdave"
