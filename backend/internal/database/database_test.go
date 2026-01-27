package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitDB(t *testing.T) {
	// Use in-memory SQLite for testing
	db, err := InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Verify connection
	err = db.Ping()
	require.NoError(t, err)

	// Verify Tables Exist
	tables := []string{"users", "repo_settings"}
	for _, table := range tables {
		var name string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		assert.NoError(t, err, "Table %s should exist", table)
		assert.Equal(t, table, name)
	}

	// Verify Columns (Migration Check)
	// Check if users table has github_access_token
	// SQLite schema info is a bit tricky, let's just try to select from it
	_, err = db.Exec("SELECT github_access_token FROM users LIMIT 1")
	assert.NoError(t, err, "Column github_access_token should exist in users table")

	// Check if repo_settings table has is_active
	_, err = db.Exec("SELECT is_active FROM repo_settings LIMIT 1")
	assert.NoError(t, err, "Column is_active should exist in repo_settings table")
}
