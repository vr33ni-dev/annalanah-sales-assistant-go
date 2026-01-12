# Sales Assistant Backend (Go + PostgreSQL)

[![Tests & Coverage](https://github.com/vr33ni-dev/sales-assistant-go/actions/workflows/tests-and-coverage.yml/badge.svg?branch=dev)](https://github.com/vr33ni-dev/sales-assistant-go/actions/workflows/tests-and-coverage.yml) [![codecov](https://codecov.io/gh/vr33ni-dev/sales-assistant-go/branch/dev/graph/badge.svg)](https://codecov.io/gh/vr33ni-dev/sales-assistant-go)

---

## 🚀 Tech Stack

- **Go** (chi router, database/sql)
- **PostgreSQL** (with `golang-migrate` for migrations + seeds)
- **Docker** (optional, for running DB locally)

---

## ⚙️ Setup

### 1. Clone the repo

```bash
git clone git@github.com:vr33ni-dev/sales-assistant-go.git
cd sales-assistant-go
```

### 2. Install dependencies

```bash
go mod tidy
```

### 3. Configure .env

Example:

```bash
DB_URL=postgres://sales_assistant_app:sales_assistant_app@localhost:5432/sales_assistant_db?sslmode=disable
PORT=8080
```

### 4. Run migrations

```bash
make migrate-up
make seed
OR
make reset
```

### 5. Add new migration

```bash
migrate create -ext sql -dir db/migrations alter_example_table
```

### 6. Run the server

```bash
go run main.go
```

### 7. Test (with coverage)

We provide small helper scripts to run unit-only tests and integration-only tests separately.

```bash
./scripts/coverage-unit.sh
go tool cover -func=coverage-unit.out # View unit coverage in the terminal
go tool cover -html=coverage-unit.out # Or open an HTML report

./scripts/coverage-integration.sh
go tool cover -func=coverage-integration.out # View unit coverage in the terminal
go tool cover -html=coverage-integration.out # Or open an HTML report
```

Note: integration tests may be skipped on machines without Docker/Colima (the test harness detects the Docker socket and will skip if Docker isn't ready). If the integration tests are skipped, no coverage will be recorded for those packages until Docker is available.

You can also run a single test directly:

```bash
go test ./api -run TestUpdateComment -v
```

---

## Release labels

The autobump supports label-driven versioning on merged PRs. If a merged PR has one of the labels `major`, `minor`, or `patch` the release workflow will use that label to decide which part of the semver to increment (major > minor > patch precedence). The workflow requires an explicit label; if no label is present the release job will fail and prompt you to add one of `major`, `minor`, or `patch`.

### PR check badge

The project includes a pull-request check that validates `VERSION` changes. The badge below shows the latest status of that check on the `dev` branch (it reflects the workflow run status, not the final release version that will be created on merge to `master`):

[![VERSION check](https://github.com/vr33ni-dev/sales-assistant-go/actions/workflows/check-version-on-pr.yml/badge.svg?branch=dev)](https://github.com/vr33ni-dev/sales-assistant-go/actions/workflows/check-version-on-pr.yml)

Notes:

- The badge shows the most recent run on the `dev` branch. It does not guarantee what version will be produced on `master` — the final release version is computed by the release workflow on `master` at merge time (which uses PR labels to decide major/minor/patch).
- The PR check will fail a PR if a `VERSION` change is invalid or is not a numerical bump over the base branch.

---
> **Note:** The canonical repository is [**on GitHub**](https://github.com/vr33ni-dev/annalanah-sales-assistant-go) · [Mirror on GitLab →](https://gitlab.com/vr33ni-work/annalanah-sales-assistant-go) [![Mirror Status](https://github.com/vr33ni-dev/annalanah-sales-assistant-go/actions/workflows/gitlab-mirror.yml/badge.svg)](https://github.com/vr33ni-dev/annalanah-sales-assistant-go/actions/workflows/gitlab-mirror.yml)
