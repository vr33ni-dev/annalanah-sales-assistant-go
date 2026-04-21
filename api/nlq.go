package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"golang.org/x/sync/singleflight"
)

type nlqRequest struct {
	Question string `json:"question"`
}

type nlqResponse struct {
	SQL      string                   `json:"sql"`
	Columns  []string                 `json:"columns,omitempty"`
	Rows     []map[string]interface{} `json:"rows"`
	RowCount int                      `json:"row_count"`
	Cached   bool                     `json:"cached,omitempty"`
	Error    string                   `json:"error,omitempty"`
}

// sqlResultCacheEntry holds a cached NLQ response and its expiration
type sqlResultCacheEntry struct {
	Response nlqResponse
	Expires  time.Time
}

// sqlResultCache is a simple in-memory cache for SQL query results
type sqlResultCache struct {
	mu    sync.Mutex
	data  map[string]sqlResultCacheEntry
	group singleflight.Group
	ttl   time.Duration
}

// questionToSQLCache is a simple in-memory cache for NLQ question to SQL mapping
type questionToSQLCache struct {
	mu   sync.Mutex
	data map[string]struct {
		SQL     string
		Expires time.Time
	}
	ttl   time.Duration
	group singleflight.Group
}

func normalizeQuestionCacheKey(question string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(question))), " ")
}

func NewQuestionToSQLCache(ttl time.Duration) *questionToSQLCache {
	return &questionToSQLCache{
		data: make(map[string]struct {
			SQL     string
			Expires time.Time
		}),
		ttl: ttl,
	}
}

func (c *questionToSQLCache) Get(question string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.data[normalizeQuestionCacheKey(question)]
	if !ok || time.Now().After(entry.Expires) {
		return "", false
	}
	return entry.SQL, true
}

func (c *questionToSQLCache) Set(question, sql string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[normalizeQuestionCacheKey(question)] = struct {
		SQL     string
		Expires time.Time
	}{SQL: sql, Expires: time.Now().Add(c.ttl)}
}

func NewSQLResultCache(ttl time.Duration) *sqlResultCache {
	return &sqlResultCache{
		data: make(map[string]sqlResultCacheEntry),
		ttl:  ttl,
	}
}

// Get by SQL string as key
func (c *sqlResultCache) Get(sql string) (nlqResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.data[sql]
	if !ok || time.Now().After(entry.Expires) {
		return nlqResponse{}, false
	}
	return entry.Response, true
}

// Set by SQL string as key
func (c *sqlResultCache) Set(sql string, resp nlqResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[sql] = sqlResultCacheEntry{Response: resp, Expires: time.Now().Add(c.ttl)}
}

// In-memory process-local caches (not persisted).
// - `sqlCache`: caches SQL query results (keyed by exact SQL string). TTL: 5 minutes.
// - `questionCache`: caches NLQ question -> SQL mappings to avoid repeated Anthropic calls. TTL: 30 minutes.
// These are stored per-process and will be lost on restart.
var sqlCache = NewSQLResultCache(5 * time.Minute)
var questionCache = NewQuestionToSQLCache(30 * time.Minute)
var nlqGenerateSQL = generateSQL

var (
	anthropicClientMu     sync.Mutex
	anthropicClient       *anthropic.Client
	anthropicClientAPIKey string
)

