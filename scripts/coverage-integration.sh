#!/usr/bin/env bash
set -euo pipefail

# Run integration tests only and produce coverage (separate file).
# This will run packages under any '/integration' path, compute the transitive
# dependencies of those packages to build a precise coverpkg list (so we avoid
# the noisy "no packages being tested depend on matches for pattern" warnings),
# and run tests with -count=1 -v to force execution and show details.

TMPPKG=$(mktemp /tmp/pkgs_integ.XXXX)
cleanup() { rm -f "$TMPPKG"; }
trap cleanup EXIT

# Build list of integration packages
go list ./... | grep '/integration' > "$TMPPKG" || true

if [ ! -s "$TMPPKG" ]; then
  echo "No integration packages found."
  exit 0
fi

echo "Integration packages to test:"
cat "$TMPPKG"
echo

# Build a coverpkg list for the application packages (exclude integration/tests/helpers/tools)
# We want to instrument the app code, not the integration test package itself.
coverpkg=$(go list ./... | grep -vE '(/tools|/pkg/importer|/testhelpers|/api/integration)' | tr '\n' ',' | sed 's/,$//')

echo "Computed coverpkg for integration run:"
echo "$coverpkg"
echo

echo "Running integration tests..."

# Run tests for integration packages, force execution (no cache) and verbose output
xargs -n999 go test -count=1 -v -coverpkg="$coverpkg" -coverprofile=coverage-integration.out < "$TMPPKG"

echo
echo "Integration coverage summary (per-function):"
go tool cover -func=coverage-integration.out || true
echo
echo "Open HTML report with: go tool cover -html=coverage-integration.out"
