# tools — CSV Parsing Pipeline

This directory contains the Python-based **parsing** layer of the import pipeline. It reads `Cashflow.csv`, validates and transforms the data, and hands the resulting records off to the Go backend. The actual database writes happen entirely on the Go side — this layer never touches the database directly.

## Overview

The pipeline has two steps:

1. **Parse** — the `csv_importer` library reads `Cashflow.csv` using the rules in `import_specs/cashflow_rules.yaml` and produces a list of structured records.
2. **Send** — `GoImportAdapter` POSTs those records as JSON to the Go backend's `/api/import/contracts` endpoint, which writes them to the database.

```
Cashflow.csv  →  csv_importer  →  GoImportAdapter  →  POST /api/import/contracts  →  PostgreSQL
```

## Prerequisites

- Python 3.9+
- The `csv_importer` library installed (see below)
- The Go backend running locally on port 8080

Install Python dependencies:

```bash
pip install csv_importer requests
```

## Running the import

From the **repository root** (not from inside `tools/`), run:

```bash
python tools/run_import.py
```

The script resolves `import_specs/cashflow_rules.yaml` and `Cashflow.csv` relative to the working directory, so it must be run from the root.

If the import succeeds you will see:

```
Import successful.
{'imported': N, 'skipped': [...]}
```

If validation errors are found in the CSV before any data is sent, the script prints them and exits without calling the backend.

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

Accepts a JSON array of records. Each record contains at minimum:

| Field | Type | Example |
|---|---|---|
| `name` | string | `"Jane Doe"` |
| `contract_start` | string (YYYY-MM-DD) | `"2024-01-01"` |
| `contract_end` | string (YYYY-MM-DD) or null | `"2025-12-31"` |
| `clv` | string (raw from CSV) | `"4800"` |
| `cashflows` | object (month → amount) | `{"Jan '24": "400", ...}` |

The endpoint is idempotent by client name — existing clients are matched and updated rather than duplicated.

> **Note:** The endpoint URL is hardcoded in `run_import.py`. If the backend runs on a different port, update the URL passed to `GoImportAdapter` in that file.

## Related docs

- [docs/importer-architecture.md](../docs/importer-architecture.md) — why the pipeline is split into Python + Go layers
- [docs/importer-logic.md](../docs/importer-logic.md) — flowchart of the full import flow
- [docs/import-mapping_v1.md](../docs/import-mapping_v1.md) — authoritative field mapping reference
