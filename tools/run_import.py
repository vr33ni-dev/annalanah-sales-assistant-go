from csv_importer.engine import ImportEngine
from csv_importer.spec import load_spec
from adapters import GoImportAdapter


def main():
    spec = load_spec("import_specs/cashflow_rules.yaml")
    engine = ImportEngine(spec)

    result = engine.run("Cashflow.csv")

    if result.errors:
        print("Import errors found:")
        print(result.errors)
        return

    adapter = GoImportAdapter(
        "http://localhost:8080/api/import/contracts"
    )

    response = adapter.insert(result.records)

    print("Import successful.")
    print(response)


if __name__ == "__main__":
    main()
