package domain

// ClientRow is the result of ListClients: includes a computed status and pre-loaded comments.
type ClientRow struct {
	ID              int64
	LeadID          *int64
	Name            string
	Email           string
	Phone           string
	Source          string
	SourceStageName string
	Status          string
	CompletedAt     *string
	Comments        []Comment
}

// ClientBasic is a minimal client record used in sales-start responses.
type ClientBasic struct {
	ID            int
	Name          string
	Email         string
	Phone         string
	Source        string
	SourceStageID *int
}
