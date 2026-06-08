package store

import (
	"database/sql"
	"time"
)

type PostgresStore struct{ db *sql.DB }

func New(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }

func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func nullTimeToString(nt sql.NullTime, layout string) *string {
	if !nt.Valid {
		return nil
	}
	s := nt.Time.Format(layout)
	return &s
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(i *int) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*i), Valid: true}
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil || t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
