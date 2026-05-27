package store

import (
	"context"
	"fmt"
	"strings"
)

// ExecuteRawQuery runs an arbitrary (pre-validated) SELECT and returns column
// names and rows as generic maps. Only used by the NLQ handler.
func (s *PostgresStore) ExecuteRawQuery(ctx context.Context, sqlText string) ([]string, []map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		columnVals := make([]interface{}, len(cols))
		columnPtrs := make([]interface{}, len(cols))
		for i := range columnVals {
			columnPtrs[i] = &columnVals[i]
		}
		if err := rows.Scan(columnPtrs...); err != nil {
			return nil, nil, err
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
	return cols, results, rows.Err()
}
