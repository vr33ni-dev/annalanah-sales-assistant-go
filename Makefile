include .env
export

migrate-up:
	migrate -path db/migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path db/migrations -database "$(DATABASE_URL)" down -all

migrate-drop:
	migrate -path db/migrations -database "$(DATABASE_URL)" drop -f

migrate-version:
	migrate -path db/migrations -database "$(DATABASE_URL)" version

seed:
	psql "$(DATABASE_URL)" -f db/seeds/dev_seed.sql

reset:
	make migrate-drop
	make migrate-up
	make seed	

# Create a new migration with timestamp-based filenames
# Usage: make migrate-new name=add_unique_email_to_clients
migrate-new:
	@if [ -z "$(name)" ]; then \
		echo "❌ Please provide a migration name, e.g. make migrate-new name=add_unique_email_to_clients"; \
		exit 1; \
	fi; \
	migrate create \
		-ext sql \
		-dir db/migrations \
		-format "20060102150405" \
		$(name)



############### IMPORT HELPERS ################

create-clients-from-staging:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "DATABASE_URL missing; set it in .env or export it in your shell"; \

		echo "DATABASE_URL missing; set it in .env or export it in your shell"; \
		exit 1; \
	fi; \
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -c "UPDATE clients SET status='active' WHERE id IN (SELECT DISTINCT matched_client_id FROM staging_cashflow WHERE matched_client_id IS NOT NULL);"

mark-ehemalige-block-lost:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "DATABASE_URL missing; set it in .env or export it in your shell"; \
		exit 1; \
	fi; \
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -c "DO $$ DECLARE m INT; rec RECORD; BEGIN SELECT id INTO m FROM staging_cashflow WHERE lower(coalesce(full_name,'')) LIKE '%ehemalige%' LIMIT 1; IF m IS NULL THEN RAISE NOTICE 'no Ehemalige marker found'; ELSE FOR rec IN SELECT id, matched_client_id, full_name FROM staging_cashflow WHERE id > m ORDER BY id LOOP IF coalesce(trim(rec.full_name),'') = '' THEN RAISE NOTICE 'stopping at empty full_name at staging id=%', rec.id; EXIT; END IF; IF rec.matched_client_id IS NOT NULL THEN UPDATE clients SET status='lost' WHERE id = rec.matched_client_id; UPDATE sales_process SET stage='lost', follow_up_result = false WHERE client_id = rec.matched_client_id; RAISE NOTICE 'marked client % (staging %) as lost', rec.matched_client_id, rec.id; ELSE RAISE NOTICE 'staging % had no matched_client_id', rec.id; END IF; END LOOP; END IF; END; $$;"

set-erschienen-for-active:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "DATABASE_URL missing; set it in .env or export it in your shell"; \
		exit 1; \
	fi; \
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -c "UPDATE sales_process SET follow_up_result = true WHERE client_id IN (SELECT id FROM clients WHERE status = 'active');"

remove-ehemalige-client:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "DATABASE_URL missing; set it in .env or export it in your shell"; \
		exit 1; \
	fi; \
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -c "DELETE FROM clients WHERE lower(name) LIKE '%ehemalige%';"

staging-auto-import:
	@echo "Running staging auto-import sequence: create clients, apply cashflow, set statuses, remove Ehemalige"; \
	make create-clients-from-staging; \
	make apply-cashflow-commit; \
	make mark-clients-active; \
	make mark-ehemalige-block-lost; \
	make set-erschienen-for-active; \
	make remove-ehemalige-client; \
	@echo "Done."



#################### Unified import helpers (consolidated) ####################

# Default file paths (override by passing IMPORT_CASHFLOW_FILE=... or IMPORT_SALES_FILE=... on the make command line)
IMPORT_CASHFLOW_FILE ?= ./csv/Cashflow_Ausschnitt.csv
IMPORT_SALES_FILE ?= ./csv/Salesprozess_Ausschnitt.csv

.PHONY: import-cashflow import-salesprocess import-all match-cashflow apply-cashflow

# import-cashflow MODE=dryrun|persist|auto (auto = persist + staging-auto-import)
import-cashflow:
	@MODE=$${MODE:-dryrun}; \
	if [ "$${MODE}" = "persist" ] || [ "$${MODE}" = "auto" ]; then \
		if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL missing; set it in .env or export it in your shell"; exit 1; fi; \
		echo "Importing cashflow CSV: $(IMPORT_CASHFLOW_FILE) -> staging (persist)"; \
		go run ./tools/importcashflow -csv="$(IMPORT_CASHFLOW_FILE)" -dsn="$(DATABASE_URL)" -persist; \
		if [ "$${MODE}" = "auto" ]; then \
			make staging-auto-import; \
		fi; \
	else \
		echo "Dry-run import of $(IMPORT_CASHFLOW_FILE)"; \
		go run ./tools/importcashflow -csv="$(IMPORT_CASHFLOW_FILE)" -dryrun; \
	fi

