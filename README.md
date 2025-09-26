# Sales Assistant Backend (Go + PostgreSQL)

This is the backend service for the **Sales Assistant** application.  
It powers client management, sales processes, contracts, cashflow tracking, and stage/event organization.

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
```

### 5. Run the server

```bash
go run main.go
```