func getAnthropicClient(apiKey string) *anthropic.Client {
	anthropicClientMu.Lock()
	defer anthropicClientMu.Unlock()

	if anthropicClient != nil && anthropicClientAPIKey == apiKey {
		return anthropicClient
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	anthropicClient = &client
	anthropicClientAPIKey = apiKey
	return anthropicClient
}

func isLikelySQLQuestion(q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))

	keywords := []string{
		// English
		"client", "customer", "revenue", "follow", "contract",
		"sales", "stage", "status", "list", "show", "report", "data", "query",
		// German
		"kunde", "kunden", "umsatz", "vertrag", "zweitgespräch", "follow-up",
		"phase", "stufe", "status", "analyse", "bericht", "zeige", "liste", "upsell",
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

	log.Printf("NLQ question=%q", req.Question)

	if !isLikelySQLQuestion(req.Question) {
		writeJSON(w, map[string]any{
			"answer": "🤖 Ich kann dir bei Datenabfragen helfen, z. B. 'Zeige mir Kunden mit geplantem Zweitgespräch'.",
		})
		return
	}

	// First, try to get SQL from questionToSQLCache
	sqlText, found := questionCache.Get(req.Question)
	if !found {
		v, err, _ := questionCache.group.Do(normalizeQuestionCacheKey(req.Question), func() (interface{}, error) {
			if cachedSQL, ok := questionCache.Get(req.Question); ok {
				return cachedSQL, nil
			}

			generatedSQL, err := nlqGenerateSQL(ctx, req.Question)
			if err != nil {
				return "", err
			}

			generatedSQL = strings.TrimSpace(generatedSQL)
			questionCache.Set(req.Question, generatedSQL)
			return generatedSQL, nil
		})
		if err != nil {
			writeJSONErr(w, nlqResponse{Error: err.Error()}, err)
			return
		}
		sqlText = strings.TrimSpace(v.(string))
	}

	if !isSelect(sqlText) || !isSafeSQL(sqlText) {
		writeJSON(w, nlqResponse{
			Error: "Unsafe SQL detected",
			SQL:   sqlText,
		})
		return
	}

	sqlText = strings.TrimSuffix(sqlText, ";")
	sqlText = strings.TrimSpace(sqlText)

	if !hasLimit(sqlText) && !isAggregateQuery(sqlText) {
		sqlText += " LIMIT 100"
	}

	// Now check cache by SQL string
	if cached, ok := sqlCache.Get(sqlText); ok {
		cached.Cached = true
		writeJSON(w, cached)
		return
	}

	// Use singleflight to prevent stampede on SQL execution
	v, err, _ := sqlCache.group.Do(sqlText, func() (interface{}, error) {
		if os.Getenv("NLQ_MOCK") == "1" || h.DB == nil {
			resp := nlqResponse{
				SQL:     sqlText,
				Columns: []string{},
				Rows:    []map[string]interface{}{},
				Error:   "",
			}
			sqlCache.Set(sqlText, resp)
			return resp, nil
		}

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		start := time.Now()

		rows, err := h.DB.QueryContext(ctx, sqlText)
		if err != nil {
			log.Printf("NLQ query error: %v | SQL=%s", err, sqlText)
			resp := nlqResponse{Error: err.Error(), SQL: sqlText}
			sqlCache.Set(sqlText, resp)
			return resp, nil
		}

		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			log.Printf("NLQ columns error: %v | SQL=%s", err, sqlText)
			resp := nlqResponse{Error: err.Error(), SQL: sqlText}
			sqlCache.Set(sqlText, resp)
			return resp, nil
		}

		results := make([]map[string]interface{}, 0)
		for rows.Next() {
			columnVals := make([]interface{}, len(cols))
			columnPtrs := make([]interface{}, len(cols))
			for i := range columnVals {
				columnPtrs[i] = &columnVals[i]
			}
			if err := rows.Scan(columnPtrs...); err != nil {
				log.Printf("NLQ scan error: %v | SQL=%s", err, sqlText)
				resp := nlqResponse{Error: err.Error(), SQL: sqlText}
				sqlCache.Set(sqlText, resp)
				return resp, nil
			}

			rowMap := make(map[string]interface{}, len(cols))
			for i, col := range cols {
				val := columnVals[i]
				switch v := val.(type) {
				case nil:
					rowMap[col] = nil
				case []byte:
					s := strings.TrimSpace(string(v))
					if s == "t" || s == "true" || s == "1" {
						rowMap[col] = true
					} else if s == "f" || s == "false" || s == "0" {
						rowMap[col] = false
					} else {
						rowMap[col] = s
					}
				case bool, int64, float64, string:
					rowMap[col] = v
				default:
					rowMap[col] = fmt.Sprintf("%v", v)
				}
			}
			results = append(results, rowMap)
		}
		if err := rows.Err(); err != nil {
			log.Printf("NLQ rows iteration error: %v | SQL=%s", err, sqlText)
			resp := nlqResponse{Error: err.Error(), SQL: sqlText}
			sqlCache.Set(sqlText, resp)
			return resp, nil
		}

		resp := nlqResponse{
			SQL:      sqlText,
			Columns:  cols,
			Rows:     results,
			RowCount: len(results),
		}
		sqlCache.Set(sqlText, resp)
		duration := time.Since(start)
		log.Printf("Executed NLQ SQL in %v: %s", duration, sqlText)
		return resp, nil
	})
	if err != nil {
		writeJSONErr(w, nlqResponse{Error: err.Error()}, err)
		return
	}
	writeJSON(w, v.(nlqResponse))
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONErr writes a JSON error response with an appropriate HTTP status code.
// For Anthropic API errors it maps the upstream status code; otherwise 500.
func writeJSONErr(w http.ResponseWriter, v interface{}, err error) {
	w.Header().Set("Content-Type", "application/json")
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusPaymentRequired: // 402
			w.WriteHeader(http.StatusPaymentRequired)
		case http.StatusTooManyRequests: // 429
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	_ = json.NewEncoder(w).Encode(v)
}

