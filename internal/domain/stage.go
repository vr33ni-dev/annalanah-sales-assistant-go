package domain

type Stage struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	Date             *string  `json:"date,omitempty"`
	AdBudget         *float64 `json:"ad_budget,omitempty"`
	Registrations    *int     `json:"registrations,omitempty"`
	Participants     *int     `json:"participants,omitempty"`      // manual
	RecordedContacts *int     `json:"recorded_contacts,omitempty"` // derived
	ClosedContracts  *int     `json:"closed_contracts,omitempty"`
	ActualRevenue    *float64 `json:"actual_revenue,omitempty"`
	AttendanceRate   *float64 `json:"attendance_rate,omitempty"`
	ClosingRate      *float64 `json:"closing_rate,omitempty"`
	ROI              *float64 `json:"roi,omitempty"`
	MonetaryMode     string   `json:"monetary_mode"`
}

type StageParticipant struct {
	ID      int `json:"id"`
	StageID int `json:"stage_id"`

	LinkedClientID *int `json:"linked_client_id,omitempty"`
	LinkedLeadID   *int `json:"linked_lead_id,omitempty"`

	ParticipantName  string  `json:"name"`
	ParticipantEmail *string `json:"email,omitempty"`
	ParticipantPhone *string `json:"phone,omitempty"`

	Attended  *bool   `json:"attended,omitempty"`
	CreatedAt *string `json:"created_at,omitempty"`
}
