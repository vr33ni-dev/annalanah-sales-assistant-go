package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

type nlqRequest struct {
	Question string `json:"question"`
}

type nlqResponse struct {
	SQL     string                   `json:"sql"`
	Columns []string                 `json:"columns,omitempty"`
	Rows    []map[string]interface{} `json:"rows"`
	Error   string                   `json:"error,omitempty"`
}

func isLikelySQLQuestion(q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))

	keywords := []string{
		// English
		"client", "customer", "revenue", "follow", "contract",
		"sales", "stage", "status", "list", "show", "report", "data", "query",
		// German
		"kunde", "kunden", "umsatz", "vertrag", "zweitgespräch", "follow-up",
		"phase", "stufe", "status", "analyse", "bericht", "zeige", "liste",
	}

	for _, k := range keywords {
		if strings.Contains(q, k) {
			return true
		}
	}
	return false
}

// RunNLQ handles POST /api/nlq
func (h *Handler) RunNLQ(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req nlqRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	if !isLikelySQLQuestion(req.Question) {
		writeJSON(w, map[string]interface{}{
			"answer": "🤖 Ich kann dir bei Datenabfragen helfen, z. B. 'Zeige mir Kunden mit geplantem Zweitgespräch'.",
		})
		return
	}

	sqlText, err := generateSQL(ctx, req.Question)
	if err != nil {
		writeJSON(w, nlqResponse{Error: err.Error()})
		return
	}

	if !isSelect(sqlText) {
		writeJSON(w, nlqResponse{Error: "only SELECT queries allowed"})
		return
	}
	if !hasLimit(sqlText) {
		sqlText += " LIMIT 100"
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(ctx, sqlText)
	if err != nil {
		writeJSON(w, nlqResponse{Error: err.Error(), SQL: sqlText})
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	results := make([]map[string]interface{}, 0)

	for rows.Next() {
		columnVals := make([]interface{}, len(cols))
		columnPtrs := make([]interface{}, len(cols))
		for i := range columnVals {
			columnPtrs[i] = &columnVals[i]
		}

		if err := rows.Scan(columnPtrs...); err != nil {
			writeJSON(w, nlqResponse{Error: err.Error(), SQL: sqlText})
			return
		}

		rowMap := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			val := columnVals[i]
			switch v := val.(type) {
			case []byte:
				rowMap[col] = string(append([]byte(nil), v...)) // clone bytes
			default:
				// Deep copy value before storing
				b, _ := json.Marshal(v)
				var copyVal interface{}
				_ = json.Unmarshal(b, &copyVal)
				rowMap[col] = copyVal
			}
		}

		results = append(results, rowMap)
	}

	writeJSON(w, nlqResponse{
		SQL:     sqlText,
		Columns: cols,
		Rows:    results,
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func isSelect(sql string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(sql)), "select")
}

func hasLimit(sql string) bool {
	return strings.Contains(strings.ToLower(sql), "limit")
}

var schemaDoc = `
You are an expert SQL translator for a CRM & sales tracking system.

Generate a SINGLE PostgreSQL SELECT query based ONLY on this schema:

TABLE clients (
  id SERIAL PRIMARY KEY,
  name TEXT,
  email TEXT,
  phone TEXT,
  source TEXT CHECK (source IN ('organic','paid')),
  source_stage_id INT REFERENCES stages(id),
  status TEXT CHECK (status IN ('active','follow_up_scheduled','awaiting_response','lost','inactive')),
  completed_at TIMESTAMPTZ
);

TABLE sales_process (
  id SERIAL PRIMARY KEY,
  client_id INT REFERENCES clients(id),
  stage TEXT CHECK (stage IN ('follow_up','closed','lost')),
  follow_up_date DATE,
  follow_up_result BOOLEAN,
  closed BOOLEAN,
  revenue NUMERIC,
  stage_id INT REFERENCES stages(id),
  created_at TIMESTAMP,
  updated_at TIMESTAMP
);

TABLE contracts (
  id SERIAL PRIMARY KEY,
  client_id INT,
  sales_process_id INT,
  start_date DATE,
  end_date DATE,
  duration_months INT,
  revenue_total NUMERIC,
  payment_frequency TEXT CHECK (payment_frequency IN ('monthly','bi-monthly','quarterly'))
);

TABLE stages (
  id SERIAL PRIMARY KEY,
  name TEXT,
  date DATE,
  ad_budget NUMERIC,
  registrations INT,
  participants INT
);

---------------------------
-- SEMANTIC MAPPINGS
---------------------------

When the user speaks in natural language, interpret as:

- "Zweitgespräch", "Follow-up":
    use sales_process.stage = 'follow_up'.

- "Kein Ergebnis", "noch offen", "wartet auf Rückmeldung":
    means a follow-up took place (sp.follow_up_result = TRUE)
    but no final decision (sp.closed IS NULL OR sp.closed = FALSE).

- "Zweitgespräch geplant", "noch kein Zweitgespräch durchgeführt":
    means follow-up is scheduled but not yet completed.
    In SQL: sp.follow_up_result IS NULL AND sp.follow_up_date >= CURRENT_DATE.

- "Zweitgespräch überfällig", "nicht erschienen":
    means follow-up date has passed and no attendance was recorded.
    In SQL: sp.follow_up_result IS NULL AND sp.follow_up_date < CURRENT_DATE.

- "hatten ein Zweitgespräch", "Zweitgespräch hatte", 
  "Follow-Up noch aussteht", "kein Ergebnis nach Zweitgespräch":
    means the follow-up has already taken place (not scheduled in the future)
    and there is no final decision yet.
    Use together:
      sp.stage = 'follow_up'
      AND sp.follow_up_result = TRUE
      AND sp.follow_up_date < CURRENT_DATE
      AND (sp.closed IS NULL OR sp.closed = FALSE)

- "alle Kunden und deren Zweitgespräch-Termine", "potenzielle, aktive und verlorene Kunden mit Zweitgespräch":
    means include ALL clients (regardless of status) who have an entry in sales_process,
    even if the stage is 'follow_up', 'closed', or 'lost'.
    In SQL: use LEFT JOIN between clients and sales_process, 
    and no stage filter unless explicitly requested.


- "Erschienen":
    means sp.follow_up_result = TRUE.

- "Nicht erschienen":
    means sp.follow_up_result = FALSE OR (sp.follow_up_date < CURRENT_DATE AND sp.follow_up_result IS NULL).


- "nicht abgeschlossen", "noch nicht abgeschlossen", "offen", "noch offen", "nicht beendet", "nicht geschlossen":
    means clients whose sales process is NOT yet closed.
    In SQL: (sp.closed IS NULL OR sp.closed = FALSE)

- "Abgeschlossen", "Closed Won":
    means sp.stage = 'closed' AND sp.closed = TRUE.

- "Verloren", "Closed Lost":
    means sp.stage = 'lost' OR (sp.closed = FALSE AND sp.stage = 'lost').

---------------------------
-- QUERY STYLE RULES
---------------------------

- Only generate a single valid SELECT statement.
- Use only the tables/columns defined above.
- Prefer clear aliases: c for clients, sp for sales_process, ct for contracts, st for stages.
- Use proper joins on id fields (e.g. sp.client_id = c.id).
- If the user asks for "overdue", compare follow_up_date with CURRENT_DATE.
- If time ranges are mentioned ("this month", "last 30 days"), convert to appropriate WHERE clauses.
- If no LIMIT is mentioned, append "LIMIT 100".
- DO NOT include comments, explanations, markdown, or code fences.
- Output MUST be ONLY the SQL text.

---------------------------
-- ADDITIONAL INSTRUCTIONS
---------------------------

When generating queries about clients, follow-ups, or Zweitgespräche:
- Always include identifying client info (c.name, c.email).
- Include sp.follow_up_date AS zweites_gespraech_datum if relevant.
- Include stage, follow_up_result, and closed columns if context involves progress or status.
`

func generateSQL(ctx context.Context, question string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")

	if apiKey == "" {
		fmt.Println("⚠️  OPENAI_API_KEY not set — using local fallback mode")
		q := strings.ToLower(question)
		switch {
		case strings.Contains(q, "zweitgespräch"):
			return `SELECT c.id, c.name, sp.stage, sp.follow_up_date
			        FROM sales_process sp
			        JOIN clients c ON c.id = sp.client_id
			        WHERE sp.stage = 'follow_up'
			          AND sp.follow_up_result IS NULL
			          AND (sp.follow_up_date IS NULL OR sp.follow_up_date < NOW() - INTERVAL '14 days')
			        ORDER BY sp.follow_up_date NULLS FIRST
			        LIMIT 100;`, nil
		default:
			return `SELECT id, name, email, status FROM clients LIMIT 10;`, nil
		}
	}

	client := openai.NewClient(apiKey)

	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: "gpt-4o-mini",
			Messages: []openai.ChatCompletionMessage{
				{Role: "system", Content: schemaDoc},
				{Role: "user", Content: question},
			},
		},
	)
	if err != nil {
		return "", err
	}

	txt := strings.TrimSpace(resp.Choices[0].Message.Content)
	txt = strings.ReplaceAll(txt, "```sql", "")
	txt = strings.ReplaceAll(txt, "```", "")
	txt = strings.TrimSpace(txt)

	if !strings.HasPrefix(strings.ToLower(txt), "select") {
		return "", fmt.Errorf("no SQL found in model output: %s", txt)
	}
	return txt, nil
}
