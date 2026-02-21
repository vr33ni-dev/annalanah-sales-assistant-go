package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestStartSalesProcess_RequiresInitialContactDate(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	h := &Handler{DB: db}

	// missing InitialContactDate
	reqBody := StartSalesProcessRequest{
		Name:  "Alice",
		Email: "a@example.com",
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStartSalesProcess_ExistingClientWithActiveContract_ReturnsConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	h := &Handler{DB: db}

	// prepare request with ClientID
	initial := "2025-10-01"
	clientID := 42
	reqBody := StartSalesProcessRequest{
		Name:               "Bob",
		Email:              "b@example.com",
		InitialContactDate: &initial,
		ClientID:           &clientID,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// Expect the lookup of existing client (SELECT name, phone, source)
	rows := sqlmock.NewRows([]string{"name", "phone", "source"}).AddRow("Bob", nil, nil)
	mock.ExpectQuery(`SELECT name, phone, source`).WithArgs(clientID).WillReturnRows(rows)

	// Expect the active contract EXISTS query -> return true
	existRows := sqlmock.NewRows([]string{"exists"}).AddRow(true)
	mock.ExpectQuery(`SELECT EXISTS`).WithArgs(clientID).WillReturnRows(existRows)

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
