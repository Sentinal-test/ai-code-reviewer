package database

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

func InitDB(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		dbPath = "./review.db"
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	createTables := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		github_id TEXT UNIQUE,
		llm_api_key TEXT,
		github_access_token TEXT
	);

	CREATE TABLE IF NOT EXISTS repo_settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		repo_id TEXT UNIQUE,
		user_id INTEGER,
		is_active BOOLEAN DEFAULT TRUE,
		security_enabled BOOLEAN DEFAULT TRUE,
		bug_enabled BOOLEAN DEFAULT TRUE,
		lint_enabled BOOLEAN DEFAULT TRUE,
		performance_enabled BOOLEAN DEFAULT TRUE,
		architecture_enabled BOOLEAN DEFAULT TRUE,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);`

	_, err = db.Exec(createTables)
	if err != nil {
		return nil, err
	}

	// Migration: Attempt to add columns to existing tables if they don't exist.
	// SQLite doesn't support IF NOT EXISTS in ALTER TABLE, so we just try and ignore error.
	migrationQueries := []string{
		"ALTER TABLE users ADD COLUMN github_access_token TEXT;",
		"ALTER TABLE repo_settings ADD COLUMN is_active BOOLEAN DEFAULT TRUE;",
	}

	for _, query := range migrationQueries {
		_, _ = db.Exec(query) // Ignore errors (e.g. column already exists)
	}

	return db, nil
}
