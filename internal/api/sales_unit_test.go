package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// ── ListSalesProcesses ────────────────────────────────────────────────────────

func TestListSalesProcesses_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		listSalesProcesses: func() ([]domain.SalesProcess, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/sales", nil)
	w := httptest.NewRecorder()
	h.ListSalesProcesses(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListSalesProcesses_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		listSalesProcesses: func() ([]domain.SalesProcess, error) {
			return []domain.SalesProcess{
				{ID: 1, ClientID: 10, ClientName: "Alice", Stage: "erstgespraech"},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/sales", nil)
	w := httptest.NewRecorder()
	h.ListSalesProcesses(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []SalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].ClientName != "Alice" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

// ── UpdateSalesProcess ────────────────────────────────────────────────────────

func TestUpdateSalesProcess_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/abc", "abc", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateSalesProcess_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/1", "1", []byte(`{bad`))
	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateSalesProcess_ClosedTrueWithoutContractFields(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	closed := true
	rev := 1000.0
	b, _ := json.Marshal(SalesProcessUpdateRequest{Closed: &closed, Revenue: &rev})
	req := chiReqWithID(http.MethodPatch, "/api/sales/1", "1", b)
	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateSalesProcess_NotFound_ZeroRows(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateSalesProcess: func(_ context.Context, _ int, _ store.SalesUpdateInput) (int64, error) {
			return 0, nil
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/99", "99", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateSalesProcess_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateSalesProcess: func(_ context.Context, _ int, _ store.SalesUpdateInput) (int64, error) {
			return 0, errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/1", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestUpdateSalesProcess_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		getSalesProcess: func(id int) (domain.SalesProcess, error) {
			return domain.SalesProcess{ID: id, ClientID: 10, ClientName: "Alice", Stage: "erstgespraech"}, nil
		},
		getSalesProcessClientID: func(_ context.Context, _ int) (int, error) {
			return 10, nil
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/1", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out SalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ClientName != "Alice" {
		t.Fatalf("expected ClientName=Alice, got %q", out.ClientName)
	}
}

func TestUpdateSalesProcess_NoShowNormalizesClosed(t *testing.T) {
	// follow_up_result=false forces closed=false regardless of what was sent
	var capturedInput store.SalesUpdateInput
	h := &Handler{store: &mockStore{
		updateSalesProcess: func(_ context.Context, _ int, in store.SalesUpdateInput) (int64, error) {
			capturedInput = in
			return 1, nil
		},
		getSalesProcessClientID: func(_ context.Context, _ int) (int, error) {
			return 10, nil
		},
		getSalesProcess: func(id int) (domain.SalesProcess, error) {
			return domain.SalesProcess{ID: id, Stage: "erstgespraech"}, nil
		},
	}}
	noShow := false
	closed := true
	b, _ := json.Marshal(SalesProcessUpdateRequest{FollowUpResult: &noShow, Closed: &closed})
	req := chiReqWithID(http.MethodPatch, "/api/sales/1", "1", b)
	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedInput.Closed == nil || *capturedInput.Closed != false {
		t.Fatalf("expected closed=false after no-show normalization, got %+v", capturedInput.Closed)
	}
}

// ── StartSalesProcess ─────────────────────────────────────────────────────────

func TestStartSalesProcess_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader([]byte(`{bad`)))
	w := httptest.NewRecorder()
	h.StartSalesProcess(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStartSalesProcess_MissingInitialContactDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(StartSalesProcessRequest{Name: "Bob", Email: "bob@test.com", Source: "organic"})
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.StartSalesProcess(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStartSalesProcess_MissingEmailAndPhone(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	icd := "2026-01-01"
	b, _ := json.Marshal(StartSalesProcessRequest{Name: "Bob", Source: "organic", InitialContactDate: &icd})
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.StartSalesProcess(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStartSalesProcess_LeadResolutionError(t *testing.T) {
	h := &Handler{store: &mockStore{
		resolveLeadForSalesStart: func(_ context.Context, _ *int, _ string) (*int, string, *int, error) {
			return nil, "", nil, errors.New("lead lookup failed")
		},
	}}
	icd := "2026-01-01"
	b, _ := json.Marshal(StartSalesProcessRequest{Name: "Bob", Email: "bob@test.com", Source: "organic", InitialContactDate: &icd})
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.StartSalesProcess(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestStartSalesProcess_HasActiveContract_Conflict(t *testing.T) {
	clientID := 42
	h := &Handler{store: &mockStore{
		resolveLeadForSalesStart: func(_ context.Context, _ *int, _ string) (*int, string, *int, error) {
			return nil, "", nil, nil
		},
		getExistingClientBasic: func(_ context.Context, _ int) (domain.ClientBasic, error) {
			return domain.ClientBasic{ID: 42, Name: "Alice"}, nil
		},
		hasActiveContractForClient: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
	}}
	icd := "2026-01-01"
	b, _ := json.Marshal(StartSalesProcessRequest{
		Name:               "Alice",
		Email:              "alice@test.com",
		Source:             "organic",
		InitialContactDate: &icd,
		ClientID:           &clientID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.StartSalesProcess(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestStartSalesProcess_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		resolveLeadForSalesStart: func(_ context.Context, _ *int, _ string) (*int, string, *int, error) {
			return nil, "", nil, nil
		},
		runStartSalesProcess: func(_ context.Context, _ store.StartSalesInput, _, _ *int) (int, int, string, *int, error) {
			return 0, 0, "", nil, errors.New("db down")
		},
	}}
	icd := "2026-01-01"
	b, _ := json.Marshal(StartSalesProcessRequest{Name: "Bob", Email: "bob@test.com", Source: "organic", InitialContactDate: &icd})
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.StartSalesProcess(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestStartSalesProcess_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		resolveLeadForSalesStart: func(_ context.Context, _ *int, _ string) (*int, string, *int, error) {
			return nil, "", nil, nil
		},
		runStartSalesProcess: func(_ context.Context, _ store.StartSalesInput, _, _ *int) (int, int, string, *int, error) {
			return 5, 20, "erstgespraech", nil, nil
		},
		getStartSalesResponseData: func(_ context.Context, _, _ int) ([]domain.Comment, domain.ClientBasic, error) {
			return nil, domain.ClientBasic{ID: 5, Name: "Bob", Email: "bob@test.com"}, nil
		},
	}}
	icd := "2026-01-01"
	b, _ := json.Marshal(StartSalesProcessRequest{Name: "Bob", Email: "bob@test.com", Source: "organic", InitialContactDate: &icd})
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.StartSalesProcess(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	var out StartSalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.SalesProcessID != 20 || out.Client.Name != "Bob" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

// ── GetUpsellForSalesProcess ──────────────────────────────────────────────────

func TestGetUpsellForSalesProcess_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodGet, "/api/sales/abc/upsell", "abc", nil)
	w := httptest.NewRecorder()
	h.GetUpsellForSalesProcess(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetUpsellForSalesProcess_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		getUpsellForSalesProcess: func(_ context.Context, _ int) ([]domain.ContractUpsell, error) {
			return nil, errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodGet, "/api/sales/1/upsell", "1", nil)
	w := httptest.NewRecorder()
	h.GetUpsellForSalesProcess(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetUpsellForSalesProcess_Success(t *testing.T) {
	rev := 1190.0
	h := &Handler{store: &mockStore{
		getUpsellForSalesProcess: func(_ context.Context, _ int) ([]domain.ContractUpsell, error) {
			return []domain.ContractUpsell{{ID: 1, SalesProcessID: 1, UpsellRevenue: &rev}}, nil
		},
	}}
	req := chiReqWithID(http.MethodGet, "/api/sales/1/upsell", "1", nil)
	w := httptest.NewRecorder()
	h.GetUpsellForSalesProcess(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []domain.ContractUpsell
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 upsell, got %d", len(out))
	}
	// Revenue converted from brutto 1190 → netto 1000
	if *out[0].UpsellRevenue != 1000.0 {
		t.Fatalf("expected netto revenue=1000, got %v", *out[0].UpsellRevenue)
	}
}

// ── ListUpsellCategories ──────────────────────────────────────────────────────

func TestListUpsellCategories_InvalidStartDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/sales/upsells/list?start_date=not-a-date", nil)
	w := httptest.NewRecorder()
	h.ListUpsellCategories(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListUpsellCategories_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		listUpsells: func(_ context.Context, _, _ *time.Time) ([]domain.ContractUpsell, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/sales/upsells/list", nil)
	w := httptest.NewRecorder()
	h.ListUpsellCategories(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListUpsellCategories_Success(t *testing.T) {
	verl := "verlaengerung"
	keine := "keine_verlaengerung"
	rev := 1190.0
	h := &Handler{store: &mockStore{
		listUpsells: func(_ context.Context, _, _ *time.Time) ([]domain.ContractUpsell, error) {
			return []domain.ContractUpsell{
				{ID: 1, UpsellResult: nil},               // scheduled
				{ID: 2, UpsellResult: &verl, UpsellRevenue: &rev},  // successful
				{ID: 3, UpsellResult: &keine},             // unsuccessful
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/sales/upsells/list", nil)
	w := httptest.NewRecorder()
	h.ListUpsellCategories(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out map[string][]domain.ContractUpsell
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out["scheduled"]) != 1 || len(out["successful"]) != 1 || len(out["unsuccessful"]) != 1 {
		t.Fatalf("unexpected categorization: %+v", out)
	}
}

// ── CreateOrUpdateUpsell ──────────────────────────────────────────────────────

func TestCreateOrUpdateUpsell_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/abc/upsell", "abc", []byte(`{}`))
	w := httptest.NewRecorder()
	h.CreateOrUpdateUpsell(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateOrUpdateUpsell_InvalidUpsellResult(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	bad := "wrong"
	b, _ := json.Marshal(CreateUpsellRequest{UpsellResult: &bad})
	req := chiReqWithID(http.MethodPatch, "/api/sales/1/upsell", "1", b)
	w := httptest.NewRecorder()
	h.CreateOrUpdateUpsell(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateOrUpdateUpsell_VerlaengerungWithoutRevenue(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	verl := "verlaengerung"
	b, _ := json.Marshal(CreateUpsellRequest{UpsellResult: &verl})
	req := chiReqWithID(http.MethodPatch, "/api/sales/1/upsell", "1", b)
	w := httptest.NewRecorder()
	h.CreateOrUpdateUpsell(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateOrUpdateUpsell_SalesNotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		getSalesProcessClientID: func(_ context.Context, _ int) (int, error) {
			return 0, errors.New("not found")
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/99/upsell", "99", []byte(`{}`))
	w := httptest.NewRecorder()
	h.CreateOrUpdateUpsell(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateOrUpdateUpsell_CannotCreateConflict(t *testing.T) {
	h := &Handler{store: &mockStore{
		getSalesProcessClientID: func(_ context.Context, _ int) (int, error) {
			return 5, nil
		},
		createOrUpdateUpsell: func(_ context.Context, _ store.UpsellInput) (store.UpsellResult, error) {
			return store.UpsellResult{}, errors.New("cannot create upsell: already exists")
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/1/upsell", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.CreateOrUpdateUpsell(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestCreateOrUpdateUpsell_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		getSalesProcessClientID: func(_ context.Context, _ int) (int, error) {
			return 5, nil
		},
		createOrUpdateUpsell: func(_ context.Context, _ store.UpsellInput) (store.UpsellResult, error) {
			return store.UpsellResult{}, errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/1/upsell", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.CreateOrUpdateUpsell(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateOrUpdateUpsell_Success(t *testing.T) {
	upsellID := 7
	h := &Handler{store: &mockStore{
		getSalesProcessClientID: func(_ context.Context, _ int) (int, error) {
			return 5, nil
		},
		createOrUpdateUpsell: func(_ context.Context, _ store.UpsellInput) (store.UpsellResult, error) {
			return store.UpsellResult{UpsellID: upsellID, Updated: false}, nil
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/sales/1/upsell", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.CreateOrUpdateUpsell(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(out["upsell_id"].(float64)) != upsellID {
		t.Fatalf("expected upsell_id=%d, got %v", upsellID, out["upsell_id"])
	}
}

// ── GetUpsellAnalytics ────────────────────────────────────────────────────────

func TestGetUpsellAnalytics_InvalidStartDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/sales/upsells/analytics?start_date=bad-date", nil)
	w := httptest.NewRecorder()
	h.GetUpsellAnalytics(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetUpsellAnalytics_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		getUpsellAnalytics: func(_ context.Context, _, _ *time.Time) (store.UpsellStats, []store.MonthlyRevenue, error) {
			return store.UpsellStats{}, nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/sales/upsells/analytics", nil)
	w := httptest.NewRecorder()
	h.GetUpsellAnalytics(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetUpsellAnalytics_Success(t *testing.T) {
	q := 75.0
	h := &Handler{store: &mockStore{
		getUpsellAnalytics: func(_ context.Context, _, _ *time.Time) (store.UpsellStats, []store.MonthlyRevenue, error) {
			return store.UpsellStats{
				VerlaengerungCount:      3,
				KeineVerlaengerungCount: 1,
				ScheduledCount:          2,
				Verlaengerungsquote:     &q,
				UmsatzSumBrutto:         2380.0,
			}, []store.MonthlyRevenue{
				{Month: "2026-01", Revenue: 1190.0},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/sales/upsells/analytics?start_date=2026-01-01&end_date=2026-12-31", nil)
	w := httptest.NewRecorder()
	h.GetUpsellAnalytics(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 2380 brutto → 2000 netto
	if out["umsatz_sum"].(float64) != 2000.0 {
		t.Fatalf("expected umsatz_sum=2000, got %v", out["umsatz_sum"])
	}
	if int(out["verlaengerung_count"].(float64)) != 3 {
		t.Fatalf("expected verlaengerung_count=3, got %v", out["verlaengerung_count"])
	}
	rev := out["revenue_by_month"].([]interface{})
	if len(rev) != 1 {
		t.Fatalf("expected 1 revenue_by_month entry, got %d", len(rev))
	}
	// 1190 brutto → 1000 netto
	if rev[0].(map[string]interface{})["revenue"].(float64) != 1000.0 {
		t.Fatalf("expected revenue_by_month[0].revenue=1000, got %v", rev[0])
	}
}
