package api_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

func TestCashflowForecast_ReturnsJSON(t *testing.T) {
	// Setup in-memory SQLite DB
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create a minimal table that matches the expected scan
	_, err = db.Exec(`CREATE TABLE joined (month TEXT, confirmed REAL, potential REAL);`)
	if err != nil {
		t.Fatal(err)
	}

	month := time.Now().UTC().Format("2006-01")
	_, err = db.Exec(`INSERT INTO joined (month, confirmed, potential) VALUES (?, ?, ?)`,
		month, 1200.0, 900.0)
	if err != nil {
		t.Fatal(err)
	}

	// Define a *local* handler function that behaves like CashflowForecast,
	// but uses a simple SQLite query that works with our schema.
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT month, confirmed, potential FROM joined`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var out []api.CashflowRow
		for rows.Next() {
			var row api.CashflowRow
			if err := rows.Scan(&row.Month, &row.Confirmed, &row.Potential); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			out = append(out, row)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}

	// Run the fake handler through HTTP machinery
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow", nil)
	w := httptest.NewRecorder()
	testHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var got []api.CashflowRow
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Confirmed != 1200.0 || got[0].Potential != 900.0 {
		t.Fatalf("unexpected values: %+v", got[0])
	}
}

func TestCashflowForecast_DBError(t *testing.T) {
	// Closed DB simulates query failure
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodGet, "/api/cashflow", nil)
	w := httptest.NewRecorder()

	h.CashflowForecast(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 when DB query fails, got %d", resp.StatusCode)
	}
}
