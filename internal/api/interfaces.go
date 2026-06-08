// interfaces.go — appStore interface: the single dependency boundary between handlers and the database layer.
package api

import (
	"context"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// appStore is the persistence contract the api package requires.
// The concrete implementation lives in internal/store.
type appStore interface {
	GetSalesProcess(id int) (domain.SalesProcess, error)
	ListSalesProcesses() ([]domain.SalesProcess, error)

	ListCashflowEntries(f store.CashflowEntryFilter) ([]domain.CashflowEntry, int, error)
	CashflowForecast(start, end time.Time, avgRevenuePerContract float64, contractID *int) ([]domain.CashflowForecastRow, error)
	CashflowYTDPaid(start, end time.Time) (float64, error)
	CashflowNextMonthsConfirmed(start, end time.Time) ([]domain.MonthConfirmed, error)
	UpdateCashflowEntryStatus(id int, status string) error

	// Comments
	ListCommentsByEntity(entityType string, entityID int) ([]domain.Comment, error)
	ListCommentsByClientID(clientID int) ([]domain.Comment, error)
	CreateComment(entityType string, entityID int, author *string, body string, metadata map[string]interface{}) (domain.Comment, error)
	DeleteComment(id int) error
	UpdateComment(id int, author *string, body *string, metadata *map[string]interface{}) (domain.Comment, error)
	InsertCommentsForEntity(entityType string, entityID int, clientID int, comments []domain.Comment) error

	// Clients
	SyncClientCompletedAt(ctx context.Context, clientID int, closed *bool, completedAt *string) error
	SyncClientStatusFromSales(ctx context.Context, salesProcessID int) error
	ValidateClientCompletedAt(ctx context.Context, clientID int, completedAt *time.Time) error
	InsertClient(ctx context.Context, name, email, phone, source string, sourceStageID *int, status string) (int, error)
	DeleteClientWithLeadReset(ctx context.Context, clientID int) (bool, error)
	GetClientCompletedAt(ctx context.Context, clientID int) (*time.Time, error)
	UpdateClientFields(ctx context.Context, id int, name, email, phone, source, status string, sourceStageID *int, sourceStageIDSet bool, completedAt *time.Time) error
	ClearClientSalesProcessStageID(ctx context.Context, clientID int) error
	SyncLeadFromClient(ctx context.Context, clientID int, name, email, phone, source string, sourceStageID *int, sourceStageIDSet bool) error
	ListClients(ctx context.Context, includeInactive bool) ([]domain.ClientRow, error)

	// Settings
	ListSettings() ([]domain.AppSetting, error)
	GetSetting(key string) (domain.AppSetting, error)
	UpsertSetting(key string, valueNumeric *float64, valueText *string) error
	GetNumericSetting(key string, def float64) float64
	GetTextSetting(key string, def string) string

	// NLQ
	ExecuteRawQuery(ctx context.Context, sqlText string) ([]string, []map[string]interface{}, error)

	// Stages
	ListStages() ([]domain.Stage, error)
	ListStageParticipants(stageID, limit, offset int) ([]domain.StageParticipant, error)
	CreateStage(stage domain.Stage) (domain.Stage, error)
	DeleteStage(id int) error
	AddStageParticipant(stageID int, participant domain.StageParticipant) (domain.StageParticipant, error)
	InsertLeadForStage(name, email, phone string, stageID int) (int, error)
	UpdateStageParticipant(stageID int, participant domain.StageParticipant) error
	DeleteStageParticipant(stageID, participantID int) error
	UpdateStageStats(stageID int, registrations *int, participants *int) error
	AssignClientToStage(stageID, clientID int) error
	UpdateStageInfo(stageID int, name, date *string, adBudget *float64) error

	// Dashboard
	GetContractsInRange(ctx context.Context, typ string, start, end *time.Time) ([]domain.ContractSummary, error)
	GetDashboardKPIs(ctx context.Context, start, end *time.Time) (domain.DashboardKPIsRaw, error)
	GetMonthlyKPIs(ctx context.Context, year int) ([]domain.MonthlyKPIRaw, error)

	// Contracts
	ListContracts(ctx context.Context, includeExpired, includeComments, includeCashflow bool) ([]domain.ContractRow, error)
	GetContractByID(ctx context.Context, id int) (domain.ContractRow, error)
	GetContractCashflow(ctx context.Context, contractID int) ([]domain.CashflowEntry, error)
	CreateContract(ctx context.Context, in store.ContractCreateInput) (contractID int, createdAt *string, err error)
	UpdateContract(ctx context.Context, id int, sd, ed time.Time, durationMonths int, revenueTotal float64, paymentFreq string) error
	GetContractClientID(ctx context.Context, contractID int) (int, error)
	GetContractNotifyData(ctx context.Context, contractID int) (domain.ContractNotifyData, error)
	PauseContract(ctx context.Context, contractID int, newEndDate string, reason string) error
	// Sales
	UpdateSalesProcess(ctx context.Context, id int, in store.SalesUpdateInput) (rowsAffected int64, err error)
	GetSalesProcessClientID(ctx context.Context, salesProcessID int) (int, error)
	InsertSalesProcessComment(ctx context.Context, salesProcessID, clientID int, author *string, body string, metadata []byte) error
	GetUpsellForSalesProcess(ctx context.Context, salesProcessID int) ([]domain.ContractUpsell, error)
	ListUpsells(ctx context.Context, startDate, endDate *time.Time) ([]domain.ContractUpsell, error)
	CreateOrUpdateUpsell(ctx context.Context, in store.UpsellInput) (store.UpsellResult, error)
	GetUpsellAnalytics(ctx context.Context, startDate, endDate *time.Time) (store.UpsellStats, []store.MonthlyRevenue, error)

	// Sales start / conversion
	HasActiveContractForClient(ctx context.Context, clientID int) (bool, error)
	RunStartSalesProcess(ctx context.Context, in store.StartSalesInput, existingClientID, foundLeadID *int) (clientID, salesID int, stage string, effectiveLeadID *int, err error)
	ResolveLeadForSalesStart(ctx context.Context, leadID *int, email string) (foundLeadID *int, source string, stageID *int, err error)
	GetExistingClientBasic(ctx context.Context, clientID int) (domain.ClientBasic, error)
	GetStartSalesResponseData(ctx context.Context, salesID, clientID int) ([]domain.Comment, domain.ClientBasic, error)
	EnsureContractForClosedSales(ctx context.Context, salesProcessID, clientID int, in store.EnsureContractInput) (*domain.ContractNotifyData, error)

	// Leads
	ListLeads(ctx context.Context) ([]domain.Lead, error)
	CreateLead(ctx context.Context, name string, email, phone *string, source string, stageID *int) (domain.Lead, bool, error)
	UpdateLead(ctx context.Context, id int, name, email, phone, source *string, stageID *int) (domain.Lead, error)
	DeleteLead(ctx context.Context, id int) error
	ConvertLead(ctx context.Context, leadID int) (clientID, salesProcessID int, err error)

	// Exports
	ExportClientsRaw(ctx context.Context) ([][]string, error)
	ExportContractsRaw(ctx context.Context) ([][]string, error)
	ExportCashflowEntriesRaw(ctx context.Context) ([][]string, error)
	ExportLegacyCashflow(ctx context.Context) (store.LegacyCashflowData, error)
	ExportAggregatedCashflow(ctx context.Context) (store.AggregatedCashflowData, error)

	// Importer
	TruncateAllTables(ctx context.Context) error
	ImportContractRecord(ctx context.Context, in store.ImportContractInput) error
}
