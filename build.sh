#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

echo "building cece..."
go build -o cece ./cmd/cece
echo "built succ"
