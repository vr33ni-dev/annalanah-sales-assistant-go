package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func RunMigrations(database *sql.DB) error {
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version bigint not null primary key,
			dirty boolean not null
		)
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	var currentVersion int64
	err := database.QueryRow(`SELECT version FROM schema_migrations WHERE dirty = false ORDER BY version DESC LIMIT 1`).Scan(&currentVersion)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("read schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}

		version, err := strconv.ParseInt(strings.SplitN(entry.Name(), "_", 2)[0], 10, 64)
		if err != nil {
			return fmt.Errorf("parse version from %s: %w", entry.Name(), err)
		}
		if version <= currentVersion {
			continue
		}

		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		if err := execMigration(database, string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}

		if currentVersion == 0 {
			_, err = database.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES ($1, false)`, version)
		} else {
			_, err = database.Exec(`UPDATE schema_migrations SET version = $1, dirty = false`, version)
		}
		if err != nil {
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
		currentVersion = version

		log.Printf("migration applied: %s", entry.Name())
	}

	return nil
}

func execMigration(db *sql.DB, sqlContent string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	for _, stmt := range splitStatements(sqlContent) {
		if _, execErr := tx.Exec(stmt); execErr != nil {
			_ = tx.Rollback()
			return execErr
		}
	}

	return tx.Commit()
}

// splitStatements splits a SQL file into individual statements on semicolons,
// skipping semicolons inside $$ dollar-quoted blocks and stripping any
// standalone transaction-control statements (BEGIN/COMMIT/ROLLBACK) since the
// runner owns the transaction.
func splitStatements(sql string) []string {
	var stmts []string
	var cur strings.Builder
	inDollarQuote := false

	for i := 0; i < len(sql); i++ {
		if !inDollarQuote && i+1 < len(sql) && sql[i] == '$' && sql[i+1] == '$' {
			inDollarQuote = true
			cur.WriteString("$$")
			i++
			continue
		}
		if inDollarQuote && i+1 < len(sql) && sql[i] == '$' && sql[i+1] == '$' {
			inDollarQuote = false
			cur.WriteString("$$")
			i++
			continue
		}
		if !inDollarQuote && sql[i] == ';' {
			if stmt := strings.TrimSpace(cur.String()); stmt != "" && !isOnlyComments(stmt) && !isTransactionControl(stmt) {
				stmts = append(stmts, stmt)
			}
			cur.Reset()
			continue
		}
		cur.WriteByte(sql[i])
	}
	if stmt := strings.TrimSpace(cur.String()); stmt != "" && !isOnlyComments(stmt) && !isTransactionControl(stmt) {
		stmts = append(stmts, stmt)
	}
	return stmts
}

func isOnlyComments(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "--") {
			return false
		}
	}
	return true
}

func isTransactionControl(s string) bool {
	upper := strings.ToUpper(strings.TrimSpace(s))
	return upper == "BEGIN" || upper == "COMMIT" || upper == "ROLLBACK" ||
		upper == "BEGIN TRANSACTION" || upper == "START TRANSACTION"
}

