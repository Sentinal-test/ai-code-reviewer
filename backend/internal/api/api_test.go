package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"code-review/backend/internal/database"
	"code-review/backend/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateUserAPIKeyHandler(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert test user
	// We need to create the table first? InitDB does that.
	_, err = db.Exec("INSERT INTO users (github_id, llm_api_key) VALUES (?, ?)", "123", "old-key")
	require.NoError(t, err)

	handler := UpdateUserAPIKeyHandler(db)

	// Prepare request
	reqBody := map[string]string{"api_key": "new-key"}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/api/users/apikey", bytes.NewBuffer(bodyBytes))
	req.AddCookie(&http.Cookie{Name: "auth_session", Value: "123"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Result().StatusCode)

	// Verify DB update
	var newKey string
	err = db.QueryRow("SELECT llm_api_key FROM users WHERE github_id = ?", "123").Scan(&newKey)
	require.NoError(t, err)
	assert.Equal(t, "new-key", newKey)
}

func TestGetRepoSettingsHandler(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert settings
	// Need user first for FK?
	db.Exec("INSERT INTO users (id, github_id) VALUES (1, '123')")
	_, err = db.Exec(`INSERT INTO repo_settings (repo_id, user_id, is_active, security_enabled) VALUES (?, 1, 0, 1)`, "repo-2")
	require.NoError(t, err)

	// Use chi router to inject params
	r := chi.NewRouter()
	r.Get("/settings/{repoID}", GetRepoSettingsHandler(db))

	req := httptest.NewRequest("GET", "/settings/repo-2", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Result().StatusCode)

	var settings models.RepoSettings
	err = json.Unmarshal(w.Body.Bytes(), &settings)
	require.NoError(t, err)

	assert.False(t, settings.IsActive)
	assert.True(t, settings.SecurityEnabled)
}
