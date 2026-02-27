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


# If exclusions are present, filter the raw profile into a filtered file
if [ ${#EXCLUDE_FILES[@]} -gt 0 ]; then
  HEADER_LINE=$(head -n1 coverage-unit.out)
  tail -n +2 coverage-unit.out > coverage-unit.body
  FILTER_CMD="cat coverage-unit.body"
  for pat in "${EXCLUDE_FILES[@]}"; do
    FILTER_CMD="$FILTER_CMD | grep -v -- \"$pat\""
  done
  # Execute the pipeline and reassemble the profile
  eval "$FILTER_CMD" > coverage-unit.body.filtered || true
  (echo "$HEADER_LINE" && cat coverage-unit.body.filtered) > coverage-unit.filtered.out || true
  rm -f coverage-unit.body coverage-unit.body.filtered
  COVERFILE=coverage-unit.filtered.out
else
  COVERFILE=coverage-unit.out
fi

echo
echo "Unit coverage summary (per-function):"
go tool cover -func="$COVERFILE" || true
echo
echo "Open HTML report with: go tool cover -html=$COVERFILE"