func isSelect(sql string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(sql)), "select")
}

func isSafeSQL(sql string) bool {
	s := strings.ToLower(sql)

	forbidden := []string{
		";", "--", "/*", "*/",
		"insert ", "update ", "delete ",
		"drop ", "alter ", "truncate ",
		"create ", "grant ", "revoke ",
		"pg_sleep", "pg_terminate",
	}

	for _, f := range forbidden {
		if strings.Contains(s, f) {
			return false
		}
	}
	return true
}

func hasLimit(sql string) bool {
	re := regexp.MustCompile(`(?i)\blimit\b`)
	return re.MatchString(sql)
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
// 2) no ANTHROPIC_API_KEY -> simple hardcoded fallback
// 3) ANTHROPIC_API_KEY set -> use Claude with schemaDoc
func generateSQL(ctx context.Context, question string) (string, error) {
	q := strings.ToLower(strings.TrimSpace(question))

	// ---------- 1) Explicit mock mode for tests ----------
	if os.Getenv("NLQ_MOCK") == "1" {
		switch {
		case strings.Contains(q, "wie viele kunden"):
			return "SELECT COUNT(*) FROM clients", nil

		case strings.Contains(q, "wie viele stages") || strings.Contains(q, "wieviele stages"):
			return "SELECT COUNT(*) FROM stages", nil

		case strings.Contains(q, "keine verlängerung") || strings.Contains(q, "keine verlaengerung") || strings.Contains(q, "nicht verlängert") || strings.Contains(q, "nicht verlaengert"):
			return "SELECT COUNT(*) FROM contract_upsells WHERE upsell_result = 'keine_verlaengerung'", nil

		case strings.Contains(q, "upsell-umsatz") || strings.Contains(q, "upsell umsatz") || strings.Contains(q, "renewal revenue"):
			return "SELECT COALESCE(SUM(upsell_revenue), 0) AS total_upsell_revenue FROM contract_upsells", nil

		case strings.Contains(q, "upsell gespräch") || strings.Contains(q, "upsell gespr") || strings.Contains(q, "benötigte upsell") || strings.Contains(q, "bald ablaufend"):
			return `SELECT c.id, c.name, ct.end_date FROM contracts ct JOIN clients c ON c.id = ct.client_id WHERE ct.end_date BETWEEN CURRENT_DATE AND CURRENT_DATE + INTERVAL '60 days' AND NOT EXISTS (SELECT 1 FROM contract_upsells cu WHERE cu.previous_contract_id = ct.id) ORDER BY ct.end_date`, nil

		case strings.Contains(q, "verlängerung") || strings.Contains(q, "verlaengerung") || strings.Contains(q, "renewal"):
			return "SELECT id, client_id, upsell_date, upsell_result, upsell_revenue FROM contract_upsells WHERE upsell_result = 'verlaengerung' LIMIT 50", nil

		case strings.Contains(q, "aktive kunden"):
			return "SELECT id, name, email, status FROM clients WHERE status = 'active'", nil

		default:
			return "SELECT id, name, email, status FROM clients LIMIT 10", nil
		}
	}

	// ---------- 2) Offline fallback (no API key) ----------
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		switch {
		case strings.Contains(q, "zweitgespräch"):
			return `SELECT c.id, c.name, sp.stage, sp.follow_up_date
			        FROM sales_process sp
			        JOIN clients c ON c.id = sp.client_id
			        WHERE sp.stage = 'follow_up'
							  AND sp.follow_up_date <= CURRENT_DATE
			        ORDER BY sp.follow_up_date NULLS FIRST
			        LIMIT 100`, nil
		case strings.Contains(q, "wie viele stages") || strings.Contains(q, "wieviele stages"):
			return "SELECT COUNT(*) FROM stages", nil
		default:
			return "SELECT id, name, email, status FROM clients LIMIT 10", nil
		}
	}

	// ---------- 3) Real Anthropic-backed generation ----------
	client := getAnthropicClient(apiKey)

	msg, err := client.Messages.New(
		ctx,
		anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeHaiku4_5,
			MaxTokens: 1024,
			System: []anthropic.TextBlockParam{
				{Text: schemaDoc, CacheControl: anthropic.NewCacheControlEphemeralParam()},
			},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(question)),
			},
		},
	)
	if err != nil {
		return "", err
	}

	if len(msg.Content) == 0 {
		return "", fmt.Errorf("no response from Anthropic")
	}

	txt := strings.TrimSpace(msg.Content[0].AsText().Text)
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
