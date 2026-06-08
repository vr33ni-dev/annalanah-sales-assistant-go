# tools — CSV Parsing Pipeline

This directory contains the Python-based **parsing** layer of the import pipeline. It reads `Cashflow.csv`, validates and transforms the data, and hands the resulting records off to the Go backend. The actual database writes happen entirely on the Go side — this layer never touches the database directly.

## Overview

The pipeline has two steps:

1. **Parse** — the `csv-tidyimport` library reads `Cashflow.csv` using the rules in `import_specs/cashflow_rules.yaml` and produces a list of structured records.
2. **Send** — `GoImportAdapter` POSTs those records as JSON to the Go backend's `/api/import/contracts` endpoint, which writes them to the database.

```
Cashflow.csv  →  csv_importer  →  GoImportAdapter  →  POST /api/import/contracts  →  PostgreSQL
```

## Prerequisites

- Python 3.10+
- The Go backend running locally on port 8080
- `csv-tidyimport` installed as a local editable package (see below)

### First-time setup

**1. Create and activate the virtual environment:**

```bash
python3 -m venv .venv-tools
source .venv-tools/bin/activate
```

**2. Clone and install the `csv-tidyimport` library:**

`csv-tidyimport` is not on PyPI — clone it as a sibling directory and install it as an editable package:

```bash
git clone git@github.com:vr33ni-dev/csv-tidyimport.git ../csv-tidyimport
pip install -e ../csv-tidyimport
pip install requests
```

If you already have the repo cloned elsewhere, adjust the path accordingly:

```bash
pip install -e /path/to/csv-tidyimport
pip install requests
```

**On subsequent runs**, just re-activate the venv — no reinstall needed:

```bash
source .venv-tools/bin/activate
```

## Running the import

All commands must be run from the **repository root** (not from inside `tools/`).

### Step 1 — Preview (always do this first)

Parses `Cashflow.csv` and writes the result to `cashflow_import.json` in the repo root. Does **not** call the backend.

```bash
source .venv-tools/bin/activate
python tools/run_import.py --dryrun
```

Inspect `cashflow_import.json` to verify the records look correct before importing.

### Step 2 — Import (choose one option)

> **Warning:** both options truncate the database first (`clients`, `contracts`, `cashflow_entries`, `comments`) and reimport everything from scratch.

**Option A — Python (parses CSV + sends in one step):**

```bash
python tools/run_import.py
```

**Option B — curl (send the already-parsed `cashflow_import.json` directly):**

```bash
curl -X POST http://localhost:8080/api/import/contracts \
  -H "Content-Type: application/json" \
  -H "X-Migration-Key: ALLOW_MIGRATION" \
  -d @cashflow_import.json
```

On success you will see:

```
{'imported': N, 'skipped': [...], 'status': 'import completed'}
```

Skipped entries are rows with unparseable dates or non-client summary rows at the bottom of the spreadsheet (e.g. "Cashflow Brutto") — these are expected and harmless.

## Files

| File | Purpose |
|---|---|
| `run_import.py` | Entry point — orchestrates parse → send |
| `adapter.py` | `GoImportAdapter` class — HTTP client that POSTs records to the backend |
| `import_specs/cashflow_rules.yaml` | Column mapping, type coercions, dynamic month-column rules for `csv_importer` |

## Rule spec (`cashflow_rules.yaml`) quick reference

| Section | What it does |
|---|---|
| `input` | Skips the first decorative row; row 2 is the header |
| `columns` | Maps named CSV columns to record fields (trim, titlecase, date parsing) |
| `computed_columns` | `name` is assembled from `firstname + ' ' + lastname`; source columns are dropped |
| `dynamic_columns` | Month columns matching `[Month] 'YY` are collected into a `cashflows` map keyed by year-month |

## Backend endpoint

`POST http://localhost:8080/api/import/contracts`

Requires header `X-Migration-Key: ALLOW_MIGRATION` (added automatically by `adapter.py`).

Accepts a JSON array of records. Each record contains at minimum:

| Field | Type | Example |
|---|---|---|
| `name` | string | `"Jane Doe"` |
| `contract_start` | string (YYYY-MM-DD) | `"2024-01-01"` |
| `contract_end` | string (YYYY-MM-DD) or null | `"2025-12-31"` |
| `cashflows` | object (month → amount) | `{"2024-01": 400.0, ...}` |
| `is_former` | bool | `false` |

> **Note:** The endpoint URL is hardcoded in `run_import.py`. If the backend runs on a different port, update the URL passed to `GoImportAdapter` in that file.

## Related docs

- [docs/importer-architecture.md](../docs/importer-architecture.md) — why the pipeline is split into Python + Go layers
- [docs/importer-logic.md](../docs/importer-logic.md) — flowchart of the full import flow
- [docs/import-mapping_v1.md](../docs/import-mapping_v1.md) — authoritative field mapping reference
