> **Note:** This diagram reflects the **current** migration-style import via `tools/run_import.py` + `POST /api/import/contracts`.
> It is **not** the old incremental/idempotent approach (no fuzzy matching, no upsells).

```mermaid
flowchart TD
  CSV[Cashflow.csv] --> Python[Python rules engine\ncashflow_rules.yaml]
  Python -->|POST /api/import/contracts| Validate{valid migration key?}
  Validate -- no --> Reject[403 Forbidden]
  Validate -- yes --> Truncate[TRUNCATE clients, contracts,\ncashflow_entries, comments]
  Truncate --> Loop[for each record in payload]
  Loop --> ParseDates{parse contract_start\n& contract_end}
  ParseDates -- invalid + is_former --> Skip[skip, add to skipped list]
  ParseDates -- invalid + active --> Abort[500 abort]
  ParseDates -- ok --> InsertClient[INSERT clients\nstatus=active or inactive]
  InsertClient --> InsertSP[INSERT sales_process\nplaceholder is_imported_placeholder=TRUE]
  InsertSP --> DeriveRevenue[derive revenue_total\n& payment_frequency\nfrom cashflows map]
  DeriveRevenue --> InsertContract[INSERT contracts]
  InsertContract --> InsertEntries[INSERT cashflow_entries\nper non-zero cashflow month]
  InsertEntries --> Commit[COMMIT transaction]
  Commit --> Loop
  Loop --> Done[return imported + skipped counts]
```
