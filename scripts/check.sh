#!/usr/bin/env sh
set -eu

echo "==> Formatting"
gofmt -w .

echo "==> Testing"
go test ./...

echo "==> Building"
./scripts/build.sh

echo
echo "✓ All checks passed."
