import json
import os
import sys
from csv_importer.engine import ImportEngine
from csv_importer.spec import load_spec
from adapter import GoImportAdapter

TOOLS_DIR = os.path.dirname(os.path.abspath(__file__))


def main():
    dryrun = "--dryrun" in sys.argv

    spec = load_spec(os.path.join(TOOLS_DIR, "import_specs/cashflow_rules.yaml"))
    engine = ImportEngine(spec)

    csv_path = os.path.join(TOOLS_DIR, "../Cashflow.csv")
    csv_stem = os.path.splitext(os.path.basename(csv_path))[0].lower()
    out_path = os.path.join(os.path.dirname(csv_path), f"{csv_stem}_import.json")

    result = engine.run(csv_path)
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(result.records, f, ensure_ascii=False, indent=2)
    print(f"Records written to {out_path}")

    if dryrun:
        print(f"Dry-run: {len(result.records)} records parsed, not sent to backend.")
        return

    adapter = GoImportAdapter(
        "http://localhost:8080/api/import/contracts"
    )

    response = adapter.insert(result.records)

    print("Import successful.")
    print(response)


if __name__ == "__main__":
    main()
