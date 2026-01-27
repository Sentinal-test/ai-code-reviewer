package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"code-review/backend/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

// Helper to add query params - simple version for MVP
func addMyOptions(s string, opt *github.ListOptions) string {
	if opt == nil {
		return s
	}
	return fmt.Sprintf("%s?per_page=%d&page=%d", s, opt.PerPage, opt.Page)
}

// ListReposHandler fetches repositories the user has access to from GitHub
func ListReposHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_session")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		githubID := cookie.Value

		// 1. Get Access Token
		var accessToken string
		err = db.QueryRow("SELECT github_access_token FROM users WHERE github_id = ?", githubID).Scan(&accessToken)
		if err != nil || accessToken == "" {
			http.Error(w, "No access token found (Try logging in again)", http.StatusUnauthorized)
			return
		}

		// 2. Fetch Repos from GitHub
		// We use the access token to list repos.
		// NOTE: User must have installed the App on these repos for us to actually review them.
		// Ideally, we list installations user has access to.
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})
		tc := oauth2.NewClient(ctx, ts)
		client := github.NewClient(tc)

		// List user installations to find accessible repos
		opts := &github.ListOptions{PerPage: 100}
		installations, _, err := client.Apps.ListUserInstallations(ctx, opts)
		if err != nil {
			fmt.Printf("Error fetching installations: %v\n", err)
			http.Error(w, "Failed to fetch installations", http.StatusInternalServerError)
			return
		}

		type RepoDTO struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
			IsActive bool   `json:"is_active"`
		}
		var repos []RepoDTO

		// For each installation, list repos
		// This might be slow if many installations, but usually just one or few organizations.
		for _, inst := range installations {
			instID := inst.GetID()
			// ListUserInstallationsRepositories is missing in this version of go-github?
			// Use manual request: GET /user/installations/{installation_id}/repositories
			u := fmt.Sprintf("user/installations/%v/repositories", instID)
			u = addMyOptions(u, opts)

			req, err := client.NewRequest("GET", u, nil)
			if err != nil {
				fmt.Printf("Warning: failed to create req for inst %d: %v\n", instID, err)
				continue
			}

			var instRepos struct {
				TotalCount   int                  `json:"total_count"`
				Repositories []*github.Repository `json:"repositories"`
			}
			_, err = client.Do(ctx, req, &instRepos)
			if err != nil {
				fmt.Printf("Warning: failed to list repos for inst %d: %v\n", instID, err)
				continue
			}

			for _, r := range instRepos.Repositories {
				repos = append(repos, RepoDTO{
					ID:       r.GetID(),
					FullName: r.GetFullName(),
					IsActive: true, // Default true for UI until we check DB
				})
			}
		}

		// 3. Merge with DB Settings (IsActive status)
		// We'll query all settings for this user and map them.
		// Wait, repo_settings has user_id, but multiple users might be in same repo.
		// For MVP, settings are per-repo owner (user_id who configured it).
		// Or we share settings? `repo_settings` has `user_id`, implying per-user config?
		// But `processPR` fetches by `repo_id` regardless of user.
		// It seems `repo_settings` is intended to be single source for the repo.
		// The `user_id` probably tracks who last updated it or "owns" it.
		// Let's just fetch by repo_id.

		// Optimization: fetch all repo settings for these IDs
		// SQLite doesn't have easy "WHERE IN (...)". We'll iterate or just fetch locally active ones if array small.
		// Let's just query one by one or fetch all for user.
		// Actually, let's fetch all settings that this user "owns" or just rely on defaults.
		// Better: return what is active in generating the view.
		for i, repo := range repos {
			var isActive bool
			repoIDStr := fmt.Sprintf("%d", repo.ID)
			err := db.QueryRow("SELECT is_active FROM repo_settings WHERE repo_id = ?", repoIDStr).Scan(&isActive)
			if err == nil {
				repos[i].IsActive = isActive
			} else {
				// Not in DB means it uses default (True) or hasn't been configured.
				// Logic says defaults are True.
				repos[i].IsActive = true
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(repos)
	}
}

// UpdateUserAPIKeyHandler updates the global LLM key
func UpdateUserAPIKeyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_session")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		githubID := cookie.Value

		var req struct {
			APIKey string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		_, err = db.Exec("UPDATE users SET llm_api_key = ? WHERE github_id = ?", req.APIKey, githubID)
		if err != nil {
			http.Error(w, "DB Error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// UpdateRepoSettingsHandler updates layers and active status
func UpdateRepoSettingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_session")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		githubID := cookie.Value

		var req struct {
			RepoID   string              `json:"repo_id"`
			IsActive bool                `json:"is_active"`
			Layers   models.RepoSettings `json:"layers"` // We'll map flat fields
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Get User ID
		var userID int
		err = db.QueryRow("SELECT id FROM users WHERE github_id = ?", githubID).Scan(&userID)
		if err != nil {
			http.Error(w, "User not found", http.StatusInternalServerError)
			return
		}

		// Upsert Repo Settings
		_, err = db.Exec(`
			INSERT INTO repo_settings (
				repo_id, user_id, is_active, 
				security_enabled, bug_enabled, lint_enabled, performance_enabled, architecture_enabled
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_id) DO UPDATE SET
				user_id=excluded.user_id,
				is_active=excluded.is_active,
				security_enabled=excluded.security_enabled,
				bug_enabled=excluded.bug_enabled,
				lint_enabled=excluded.lint_enabled,
				performance_enabled=excluded.performance_enabled,
				architecture_enabled=excluded.architecture_enabled
		`,
			req.RepoID, userID, req.IsActive,
			req.Layers.SecurityEnabled, req.Layers.BugEnabled, req.Layers.LintEnabled, req.Layers.PerformanceEnabled, req.Layers.ArchitectureEnabled,
		)

		if err != nil {
			fmt.Printf("DB Error: %v\n", err)
			http.Error(w, "DB Error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// GetRepoSettingsHandler (Moved from main.go)
func GetRepoSettingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID := chi.URLParam(r, "repoID")
		var settings models.RepoSettings
		// Defaults
		settings.SecurityEnabled = true
		settings.BugEnabled = true
		settings.LintEnabled = true
		settings.PerformanceEnabled = true
		settings.ArchitectureEnabled = true
		settings.IsActive = true

		err := db.QueryRow(`
			SELECT is_active, security_enabled, bug_enabled, lint_enabled, performance_enabled, architecture_enabled
			FROM repo_settings
			WHERE repo_id = ?
		`, repoID).Scan(
			&settings.IsActive,
			&settings.SecurityEnabled, &settings.BugEnabled, &settings.LintEnabled, &settings.PerformanceEnabled, &settings.ArchitectureEnabled,
		)

		if err != nil && err != sql.ErrNoRows {
			http.Error(w, "DB Error", http.StatusInternalServerError)
			return
		}
		// If rows not found, we return defaults (Active=True)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)
	}
}


func init() {
	// Ensure repo_settings table exists
	// This is a simple migration step for MVP
}	func EnsureRepoSettingsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS repo_settings (
			repo_id TEXT PRIMARY KEY,
			user_id INTEGER,
			is_active BOOLEAN DEFAULT 1,
			security_enabled BOOLEAN DEFAULT 1,
			bug_enabled BOOLEAN DEFAULT 1,
			lint_enabled BOOLEAN DEFAULT 1,
			performance_enabled BOOLEAN DEFAULT 1,
			architecture_enabled BOOLEAN DEFAULT 1,
			FOREIGN KEY(user_id) REFERENCES users(id)
		)
	`)
	return err
}