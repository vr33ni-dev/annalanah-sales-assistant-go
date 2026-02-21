#!/usr/bin/env bash
set -euo pipefail

# Run unit tests (exclude integration and other excluded packages) and produce coverage
TMP=$(mktemp /tmp/pkgs.XXXX)
cleanup() { rm -f "$TMP"; }
trap cleanup EXIT

# determine module path and packages to exclude from unit tests
MODULE=$(go list -m)
# exclude the module root, tools, importer, testhelpers, integration and pkg/version
EXCLUDE_RE="(^${MODULE}$|/tools|/pkg/importer|/testhelpers|/api/integration|/pkg/version)"

# build package list (one per line)
go list ./... | grep -vE "$EXCLUDE_RE" > "$TMP"

if [ ! -s "$TMP" ]; then
  echo "No packages found to test (after exclusions)" >&2
  exit 1
fi

coverpkg=$(paste -sd, "$TMP")

echo "Running unit tests for packages (excluded integration and tools/importer/testhelpers):"
cat "$TMP"
echo

go test -v $(cat "$TMP") -coverpkg="$coverpkg" -coverprofile=coverage-unit.out

echo
echo "Unit coverage summary (per-function):"
go tool cover -func=coverage-unit.out || true
echo
echo "Open HTML report with: go tool cover -html=coverage-unit.out"
