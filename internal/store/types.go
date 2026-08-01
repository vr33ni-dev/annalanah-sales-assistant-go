package store

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicateEmail = errors.New("duplicate email")

const (
	monetaryModeNetto  = "netto"
	monetaryModeBrutto = "brutto"
)

type CashflowEntryFilter struct {
	ContractID string
	ClientID   string
	Status     string
	StartDate  *time.Time
	EndDate    *time.Time
	Page       int
	PerPage    int
	SortOrder  string // "asc" (default) or "desc"
}

// ContractCreateInput is the set of parameters used to create a contract (inside a transaction).
type ContractCreateInput struct {
	ClientID          int
	SalesProcessID    *int
	StartDate         time.Time
	EndDate           *time.Time
	DurationMonths    int
	RevenueTotal      float64
	PaymentFreq       string
	CreatedAtOverride *time.Time
	GenerateSchedule  bool
	Source            string // "manual" or "imported"; defaults to "manual" if empty
}

// SalesUpdateInput carries the nullable fields for UpdateSalesProcess.
type SalesUpdateInput struct {
	InitialContactDate *string
	FollowUpDate       *string
	FollowUpResult     *bool
	Closed             *bool
	Revenue            *float64
	StageIDProvided    bool
	StageID            *int
}

// UpsellInput is the payload for CreateOrUpdateUpsell.
type UpsellInput struct {
	SalesProcessID         int
	ClientID               int
	UpsellDateProvided     bool
	ResolvedUpsellDate     *string
	UpsellResult           *string
	UpsellRevenue          *float64
	ContractStartDate      *string
	ContractDurationMonths *int
	ContractFrequency      *string
}

// UpsellResult is what CreateOrUpdateUpsell returns to the handler.
type UpsellResult struct {
	UpsellID      int
	Updated       bool
	NewContractID *int
	// Fields for the async notification (nil when no new contract was created)
	NotifyContractID     *int
	NotifyRevenue        *float64
	NotifyStartDate      *time.Time
	NotifySalesProcessID *int
}

// UpsellStats is returned by GetUpsellAnalytics.
type UpsellStats struct {
	VerlaengerungCount      int
	KeineVerlaengerungCount int
	ScheduledCount          int
	Verlaengerungsquote     *float64
	UmsatzSumBrutto         float64
}

// MonthlyRevenue is one entry in the analytics breakdown.
type MonthlyRevenue struct {
	Month   string
	Revenue float64
}

// StartSalesInput holds the distilled fields needed by RunStartSalesProcess.
type StartSalesInput struct {
	Name               string
	Email              string
	Phone              string
	Source             string
	SourceStageID      *int
	InitialContactDate *string
	FollowUpDate       *string
	MergeStrategy      *string
}

// EnsureContractInput carries the contract fields needed by EnsureContractForClosedSales.
type EnsureContractInput struct {
	Closed                 *bool
	Revenue                *float64
	ContractDurationMonths *int
	ContractStartDate      *string
	ContractFrequency      *string
}

// ImportContractInput is a single pre-parsed record sent to ImportContractRecord.
type ImportContractInput struct {
	ClientName            string
	Email                 string
	Status                string
	CreatedAt             time.Time
	SalesProcessCreatedAt time.Time
	IsFormer              bool

	PeriodContracts []PeriodContract
}

// PeriodContract holds one 6-month (or full) contract period for the importer.
type PeriodContract struct {
	Start          time.Time
	End            time.Time
	DurationMonths int
	Revenue        float64
	PaymentFreq    string
	Cashflows      map[string]interface{}
	IsUpsell       bool // true for period index > 0
	PrevContractID int  // set by the store after inserting period[i-1]
	IsNonRenewal   bool // flag to insert keine_verlaengerung
}

// LegacyCashflowClientRow is one client row in the legacy cashflow export.
type LegacyCashflowClientRow struct {
	ID              int
	Name            string
	Status          string
	StartDate       string
	EndDate         string
	CLV             float64
	Source          string
	SourceStageName string
}

// LegacyCashflowData is everything the legacy cashflow export handler needs.
type LegacyCashflowData struct {
	Clients             []LegacyCashflowClientRow
	AmountByClientMonth map[int]map[string]float64
	UpsellsByClient     map[int]map[string]int
	CommentsByClient    map[int][]string
}

// AggregatedCashflowClientRow is one client row in the aggregated cashflow export.
type AggregatedCashflowClientRow struct {
	ID              int
	Name            string
	Email           string
	Phone           string
	Source          string
	SourceStageName string
	Status          string
	StartDate       string
	EndDate         string
	TotalRevenue    float64
}

// AggregatedCashflowData is everything the aggregated cashflow export handler needs.
type AggregatedCashflowData struct {
	Clients             []AggregatedCashflowClientRow
	AmountByClientMonth map[int]map[string]float64
}
