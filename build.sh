#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

WEBAPP_DIR="internal/observatory/webapp"

echo "building observatory webapp..."
npm --prefix "$WEBAPP_DIR" ci
npm --prefix "$WEBAPP_DIR" run build

echo "building cece..."
go build -o cece ./cmd/cece
echo "starting cece..."
./cece
