#!/usr/bin/env bash
set -euo pipefail

# Simple installer to build the ollmlx binary and ensure MLX Python deps are present.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "[ollmlx] verifying prerequisites (go, python3, pip)"
command -v go >/dev/null || { echo "go is required" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }
command -v pip >/dev/null || { echo "pip is required" >&2; exit 1; }

REQ_FILE="$ROOT/mlx_backend/requirements.txt"
if [ -f "$REQ_FILE" ]; then
  echo "[ollmlx] installing Python dependencies from $REQ_FILE"
  python3 -m pip install --upgrade -r "$REQ_FILE"
else
  echo "[ollmlx] warning: $REQ_FILE not found; skipping Python deps"
fi

echo "[ollmlx] building Go binary"
cd "$ROOT"
go build -o "$ROOT/ollmlx" .

echo "[ollmlx] install complete"
echo "Binary: $ROOT/ollmlx"
echo "Run:    ./ollmlx serve"
