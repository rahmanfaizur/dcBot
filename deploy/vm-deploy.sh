#!/usr/bin/env bash
# Run on the Azure VM after git pull (or via GitHub Actions deploy workflow).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

git pull --ff-only
docker compose --profile docker up -d --build
docker compose ps

echo ""
echo "Bot logs: docker compose logs -f bot"
