#!/usr/bin/env bash
set -euo pipefail

# bump-version.sh
# Portable helper for GitHub Actions to compute a semantic version bump
# Supports two targets: VERSION (plain file) and package.json (npm project)
# Usage: bump-version.sh <target> <bump>
#   target: VERSION | package.json
#   bump: major|minor|patch|auto
# If bump == auto the script will try to read labels from GITHUB_EVENT_PATH

TARGET=${1:-VERSION}
BUMP_ARG=${2:-auto}
GITHUB_EVENT_PATH=${GITHUB_EVENT_PATH:-}
GITHUB_REPOSITORY=${GITHUB_REPOSITORY:-}
GITHUB_TOKEN=${GITHUB_TOKEN:-}
PR_LABELS=${PR_LABELS:-}

# Write output for GH Actions (new runner behavior uses GITHUB_OUTPUT file path)
gh_output() {
  name="$1"
  value="$2"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "${name}=${value}" >> "${GITHUB_OUTPUT}"
  else
    # fallback for older runners (best-effort)
    echo "${name}=${value}"
  fi
}

determine_bump_from_event() {
  # Prefer PR_LABELS env (JSON array) if provided by workflow to avoid extra API calls
  if [ -n "${PR_LABELS:-}" ] && [ "${PR_LABELS}" != "null" ]; then
    LABELS=$(echo "${PR_LABELS}" | jq -r '.[].name' 2>/dev/null || true)
  elif [ -n "${GITHUB_EVENT_PATH:-}" ] && [ -f "${GITHUB_EVENT_PATH}" ]; then
    LABELS=$(jq -r '.pull_request.labels[]?.name' "${GITHUB_EVENT_PATH}" 2>/dev/null || true)
  else
    LABELS=""
  fi
  LABELS_LOWER=$(echo "$LABELS" | tr '[:upper:]' '[:lower:]' || true)
  if echo "$LABELS_LOWER" | grep -q "major"; then
    echo "major"; return
  elif echo "$LABELS_LOWER" | grep -q "minor"; then
    echo "minor"; return
  elif echo "$LABELS_LOWER" | grep -q "patch"; then
    echo "patch"; return
  fi
  echo ""
}

if [ "$BUMP_ARG" != "auto" ]; then
  BUMP="$BUMP_ARG"
else
  BUMP=$(determine_bump_from_event)
  if [ -z "$BUMP" ]; then
    BUMP="patch"
  fi
fi

echo "Target: $TARGET"
echo "Bump type: $BUMP"

# Ensure we have tags locally
git fetch --tags --quiet origin || true
LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || true)
echo "Latest tag: ${LATEST_TAG:-<none>}"

if [ -z "$LATEST_TAG" ]; then
  MAJOR=0; MINOR=1; PATCH=0
else
  BASE=${LATEST_TAG#v}
  IFS='.' read -r MAJOR MINOR PATCH <<< "${BASE}"
  MAJOR=${MAJOR:-0}; MINOR=${MINOR:-0}; PATCH=${PATCH:-0}
fi

case "$BUMP" in
  major)
    MAJOR=$((MAJOR+1)); MINOR=0; PATCH=0
    ;;
  minor)
    MINOR=$((MINOR+1)); PATCH=0
    ;;
  patch)
    PATCH=$((PATCH+1))
    ;;
  *)
    echo "Unknown bump: $BUMP"; exit 2
    ;;
esac

NEW_VERSION="v${MAJOR}.${MINOR}.${PATCH}"
NEW_TAG="$NEW_VERSION"

echo "Computed: ${NEW_VERSION}"

if [ "$TARGET" = "VERSION" ]; then
  # write the v-prefixed value to VERSION to match current repo behaviour
  echo "${NEW_VERSION}" > VERSION
  echo "Wrote VERSION -> $(cat VERSION)"
elif [ "$TARGET" = "package.json" ]; then
  # requires node/npm in runner
  if ! command -v npm >/dev/null 2>&1; then
    echo "npm not found in PATH; cannot bump package.json" >&2
    exit 3
  fi
  CUR_VER=$(node -p "require('./package.json').version")
  echo "Current package.json version: ${CUR_VER}"
  npm --no-git-tag-version version "${BUMP}"
  NEW_VER=$(node -p "require('./package.json').version")
  NEW_TAG="v${NEW_VER}"
  echo "Updated package.json -> ${NEW_VER}"
else
  echo "Unknown target: $TARGET"; exit 2
fi

# emit outputs for GH Actions
gh_output "NEW_VERSION" "${NEW_VERSION}"
gh_output "NEW_TAG" "${NEW_TAG}"

echo "NEW_VERSION=${NEW_VERSION}"
echo "NEW_TAG=${NEW_TAG}"

exit 0
