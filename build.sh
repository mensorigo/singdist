#!/usr/bin/env bash
# Build the SubDist panel: frontend -> embedded dist -> single Go binary.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEBDIST="$ROOT/server/internal/httpx/webdist"

# Go is expected on PATH; fall back to a local toolchain install if present.
if ! command -v go >/dev/null 2>&1 && [ -x "$HOME/.local/go1.27.3/bin/go" ]; then
  export PATH="$HOME/.local/go1.27.3/bin:$PATH"
fi

echo "==> Building web frontend"
cd "$ROOT/web"
if [ -f package-lock.json ]; then
  npm ci
else
  npm install
fi
npm run build

echo "==> Embedding frontend into server/internal/httpx/webdist"
rm -rf "$WEBDIST"
mkdir -p "$WEBDIST"
cp -r "$ROOT/web/dist/." "$WEBDIST/"

echo "==> Building server binary"
mkdir -p "$ROOT/bin"
cd "$ROOT/server"
go build -trimpath -o "$ROOT/bin/subdist" ./cmd/subdist

echo "==> Done: $ROOT/bin/subdist"
