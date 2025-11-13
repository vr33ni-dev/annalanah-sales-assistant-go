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
		writeJSON(w, map[string]any{
			"answer": "🤖 Ich kann dir bei Datenabfragen helfen, z. B. 'Zeige mir Kunden mit geplantem Zweitgespräch'.",
		})
		return
	}

	sqlText, err := generateSQL(ctx, req.Question)
	if err != nil {
		writeJSON(w, nlqResponse{Error: err.Error()})
		return
	}

	sqlText = strings.TrimSpace(sqlText)

	if !isSelect(sqlText) {
		writeJSON(w, nlqResponse{
			Error: fmt.Sprintf("only SELECT queries allowed (got: %s)", sqlText),
			SQL:   sqlText,
		})
		return
	}

	// normalize: strip trailing semicolon
	sqlText = strings.TrimSuffix(sqlText, ";")
	sqlText = strings.TrimSpace(sqlText)

	// If it's NOT an aggregate-style query and has no LIMIT, append LIMIT 100
	if !hasLimit(sqlText) && !isAggregateQuery(sqlText) {
		sqlText += " LIMIT 100"
	}

	// In mock mode or without DB: just return the SQL, don't execute it.
	if os.Getenv("NLQ_MOCK") == "1" || h.DB == nil {
		writeJSON(w, nlqResponse{
			SQL:     sqlText,
			Columns: []string{},
			Rows:    []map[string]interface{}{},
			Error:   "",
		})
		return
	}

	// Real execution path
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(ctx, sqlText)
	if err != nil {
		writeJSON(w, nlqResponse{Error: err.Error(), SQL: sqlText})
		return
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		writeJSON(w, nlqResponse{Error: err.Error(), SQL: sqlText})
		return
	}

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
				// copy []byte -> string
				rowMap[col] = string(append([]byte(nil), v...))
			default:
				// JSON round-trip to avoid pointer aliasing issues
				b, _ := json.Marshal(v)
				var copyVal interface{}
				_ = json.Unmarshal(b, &copyVal)
				rowMap[col] = copyVal
			}
		}

		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		writeJSON(w, nlqResponse{Error: err.Error(), SQL: sqlText})
		return
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
	return strings.Contains(strings.ToLower(sql), " limit ")
}

// very dumb heuristic: if it has COUNT( / SUM( / AVG(, treat as aggregate
func isAggregateQuery(sql string) bool {
	s := strings.ToLower(sql)
	return strings.Contains(s, "count(") ||
		strings.Contains(s, "sum(") ||
		strings.Contains(s, "avg(")
}

// generateSQL uses three modes:
// 1) NLQ_MOCK=1  -> deterministic for tests
// 2) no OPENAI key -> simple hardcoded fallback
// 3) OPENAI key set -> use OpenAI with schemaDoc
func generateSQL(ctx context.Context, question string) (string, error) {
	q := strings.ToLower(strings.TrimSpace(question))

	// ---------- 1) Explicit mock mode for tests ----------
	if os.Getenv("NLQ_MOCK") == "1" {
		switch {
		case strings.Contains(q, "wie viele kunden"):
			return "SELECT COUNT(*) FROM clients", nil

		case strings.Contains(q, "wie viele stages") || strings.Contains(q, "wieviele stages"):
			return "SELECT COUNT(*) FROM stages", nil

		case strings.Contains(q, "aktive kunden"):
			return "SELECT id, name, email, status FROM clients WHERE status = 'active'", nil

		default:
			return "SELECT id, name, email, status FROM clients LIMIT 10", nil
		}
	}

	// ---------- 2) Offline fallback (no API key) ----------
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		switch {
		case strings.Contains(q, "zweitgespräch"):
			return `SELECT c.id, c.name, sp.stage, sp.follow_up_date
			        FROM sales_process sp
			        JOIN clients c ON c.id = sp.client_id
			        WHERE sp.stage = 'follow_up'
			        ORDER BY sp.follow_up_date NULLS FIRST
			        LIMIT 100`, nil
		case strings.Contains(q, "wie viele stages") || strings.Contains(q, "wieviele stages"):
			return "SELECT COUNT(*) FROM stages", nil
		default:
			return "SELECT id, name, email, status FROM clients LIMIT 10", nil
		}
	}

	// ---------- 3) Real OpenAI-backed generation ----------
	client := openai.NewClient(apiKey)

	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: "gpt-4o-mini",
			Messages: []openai.ChatCompletionMessage{
				{Role: "system", Content: schemaDoc}, // defined in nlq_schema.go (same package)
				{Role: "user", Content: question},
			},
			Temperature: 0,
		},
	)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices from OpenAI")
	}

	txt := strings.TrimSpace(resp.Choices[0].Message.Content)
	txt = strings.ReplaceAll(txt, "```sql", "")
	txt = strings.ReplaceAll(txt, "```", "")
	txt = strings.TrimSpace(txt)

	if !isSelect(txt) {
		return "", fmt.Errorf("no SQL found in model output: %s", txt)
	}

	// normalize (no semicolon here; RunNLQ will handle LIMIT logic)
	txt = strings.TrimSuffix(txt, ";")
	return strings.TrimSpace(txt), nil
}
