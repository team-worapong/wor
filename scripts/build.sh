#!/usr/bin/env sh
set -eu

output="${1:-dist/wor}"
mkdir -p "$(dirname "$output")"

go build -o "$output" ./cmd/wor
