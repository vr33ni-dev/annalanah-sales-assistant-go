package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// ── Helpers ───────────────────────────────────────────────────────────────────
func TestSetCSVHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	filename := "test.csv"
	setCSVHeaders(w, filename)

	if w.Header().Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("expected Content-Type text/csv; charset=utf-8, got %s", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Content-Disposition") != `attachment; filename="test.csv"` {
		t.Fatalf("unexpected Content-Disposition: %s", w.Header().Get("Content-Disposition"))
	}
}

func TestParseMonthParam(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid month", "2024-06", "2024-06-01 00:00:00 +0000 UTC", false},
		{"empty string", "", "0001-01-01 00:00:00 +0000 UTC", false},
		{"invalid format", "2024/06", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMonthParam(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got error %v, want error: %v", err, tt.wantErr)
			}
			if !tt.wantErr && got.String() != tt.want {
				t.Fatalf("got %s, want %s", got.String(), tt.want)
			}
		})
	}
}

func TestMonthStart(t *testing.T) {
	input := "2024-06-15T12:34:56Z"
	want := "2024-06-01 00:00:00 +0000 UTC"

	tm, _ := time.Parse(time.RFC3339, input)
	got := monthStart(tm)

	if got.String() != want {
		t.Fatalf("got %s, want %s", got.String(), want)
	}
}

func TestMonthKey(t *testing.T) {
	input := "2024-06-15T12:34:56Z"
	want := "2024-06"

	tm, _ := time.Parse(time.RFC3339, input)
	got := monthKey(tm)

	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestMonthHeaderKey(t *testing.T) {
	input := "2024-06-15T12:34:56Z"
	want := "m_2024_06"

	tm, _ := time.Parse(time.RFC3339, input)
	got := monthHeaderKey(tm)

	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestParseDateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{"valid date", "2024-06-15", "2024-06-15 00:00:00 +0000 UTC", true},
		{"valid datetime", "2024-06-15 12:34:56", "2024-06-15 12:34:56 +0000 UTC", true},
		{"valid RFC3339", "2024-06-15T12:34:56Z", "2024-06-15 12:34:56 +0000 UTC", true},
		{"invalid format", "2024/06/15", "", false},
		{"empty string", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseDateString(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("got ok=%v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.String() != tt.want {
				t.Fatalf("got %s, want %s", got.String(), tt.want)
			}
		})
	}
}

func TestBuildMonthRangeInclusive(t *testing.T) {
	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 10, 0, 0, 0, 0, time.UTC)
	want := []string{"2024-01-01 00:00:00 +0000 UTC", "2024-02-01 00:00:00 +0000 UTC", "2024-03-01 00:00:00 +0000 UTC"}

	got := buildMonthRangeInclusive(start, end)
	if len(got) != len(want) {
		t.Fatalf("got %d months, want %d", len(got), len(want))
	}
	for i, tm := range got {
		if tm.String() != want[i] {
			t.Errorf("got %s, want %s", tm.String(), want[i])
		}
	}
}

func TestAsString(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"string input", "hello", "hello"},
		{"int input", 123, "123"},
		{"nil input", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := asString(tt.input)
			if got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestExportRawClientsCSV_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		exportClientsRaw: func(_ context.Context) ([][]string, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/clients.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawClientsCSV(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestExportRawClientsCSV_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		exportClientsRaw: func(_ context.Context) ([][]string, error) {
			return [][]string{
				{"1", "Alice", "alice@example.com"},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/clients.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawClientsCSV(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestExportRawContractsCSV_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		exportContractsRaw: func(_ context.Context) ([][]string, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/contracts.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawContractsCSV(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestExportRawContractsCSV_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		exportContractsRaw: func(_ context.Context) ([][]string, error) {
			return [][]string{
				{"1", "Contract A", "1000"},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/contracts.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawContractsCSV(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestExportRawCashflowEntriesCSV_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		exportCashflowEntriesRaw: func(_ context.Context) ([][]string, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/cashflows.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawCashflowEntriesCSV(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestExportRawCashflowEntriesCSV_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		exportCashflowEntriesRaw: func(_ context.Context) ([][]string, error) {
			return [][]string{
				{"1", "2024-07-01", "1000"},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/cashflows.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawCashflowEntriesCSV(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestExportLegacyCashflowCSV_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		exportLegacyCashflow: func(_ context.Context) (store.LegacyCashflowData, error) {
			return store.LegacyCashflowData{}, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestExportAggregatedCashflowCSV_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		exportAggregatedCashflow: func(_ context.Context) (store.AggregatedCashflowData, error) {
			return store.AggregatedCashflowData{}, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/aggregated/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportAggregatedCashflowCSV(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}
