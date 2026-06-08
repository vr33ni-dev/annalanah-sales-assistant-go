package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func (s *PostgresStore) ListSettings() ([]domain.AppSetting, error) {
	rows, err := s.db.Query(`
		SELECT key, value_numeric, value_text, CAST(updated_at AS text)
		FROM app_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.AppSetting
	for rows.Next() {
		var setting domain.AppSetting
		var vn sql.NullFloat64
		var vt, ua sql.NullString
		if err := rows.Scan(&setting.Key, &vn, &vt, &ua); err != nil {
			return nil, err
		}
		if vn.Valid {
			setting.ValueNumeric = &vn.Float64
		}
		if vt.Valid {
			setting.ValueText = &vt.String
		}
		setting.UpdatedAt = normalizeSettingUpdatedAt(ua)
		out = append(out, setting)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetSetting(key string) (domain.AppSetting, error) {
	var vn sql.NullFloat64
	var vt, ua sql.NullString
	err := s.db.QueryRow(`
		SELECT value_numeric, value_text, CAST(updated_at AS text)
		FROM app_settings WHERE key = $1`, key).Scan(&vn, &vt, &ua)
	if err == sql.ErrNoRows {
		return domain.AppSetting{}, ErrNotFound
	}
	if err != nil {
		return domain.AppSetting{}, err
	}

	setting := domain.AppSetting{Key: key, UpdatedAt: normalizeSettingUpdatedAt(ua)}
	if vn.Valid {
		setting.ValueNumeric = &vn.Float64
	}
	if vt.Valid {
		setting.ValueText = &vt.String
	}
	return setting, nil
}

func (s *PostgresStore) UpsertSetting(key string, valueNumeric *float64, valueText *string) error {
	_, err := s.db.Exec(`
		INSERT INTO app_settings (key, value_numeric, value_text, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (key) DO UPDATE
		SET value_numeric = EXCLUDED.value_numeric,
		    value_text    = EXCLUDED.value_text,
		    updated_at    = now()
	`, key, valueNumeric, valueText)
	return err
}

func (s *PostgresStore) GetNumericSetting(key string, def float64) float64 {
	var v sql.NullFloat64
	_ = s.db.QueryRow(`SELECT value_numeric FROM app_settings WHERE key = $1`, key).Scan(&v)
	if v.Valid {
		return v.Float64
	}
	return def
}

func (s *PostgresStore) GetTextSetting(key string, def string) string {
	var v sql.NullString
	_ = s.db.QueryRow(`SELECT value_text FROM app_settings WHERE key = $1`, key).Scan(&v)
	if v.Valid {
		if trimmed := strings.TrimSpace(v.String); trimmed != "" {
			return trimmed
		}
	}
	return def
}

func normalizeSettingUpdatedAt(raw sql.NullString) *string {
	if !raw.Valid {
		return nil
	}
	s := strings.TrimSpace(raw.String)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, s); err == nil {
			normalized := ts.UTC().Format(time.RFC3339)
			return &normalized
		}
	}
	return &s
}
