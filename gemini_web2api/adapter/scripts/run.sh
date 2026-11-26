#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
# Load .env if present
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi
exec python3 app.py
