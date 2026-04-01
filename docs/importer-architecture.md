# Importer Architecture: Python + Go Two-Step Pipeline

## Overview

The import pipeline is split into two clearly separated layers:

1. **Python rules engine** — transforms the raw CSV into clean JSON
2. **Go importer** — writes clean JSON into the database with business logic

---

## Layer responsibilities

### Python rules engine (`csv_importer` + `cashflow_rules.yaml`)

- **Owns:** CSV parsing, column mapping, transforms (trim/titlecase), date format normalization, dynamic month column detection, name concatenation
- **Knows about:** spreadsheet quirks, German date formats, multi-format columns, the "wide" layout of `Cashflow.csv`
- **Produces:** clean, normalized JSON records the Go API can understand

> Rule of thumb: anything that is specific to the shape of your spreadsheet lives here.

### Go importer (`POST /api/import/contracts`)

- **Owns:** database writes, transactional integrity, business rules (active vs inactive, frequency derivation, revenue calculation), migration safety guards (truncate, production block, migration key)
- **Knows about:** your DB schema, domain logic, cashflow entry types, contract structure
- **Consumes:** clean JSON it can trust

> Rule of thumb: anything that touches persistence or business logic lives here.

---

## Why this separation is good

1. **Swap-safe** — if you switch from CSV to an API feed later, you replace only the Python side; the Go importer does not change
2. **Testable in isolation** — you can unit-test Python transforms with no DB, and test Go with mock JSON payloads
3. **Readable rules** — the YAML spec is human-readable and editable by non-developers
4. **No spreadsheet logic bleeds into your backend** — the Go service does not know what `März '26` means

---

## One thing to watch

The Python adapter in `tools/adapter.py` sends to a hardcoded `localhost:8080` URL. If you ever want to run the importer against staging or production, that should be configurable via an environment variable or CLI argument.

---

## Files involved

| File | Role |
|---|---|
| `tools/run_import.py` | Entry point — orchestrates the pipeline |
| `tools/adapter.py` | HTTP adapter — POSTs transformed records to the Go API |
| `tools/import_specs/cashflow_rules.yaml` | Column mapping and transform rules |
| `api/importer.go` | Go handler — validates, transforms and persists to DB |

---

## Related docs

- [import-mapping_v1.md](import-mapping_v1.md) — detailed field mapping, payload shape, DB behavior
- [importer-logic.md](importer-logic.md) — migration flow diagram
