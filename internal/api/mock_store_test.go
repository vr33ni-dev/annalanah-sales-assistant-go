package api

import (
	"context"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// mockStore is a configurable test double for store.Store.
// Set only the fields your test needs; unset fields return zero values.
type mockStore struct {
	getSalesProcess             func(id int) (domain.SalesProcess, error)
	listSalesProcesses          func() ([]domain.SalesProcess, error)
	listCashflowEntries         func(store.CashflowEntryFilter) ([]domain.CashflowEntry, int, error)
	cashflowForecast            func(start, end time.Time, avg float64, contractID *int) ([]domain.CashflowForecastRow, error)
	cashflowYTDPaid             func(start, end time.Time) (float64, error)
	cashflowNextMonthsConfirmed func(start, end time.Time) ([]domain.MonthConfirmed, error)
	updateCashflowEntryStatus   func(id int, status string) error

	listCommentsByEntity    func(entityType string, entityID int) ([]domain.Comment, error)
	listCommentsByClientID  func(clientID int) ([]domain.Comment, error)
	createComment           func(entityType string, entityID int, author *string, body string, metadata map[string]interface{}) (domain.Comment, error)
	deleteComment           func(id int) error
	updateComment           func(id int, author *string, body *string, metadata *map[string]interface{}) (domain.Comment, error)
	insertCommentsForEntity func(entityType string, entityID int, clientID int, comments []domain.Comment) error

	syncClientCompletedAt     func(ctx context.Context, clientID int, closed *bool, completedAt *string) error
	syncClientStatusFromSales func(ctx context.Context, salesProcessID int) error

	validateClientCompletedAt    func(ctx context.Context, clientID int, completedAt *time.Time) error
	insertClient                 func(ctx context.Context, name, email, phone, source string, sourceStageID *int, status string) (int, error)
	deleteClientWithLeadReset    func(ctx context.Context, clientID int) (bool, error)
	getClientCompletedAt         func(ctx context.Context, clientID int) (*time.Time, error)
	updateClientFields           func(ctx context.Context, id int, name, email, phone, source, status string, sourceStageID *int, sourceStageIDSet bool, completedAt *time.Time) error
	clearClientSalesProcessStageID func(ctx context.Context, clientID int) error
	syncLeadFromClient             func(ctx context.Context, clientID int, name, email, phone, source string, sourceStageID *int, sourceStageIDSet bool) error
	listClients                    func(ctx context.Context, includeInactive bool) ([]domain.ClientRow, error)

	executeRawQuery func(ctx context.Context, sqlText string) ([]string, []map[string]interface{}, error)

	updateSalesProcess           func(ctx context.Context, id int, in store.SalesUpdateInput) (int64, error)
	getSalesProcessClientID      func(ctx context.Context, salesProcessID int) (int, error)
	insertSalesProcessComment    func(ctx context.Context, salesProcessID, clientID int, author *string, body string, metadata []byte) error
	getUpsellForSalesProcess     func(ctx context.Context, salesProcessID int) ([]domain.ContractUpsell, error)
	listUpsells                  func(ctx context.Context, startDate, endDate *time.Time) ([]domain.ContractUpsell, error)
	createOrUpdateUpsell         func(ctx context.Context, in store.UpsellInput) (store.UpsellResult, error)
	getUpsellAnalytics           func(ctx context.Context, startDate, endDate *time.Time) (store.UpsellStats, []store.MonthlyRevenue, error)
	hasActiveContractForClient   func(ctx context.Context, clientID int) (bool, error)
	runStartSalesProcess         func(ctx context.Context, in store.StartSalesInput, existingClientID, foundLeadID *int) (int, int, string, *int, error)
	resolveLeadForSalesStart     func(ctx context.Context, leadID *int, email string) (*int, string, *int, error)
	getExistingClientBasic       func(ctx context.Context, clientID int) (domain.ClientBasic, error)
	getStartSalesResponseData    func(ctx context.Context, salesID, clientID int) ([]domain.Comment, domain.ClientBasic, error)
	ensureContractForClosedSales func(ctx context.Context, salesProcessID, clientID int, in store.EnsureContractInput) (*domain.ContractNotifyData, error)

	getContractsInRange func(ctx context.Context, typ string, start, end *time.Time) ([]domain.ContractSummary, error)
	getDashboardKPIs    func(ctx context.Context, start, end *time.Time) (domain.DashboardKPIsRaw, error)
	getMonthlyKPIs      func(ctx context.Context, year int) ([]domain.MonthlyKPIRaw, error)

	listLeads   func(ctx context.Context) ([]domain.Lead, error)
	createLead  func(ctx context.Context, name string, email, phone *string, source string, stageID *int) (domain.Lead, bool, error)
	updateLead  func(ctx context.Context, id int, name, email, phone, source *string, stageID *int) (domain.Lead, error)
	deleteLead  func(ctx context.Context, id int) error
	convertLead func(ctx context.Context, leadID int) (int, int, error)

	listContracts         func(ctx context.Context, includeExpired, includeComments, includeCashflow bool) ([]domain.ContractRow, error)
	getContractByID       func(ctx context.Context, id int) (domain.ContractRow, error)
	getContractCashflow   func(ctx context.Context, contractID int) ([]domain.CashflowEntry, error)
	createContract        func(ctx context.Context, in store.ContractCreateInput) (int, *string, error)
	updateContract        func(ctx context.Context, id int, sd, ed time.Time, durationMonths int, revenueTotal float64, paymentFreq string) error
	getContractClientID   func(ctx context.Context, contractID int) (int, error)
	getContractNotifyData func(ctx context.Context, contractID int) (domain.ContractNotifyData, error)

	exportClientsRaw        func(ctx context.Context) ([][]string, error)
	exportContractsRaw      func(ctx context.Context) ([][]string, error)
	exportCashflowEntriesRaw func(ctx context.Context) ([][]string, error)
	exportLegacyCashflow    func(ctx context.Context) (store.LegacyCashflowData, error)
	exportAggregatedCashflow func(ctx context.Context) (store.AggregatedCashflowData, error)

	listStages              func() ([]domain.Stage, error)
	listStageParticipants   func(stageID, limit, offset int) ([]domain.StageParticipant, error)
	createStage             func(stage domain.Stage) (domain.Stage, error)
	deleteStage             func(id int) error
	addStageParticipant     func(stageID int, participant domain.StageParticipant) (domain.StageParticipant, error)
	insertLeadForStage      func(name, email, phone string, stageID int) (int, error)
	updateStageParticipant  func(stageID int, participant domain.StageParticipant) error
	deleteStageParticipant  func(stageID, participantID int) error
	updateStageStats        func(stageID int, registrations *int, participants *int) error
	assignClientToStage     func(stageID, clientID int) error
	updateStageInfo         func(stageID int, name, date *string, adBudget *float64) error

	listSettings      func() ([]domain.AppSetting, error)
	getSetting        func(key string) (domain.AppSetting, error)
	upsertSetting     func(key string, valueNumeric *float64, valueText *string) error
	getNumericSetting func(key string, def float64) float64
	getTextSetting    func(key string, def string) string
}

func (m *mockStore) GetSalesProcess(id int) (domain.SalesProcess, error) {
	if m.getSalesProcess != nil {
		return m.getSalesProcess(id)
	}
	return domain.SalesProcess{}, nil
}

func (m *mockStore) ListSalesProcesses() ([]domain.SalesProcess, error) {
	if m.listSalesProcesses != nil {
		return m.listSalesProcesses()
	}
	return nil, nil
}

func (m *mockStore) ListCashflowEntries(f store.CashflowEntryFilter) ([]domain.CashflowEntry, int, error) {
	if m.listCashflowEntries != nil {
		return m.listCashflowEntries(f)
	}
	return nil, 0, nil
}

func (m *mockStore) CashflowForecast(start, end time.Time, avg float64, contractID *int) ([]domain.CashflowForecastRow, error) {
	if m.cashflowForecast != nil {
		return m.cashflowForecast(start, end, avg, contractID)
	}
	return nil, nil
}

func (m *mockStore) CashflowYTDPaid(start, end time.Time) (float64, error) {
	if m.cashflowYTDPaid != nil {
		return m.cashflowYTDPaid(start, end)
	}
	return 0, nil
}

func (m *mockStore) CashflowNextMonthsConfirmed(start, end time.Time) ([]domain.MonthConfirmed, error) {
	if m.cashflowNextMonthsConfirmed != nil {
		return m.cashflowNextMonthsConfirmed(start, end)
	}
	return nil, nil
}

func (m *mockStore) UpdateCashflowEntryStatus(id int, status string) error {
	if m.updateCashflowEntryStatus != nil {
		return m.updateCashflowEntryStatus(id, status)
	}
	return nil
}

func (m *mockStore) ListCommentsByEntity(entityType string, entityID int) ([]domain.Comment, error) {
	if m.listCommentsByEntity != nil {
		return m.listCommentsByEntity(entityType, entityID)
	}
	return nil, nil
}

func (m *mockStore) ListCommentsByClientID(clientID int) ([]domain.Comment, error) {
	if m.listCommentsByClientID != nil {
		return m.listCommentsByClientID(clientID)
	}
	return nil, nil
}

func (m *mockStore) CreateComment(entityType string, entityID int, author *string, body string, metadata map[string]interface{}) (domain.Comment, error) {
	if m.createComment != nil {
		return m.createComment(entityType, entityID, author, body, metadata)
	}
	return domain.Comment{}, nil
}

func (m *mockStore) DeleteComment(id int) error {
	if m.deleteComment != nil {
		return m.deleteComment(id)
	}
	return nil
}

func (m *mockStore) UpdateComment(id int, author *string, body *string, metadata *map[string]interface{}) (domain.Comment, error) {
	if m.updateComment != nil {
		return m.updateComment(id, author, body, metadata)
	}
	return domain.Comment{}, nil
}

func (m *mockStore) InsertCommentsForEntity(entityType string, entityID int, clientID int, comments []domain.Comment) error {
	if m.insertCommentsForEntity != nil {
		return m.insertCommentsForEntity(entityType, entityID, clientID, comments)
	}
	return nil
}

func (m *mockStore) SyncClientCompletedAt(ctx context.Context, clientID int, closed *bool, completedAt *string) error {
	if m.syncClientCompletedAt != nil {
		return m.syncClientCompletedAt(ctx, clientID, closed, completedAt)
	}
	return nil
}

func (m *mockStore) SyncClientStatusFromSales(ctx context.Context, salesProcessID int) error {
	if m.syncClientStatusFromSales != nil {
		return m.syncClientStatusFromSales(ctx, salesProcessID)
	}
	return nil
}

func (m *mockStore) ValidateClientCompletedAt(ctx context.Context, clientID int, completedAt *time.Time) error {
	if m.validateClientCompletedAt != nil {
		return m.validateClientCompletedAt(ctx, clientID, completedAt)
	}
	return nil
}

func (m *mockStore) InsertClient(ctx context.Context, name, email, phone, source string, sourceStageID *int, status string) (int, error) {
	if m.insertClient != nil {
		return m.insertClient(ctx, name, email, phone, source, sourceStageID, status)
	}
	return 0, nil
}

func (m *mockStore) DeleteClientWithLeadReset(ctx context.Context, clientID int) (bool, error) {
	if m.deleteClientWithLeadReset != nil {
		return m.deleteClientWithLeadReset(ctx, clientID)
	}
	return true, nil
}

func (m *mockStore) GetClientCompletedAt(ctx context.Context, clientID int) (*time.Time, error) {
	if m.getClientCompletedAt != nil {
		return m.getClientCompletedAt(ctx, clientID)
	}
	return nil, nil
}

func (m *mockStore) UpdateClientFields(ctx context.Context, id int, name, email, phone, source, status string, sourceStageID *int, sourceStageIDSet bool, completedAt *time.Time) error {
	if m.updateClientFields != nil {
		return m.updateClientFields(ctx, id, name, email, phone, source, status, sourceStageID, sourceStageIDSet, completedAt)
	}
	return nil
}

func (m *mockStore) ClearClientSalesProcessStageID(ctx context.Context, clientID int) error {
	if m.clearClientSalesProcessStageID != nil {
		return m.clearClientSalesProcessStageID(ctx, clientID)
	}
	return nil
}

func (m *mockStore) SyncLeadFromClient(ctx context.Context, clientID int, name, email, phone, source string, sourceStageID *int, sourceStageIDSet bool) error {
	if m.syncLeadFromClient != nil {
		return m.syncLeadFromClient(ctx, clientID, name, email, phone, source, sourceStageID, sourceStageIDSet)
	}
	return nil
}

func (m *mockStore) ListSettings() ([]domain.AppSetting, error) {
	if m.listSettings != nil {
		return m.listSettings()
	}
	return nil, nil
}

func (m *mockStore) GetSetting(key string) (domain.AppSetting, error) {
	if m.getSetting != nil {
		return m.getSetting(key)
	}
	return domain.AppSetting{}, nil
}

func (m *mockStore) UpsertSetting(key string, valueNumeric *float64, valueText *string) error {
	if m.upsertSetting != nil {
		return m.upsertSetting(key, valueNumeric, valueText)
	}
	return nil
}

func (m *mockStore) GetNumericSetting(key string, def float64) float64 {
	if m.getNumericSetting != nil {
		return m.getNumericSetting(key, def)
	}
	return def
}

func (m *mockStore) GetTextSetting(key string, def string) string {
	if m.getTextSetting != nil {
		return m.getTextSetting(key, def)
	}
	return def
}

func (m *mockStore) ExecuteRawQuery(ctx context.Context, sqlText string) ([]string, []map[string]interface{}, error) {
	if m.executeRawQuery != nil {
		return m.executeRawQuery(ctx, sqlText)
	}
	return nil, nil, nil
}

// Stages
func (m *mockStore) ListStages() ([]domain.Stage, error) {
	if m.listStages != nil {
		return m.listStages()
	}
	return nil, nil
}
func (m *mockStore) ListStageParticipants(stageID, limit, offset int) ([]domain.StageParticipant, error) {
	if m.listStageParticipants != nil {
		return m.listStageParticipants(stageID, limit, offset)
	}
	return nil, nil
}
func (m *mockStore) CreateStage(stage domain.Stage) (domain.Stage, error) {
	if m.createStage != nil {
		return m.createStage(stage)
	}
	return domain.Stage{}, nil
}
func (m *mockStore) DeleteStage(id int) error {
	if m.deleteStage != nil {
		return m.deleteStage(id)
	}
	return nil
}
func (m *mockStore) AddStageParticipant(stageID int, participant domain.StageParticipant) (domain.StageParticipant, error) {
	if m.addStageParticipant != nil {
		return m.addStageParticipant(stageID, participant)
	}
	return domain.StageParticipant{}, nil
}
func (m *mockStore) InsertLeadForStage(name, email, phone string, stageID int) (int, error) {
	if m.insertLeadForStage != nil {
		return m.insertLeadForStage(name, email, phone, stageID)
	}
	return 0, nil
}
func (m *mockStore) UpdateStageParticipant(stageID int, participant domain.StageParticipant) error {
	if m.updateStageParticipant != nil {
		return m.updateStageParticipant(stageID, participant)
	}
	return nil
}
func (m *mockStore) DeleteStageParticipant(stageID, participantID int) error {
	if m.deleteStageParticipant != nil {
		return m.deleteStageParticipant(stageID, participantID)
	}
	return nil
}
func (m *mockStore) UpdateStageStats(stageID int, registrations *int, participants *int) error {
	if m.updateStageStats != nil {
		return m.updateStageStats(stageID, registrations, participants)
	}
	return nil
}
func (m *mockStore) AssignClientToStage(stageID, clientID int) error {
	if m.assignClientToStage != nil {
		return m.assignClientToStage(stageID, clientID)
	}
	return nil
}
func (m *mockStore) UpdateStageInfo(stageID int, name, date *string, adBudget *float64) error {
	if m.updateStageInfo != nil {
		return m.updateStageInfo(stageID, name, date, adBudget)
	}
	return nil
}

// Dashboard
func (m *mockStore) GetContractsInRange(ctx context.Context, typ string, start, end *time.Time) ([]domain.ContractSummary, error) {
	if m.getContractsInRange != nil {
		return m.getContractsInRange(ctx, typ, start, end)
	}
	return nil, nil
}
func (m *mockStore) GetDashboardKPIs(ctx context.Context, start, end *time.Time) (domain.DashboardKPIsRaw, error) {
	if m.getDashboardKPIs != nil {
		return m.getDashboardKPIs(ctx, start, end)
	}
	return domain.DashboardKPIsRaw{}, nil
}
func (m *mockStore) GetMonthlyKPIs(ctx context.Context, year int) ([]domain.MonthlyKPIRaw, error) {
	if m.getMonthlyKPIs != nil {
		return m.getMonthlyKPIs(ctx, year)
	}
	return nil, nil
}

// Contracts
func (m *mockStore) ListContracts(ctx context.Context, includeExpired, includeComments, includeCashflow bool) ([]domain.ContractRow, error) {
	if m.listContracts != nil {
		return m.listContracts(ctx, includeExpired, includeComments, includeCashflow)
	}
	return nil, nil
}
func (m *mockStore) GetContractByID(ctx context.Context, id int) (domain.ContractRow, error) {
	if m.getContractByID != nil {
		return m.getContractByID(ctx, id)
	}
	return domain.ContractRow{}, nil
}
func (m *mockStore) GetContractCashflow(ctx context.Context, contractID int) ([]domain.CashflowEntry, error) {
	if m.getContractCashflow != nil {
		return m.getContractCashflow(ctx, contractID)
	}
	return nil, nil
}
func (m *mockStore) CreateContract(ctx context.Context, in store.ContractCreateInput) (int, *string, error) {
	if m.createContract != nil {
		return m.createContract(ctx, in)
	}
	return 0, nil, nil
}
func (m *mockStore) UpdateContract(ctx context.Context, id int, sd, ed time.Time, durationMonths int, revenueTotal float64, paymentFreq string) error {
	if m.updateContract != nil {
		return m.updateContract(ctx, id, sd, ed, durationMonths, revenueTotal, paymentFreq)
	}
	return nil
}
func (m *mockStore) GetContractClientID(ctx context.Context, contractID int) (int, error) {
	if m.getContractClientID != nil {
		return m.getContractClientID(ctx, contractID)
	}
	return 0, nil
}
func (m *mockStore) GetContractNotifyData(ctx context.Context, contractID int) (domain.ContractNotifyData, error) {
	if m.getContractNotifyData != nil {
		return m.getContractNotifyData(ctx, contractID)
	}
	return domain.ContractNotifyData{}, nil
}

// Sales
func (m *mockStore) UpdateSalesProcess(ctx context.Context, id int, in store.SalesUpdateInput) (int64, error) {
	if m.updateSalesProcess != nil {
		return m.updateSalesProcess(ctx, id, in)
	}
	return 1, nil
}
func (m *mockStore) GetSalesProcessClientID(ctx context.Context, salesProcessID int) (int, error) {
	if m.getSalesProcessClientID != nil {
		return m.getSalesProcessClientID(ctx, salesProcessID)
	}
	return 0, nil
}
func (m *mockStore) InsertSalesProcessComment(ctx context.Context, salesProcessID, clientID int, author *string, body string, metadata []byte) error {
	if m.insertSalesProcessComment != nil {
		return m.insertSalesProcessComment(ctx, salesProcessID, clientID, author, body, metadata)
	}
	return nil
}
func (m *mockStore) GetUpsellForSalesProcess(ctx context.Context, salesProcessID int) ([]domain.ContractUpsell, error) {
	if m.getUpsellForSalesProcess != nil {
		return m.getUpsellForSalesProcess(ctx, salesProcessID)
	}
	return nil, nil
}
func (m *mockStore) ListUpsells(ctx context.Context, startDate, endDate *time.Time) ([]domain.ContractUpsell, error) {
	if m.listUpsells != nil {
		return m.listUpsells(ctx, startDate, endDate)
	}
	return nil, nil
}
func (m *mockStore) CreateOrUpdateUpsell(ctx context.Context, in store.UpsellInput) (store.UpsellResult, error) {
	if m.createOrUpdateUpsell != nil {
		return m.createOrUpdateUpsell(ctx, in)
	}
	return store.UpsellResult{}, nil
}
func (m *mockStore) GetUpsellAnalytics(ctx context.Context, startDate, endDate *time.Time) (store.UpsellStats, []store.MonthlyRevenue, error) {
	if m.getUpsellAnalytics != nil {
		return m.getUpsellAnalytics(ctx, startDate, endDate)
	}
	return store.UpsellStats{}, nil, nil
}

// Sales start
func (m *mockStore) HasActiveContractForClient(ctx context.Context, clientID int) (bool, error) {
	if m.hasActiveContractForClient != nil {
		return m.hasActiveContractForClient(ctx, clientID)
	}
	return false, nil
}
func (m *mockStore) RunStartSalesProcess(ctx context.Context, in store.StartSalesInput, existingClientID, foundLeadID *int) (int, int, string, *int, error) {
	if m.runStartSalesProcess != nil {
		return m.runStartSalesProcess(ctx, in, existingClientID, foundLeadID)
	}
	return 0, 0, "", nil, nil
}
func (m *mockStore) ResolveLeadForSalesStart(ctx context.Context, leadID *int, email string) (*int, string, *int, error) {
	if m.resolveLeadForSalesStart != nil {
		return m.resolveLeadForSalesStart(ctx, leadID, email)
	}
	return nil, "", nil, nil
}
func (m *mockStore) GetExistingClientBasic(ctx context.Context, clientID int) (domain.ClientBasic, error) {
	if m.getExistingClientBasic != nil {
		return m.getExistingClientBasic(ctx, clientID)
	}
	return domain.ClientBasic{}, nil
}
func (m *mockStore) GetStartSalesResponseData(ctx context.Context, salesID, clientID int) ([]domain.Comment, domain.ClientBasic, error) {
	if m.getStartSalesResponseData != nil {
		return m.getStartSalesResponseData(ctx, salesID, clientID)
	}
	return nil, domain.ClientBasic{}, nil
}
func (m *mockStore) EnsureContractForClosedSales(ctx context.Context, salesProcessID, clientID int, in store.EnsureContractInput) (*domain.ContractNotifyData, error) {
	if m.ensureContractForClosedSales != nil {
		return m.ensureContractForClosedSales(ctx, salesProcessID, clientID, in)
	}
	return nil, nil
}

// Leads
func (m *mockStore) ListLeads(ctx context.Context) ([]domain.Lead, error) {
	if m.listLeads != nil {
		return m.listLeads(ctx)
	}
	return nil, nil
}
func (m *mockStore) CreateLead(ctx context.Context, name string, email, phone *string, source string, stageID *int) (domain.Lead, bool, error) {
	if m.createLead != nil {
		return m.createLead(ctx, name, email, phone, source, stageID)
	}
	return domain.Lead{}, false, nil
}
func (m *mockStore) UpdateLead(ctx context.Context, id int, name, email, phone, source *string, stageID *int) (domain.Lead, error) {
	if m.updateLead != nil {
		return m.updateLead(ctx, id, name, email, phone, source, stageID)
	}
	return domain.Lead{}, nil
}
func (m *mockStore) DeleteLead(ctx context.Context, id int) error {
	if m.deleteLead != nil {
		return m.deleteLead(ctx, id)
	}
	return nil
}
func (m *mockStore) ConvertLead(ctx context.Context, leadID int) (int, int, error) {
	if m.convertLead != nil {
		return m.convertLead(ctx, leadID)
	}
	return 0, 0, nil
}

// ListClients
func (m *mockStore) ListClients(ctx context.Context, includeInactive bool) ([]domain.ClientRow, error) {
	if m.listClients != nil {
		return m.listClients(ctx, includeInactive)
	}
	return nil, nil
}

// Exports
func (m *mockStore) ExportClientsRaw(ctx context.Context) ([][]string, error) {
	if m.exportClientsRaw != nil {
		return m.exportClientsRaw(ctx)
	}
	return nil, nil
}
func (m *mockStore) ExportContractsRaw(ctx context.Context) ([][]string, error) {
	if m.exportContractsRaw != nil {
		return m.exportContractsRaw(ctx)
	}
	return nil, nil
}
func (m *mockStore) ExportCashflowEntriesRaw(ctx context.Context) ([][]string, error) {
	if m.exportCashflowEntriesRaw != nil {
		return m.exportCashflowEntriesRaw(ctx)
	}
	return nil, nil
}
func (m *mockStore) ExportLegacyCashflow(ctx context.Context) (store.LegacyCashflowData, error) {
	if m.exportLegacyCashflow != nil {
		return m.exportLegacyCashflow(ctx)
	}
	return store.LegacyCashflowData{}, nil
}
func (m *mockStore) ExportAggregatedCashflow(ctx context.Context) (store.AggregatedCashflowData, error) {
	if m.exportAggregatedCashflow != nil {
		return m.exportAggregatedCashflow(ctx)
	}
	return store.AggregatedCashflowData{}, nil
}

// Importer
func (m *mockStore) TruncateAllTables(ctx context.Context) error { return nil }
func (m *mockStore) ImportContractRecord(ctx context.Context, in store.ImportContractInput) error {
	return nil
}
