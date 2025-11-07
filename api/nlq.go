package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

type nlqRequest struct {
	Question string `json:"question"`
}

type nlqResponse struct {
	SQL   string        `json:"sql"`
	Rows  []interface{} `json:"rows"`
	Error string        `json:"error,omitempty"`
}

// RunNLQ handles POST /api/nlq
func (h *Handler) RunNLQ(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req nlqRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", 400)
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

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(ctx, sqlText)
	if err != nil {
		writeJSON(w, nlqResponse{Error: err.Error(), SQL: sqlText})
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	data := make([]interface{}, 0)
	for rows.Next() {
		colsData := make([]interface{}, len(cols))
		colsPtrs := make([]interface{}, len(cols))
		for i := range colsData {
			colsPtrs[i] = &colsData[i]
		}
		if err := rows.Scan(colsPtrs...); err != nil {
			writeJSON(w, nlqResponse{Error: err.Error(), SQL: sqlText})
			return
		}
		rowMap := map[string]interface{}{}
		for i, col := range cols {
			rowMap[col] = colsData[i]
		}
		data = append(data, rowMap)
	}

	writeJSON(w, nlqResponse{SQL: sqlText, Rows: data})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func isSelect(sql string) bool {
	re := regexp.MustCompile(`(?i)^\s*select\s`)
	return re.MatchString(sql)
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
  follow_up_result BOOLEAN,   -- true = appeared / call happened, false = no-show
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


- "Erschienen":
    means sp.follow_up_result = TRUE.

- "Nicht erschienen":
    means sp.follow_up_result = FALSE OR (sp.follow_up_date < CURRENT_DATE AND sp.follow_up_result IS NULL).

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
`

func generateSQL(ctx context.Context, question string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")

	// 🚧 Fallback for local dev
	if apiKey == "" {
		fmt.Println("⚠️  OPENAI_API_KEY not set — using local fallback mode")

		// Simple heuristic examples so you can test without the API
		q := strings.ToLower(question)
		switch {
		case strings.Contains(q, "zweitgespräch"):
			return `SELECT c.id, c.name, sp.stage, sp.follow_up_date
			        FROM sales_process sp
			        JOIN clients c ON c.id = sp.client_id
			        WHERE (sp.stage = 'follow_up' OR sp.stage_id = 2)
			          AND (sp.follow_up_result IS NULL)
			          AND (sp.follow_up_date IS NULL OR sp.follow_up_date < NOW() - INTERVAL '14 days')
			        ORDER BY sp.follow_up_date NULLS FIRST
			        LIMIT 100;`, nil
		case strings.Contains(q, "client") && strings.Contains(q, "revenue"):
			return `SELECT c.name, SUM(sp.revenue) AS total_revenue
			        FROM sales_process sp
			        JOIN clients c ON c.id = sp.client_id
			        GROUP BY c.name
			        ORDER BY total_revenue DESC
			        LIMIT 100;`, nil
		default:
			// Generic stub query
			return `SELECT id, name, email, status FROM clients LIMIT 10;`, nil
		}
	}

	// ✅ Real LLM call (when API key exists)
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

	txt := resp.Choices[0].Message.Content
	re := regexp.MustCompile("(?is)```sql\\s*(.*?)\\s*```")
	m := re.FindStringSubmatch(txt)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1]), nil
	}

	// fallback: assume the model already output raw SQL (no code block)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(txt)), "select") {
		return strings.TrimSpace(txt), nil
	}

	return "", fmt.Errorf("no SQL found in model output: %s", txt)
}