# Minimal unified import command
.PHONY: import process-staging status-staging

# Usage examples:
#   make import                # dry-run cashflow import (default)
#   make import TYPE=cashflow MODE=auto   # persist cashflow and auto-apply (create clients, apply, statuses)
#   make import TYPE=sales MODE=staging  # persist salesprocess to staging and write ambiguous_names.csv
#   make import TYPE=both MODE=dryrun    # dry-run both imports

import:
	@TYPE=$${TYPE:-cashflow}; MODE=$${MODE:-dryrun}; FILE=$${FILE:-}; \
	CASHFILE=$${FILE:-$(IMPORT_CASHFLOW_FILE)}; SALESFILE=$${SALESFILE:-$(IMPORT_SALES_FILE)}; \
	echo "import: TYPE=$${TYPE} MODE=$${MODE} CASHFILE=$${CASHFILE} SALESFILE=$${SALESFILE}"; \
	if [ "$${TYPE}" = "cashflow" ] || [ "$${TYPE}" = "both" ]; then \
		if [ "$${MODE}" = "dryrun" ]; then \
			go run ./tools/importcashflow -csv="$${CASHFILE}" -dryrun; \
		else \
			if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL missing"; exit 1; fi; \
			go run ./tools/importcashflow -csv="$${CASHFILE}" -dsn="$(DATABASE_URL)" -persist; \
		fi; \
	fi; \
	if [ "$${TYPE}" = "sales" ] || [ "$${TYPE}" = "both" ]; then \
		if [ "$${MODE}" = "preview" ] || [ "$${MODE}" = "dryrun" ]; then \
			go run ./tools/importcsv -csv="$${SALESFILE}" -table=staging_table -mapping=tools/mappings/salesprocess.json -preview; \
		else \
			if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL missing"; exit 1; fi; \
			go run ./tools/importcsv -csv="$${SALESFILE}" -table=staging_salesprocess -mapping=tools/mappings/salesprocess.json -dsn="$(DATABASE_URL)" -staging; \
			go run ./tools/transform_staging -csv="$${SALESFILE}" -out=ambiguous_names.csv; \
			echo "Wrote ambiguous_names.csv"; \
		fi; \
	fi; \
	if [ "$${MODE}" = "auto" ]; then \
		$(MAKE) process-staging MODE=commit; \
	fi

# Single-command full import: cashflow + sales + staging processing
# Usage:
#   make import-all            # dry-run both imports and show what would run
#   make import-all MODE=auto  # persist both imports and run full staging processing (requires DATABASE_URL)
.PHONY: import-all
import-all:
	@MODE=$${MODE:-dryrun}; \
	if [ "$${MODE}" = "dryrun" ]; then \
		echo "import-all (dry-run): will run cashflow and sales previews"; \
		$(MAKE) import TYPE=both MODE=dryrun; \
	else \
		if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL missing; set it in .env or export it in your shell"; exit 1; fi; \
		echo "import-all: persisting cashflow and sales to staging, then processing"; \
    		$(MAKE) import TYPE=cashflow MODE=persist; \
    		go run ./tools/transform_staging -csv="$(IMPORT_SALES_FILE)" -out=ambiguous_names.csv; \
    		echo "Wrote ambiguous_names.csv"; \
	fi

process-staging:
	@MODE=$${MODE:-dryrun}; \
	if [ "$${MODE}" = "commit" ]; then \
		echo "process-staging is deprecated because staging tables were removed."; \
		echo "Use 'make import-all MODE=auto' to run the full import and processing pipeline (cashflow persistence + sales ambiguous extraction)."; \
	else \
		echo "process-staging (no-op): staging tables were removed. To preview, run 'make import-all' (dry-run) or 'make import TYPE=cashflow MODE=dryrun'"; \
	fi


.PHONY: show-import-summary
show-import-summary:
	@# Summary of recent import results (clients, contracts, cashflow entries). Requires DATABASE_URL in environment or .env
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL missing; set it in .env or export it"; exit 1; fi; \
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -c "\
SELECT 'clients: ' || count(*) FROM clients;\
SELECT id, name, status, created_at FROM clients ORDER BY created_at DESC LIMIT 10;\
SELECT 'contracts: ' || count(*) FROM contracts;\
SELECT c.id, c.client_id, cl.name AS client_name, c.start_date, c.duration_months, c.revenue_total FROM contracts c JOIN clients cl ON cl.id = c.client_id ORDER BY c.created_at DESC LIMIT 10;\
SELECT 'cashflow_entries: ' || count(*) FROM cashflow_entries;\
SELECT cf.id, cf.contract_id, co.client_id, cl.name AS client_name, cf.due_date, cf.amount, cf.status FROM cashflow_entries cf JOIN contracts co ON co.id = cf.contract_id JOIN clients cl ON cl.id = co.client_id ORDER BY cf.due_date DESC LIMIT 10;\
"

