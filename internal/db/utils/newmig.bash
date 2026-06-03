#!/bin/bash
name="$1"
if [ -z "$name" ]; then
  echo "Usage: $0 <migration_name>"
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
MIGRATIONS_DIR="${ROOT_DIR}/internal/db/migrations"
mkdir -p "$MIGRATIONS_DIR"

ts=$(date +%Y%m%d%H%M%S)
up="${MIGRATIONS_DIR}/${ts}_${name}.up.sql"
down="${MIGRATIONS_DIR}/${ts}_${name}.down.sql"

touch "$up" "$down"
echo "✅ Created:"
echo "  - $up"
echo "  - $down"
