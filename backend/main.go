package main

import (
	"code-review/backend/internal/database"
	internalGH "code-review/backend/internal/github"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"code-review/backend/internal/api"
	"code-review/backend/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/go-github/v60/github"
	"github.com/joho/godotenv"
)

type Config struct {
	GitHubAppID     int64
	GitHubAppSecret string
	WebhookSecret   string
}

func main() {
	// 1. Load environment variables
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Note: .env file not found, using system environment variables\n")
	}

	// 2. Validate required environment variables
	requiredEnv := []string{
		"GITHUB_APP_ID",
		"GITHUB_PRIVATE_KEY_PATH",
		"GITHUB_WEBHOOK_SECRET",
		"GITHUB_CLIENT_ID",
		"GITHUB_CLIENT_SECRET",
	}
	for _, env := range requiredEnv {
		if os.Getenv(env) == "" {
			fmt.Printf("CRITICAL: Environment variable %s is missing\n", env)
			os.Exit(1)
		}
	}

	if os.Getenv("GEMINI_API_KEY") == "" {
		fmt.Printf("Note: GEMINI_API_KEY is not set. Dashboard settings might be required for reviews to work.\n")
	}

	// 3. Initialize DB
	db, err := database.InitDB("./review.db") // Explicit path or from helper
	if err != nil {
		fmt.Printf("CRITICAL: Error initializing DB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// 3.5 Init Auth
	auth.InitAuth()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// 4. Basic CORS configuration
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Post("/webhook", handleWebhook(db))
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// Auth Routes
	r.Get("/auth/login", auth.LoginHandler)
	r.Get("/auth/callback", auth.CallbackHandler(db))

	// API Routes
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.AuthMiddleware) // Protect API
		r.Get("/me", auth.GetMeHandler(db))
		r.Get("/repos", api.ListReposHandler(db))
		r.Put("/users/apikey", api.UpdateUserAPIKeyHandler(db))
		r.Put("/settings", api.UpdateRepoSettingsHandler(db))
		r.Get("/settings/{repoID}", api.GetRepoSettingsHandler(db))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🚀 Server starting on port %s...\n", port)
	http.ListenAndServe(":"+port, r)
}

func handleWebhook(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		eventID := r.Header.Get("X-GitHub-Delivery")
		eventType := r.Header.Get("X-GitHub-Event")
		signature := r.Header.Get("X-Hub-Signature-256")

		fmt.Printf("📥 Received Webhook | ID: %s | Event: %s\n", eventID, eventType)

		secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
		payload, err := github.ValidatePayload(r, []byte(secret))
		if err != nil {
			fmt.Printf("❌ Signature Validation Failed: %v | Signature: %s | Secret Length: %d\n", err, signature, len(secret))
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		event, err := github.ParseWebHook(eventType, payload)
		if err != nil {
			fmt.Printf("❌ Parsing Failed: %v\n", err)
			http.Error(w, "Could not parse event", http.StatusBadRequest)
			return
		}

		switch e := event.(type) {
		case *github.PullRequestEvent:
			action := e.GetAction()
			fmt.Printf("🐙 Pull Request Event | Action: %s | PR: #%d | Repo: %s\n", action, e.GetPullRequest().GetNumber(), e.GetRepo().GetFullName())
			if action == "opened" || action == "synchronize" {
				go processPR(e, db) // Run in background to respond fast
			}
		default:
			fmt.Printf("ℹ️  Ignoring unsupported event type: %s\n", eventType)
		}

		w.WriteHeader(http.StatusOK)
	}
}

func processPR(event *github.PullRequestEvent, db *sql.DB) {
	ctx := context.Background()
	repo := event.GetRepo()
	pr := event.GetPullRequest()
	repoID := fmt.Sprintf("%d", repo.GetID())
	installationID := event.GetInstallation().GetID()
	commitSHA := pr.GetHead().GetSHA()

	fmt.Printf("🏗️  Processing PR #%d | Repo: %s | Commit: %.7s | Installation: %d\n",
		pr.GetNumber(), repo.GetFullName(), commitSHA, installationID)

	// 1. Get Repo Settings & API Key (with fallbacks)
	var settings models.RepoSettings
	var apiKey string
	err := db.QueryRow(`
		SELECT rs.is_active, rs.security_enabled, rs.bug_enabled, rs.lint_enabled, rs.performance_enabled, rs.architecture_enabled, u.llm_api_key 
		FROM repo_settings rs 
		JOIN users u ON rs.user_id = u.id 
		WHERE rs.repo_id = ?`, repoID).Scan(
		&settings.IsActive, &settings.SecurityEnabled, &settings.BugEnabled, &settings.LintEnabled, &settings.PerformanceEnabled, &settings.ArchitectureEnabled, &apiKey,
	)

	if err == sql.ErrNoRows {
		fmt.Printf("ℹ️  No settings found for repo ID %s. Using safe defaults (All layers enabled).\n", repoID)
		settings = models.RepoSettings{
			IsActive:            true,
			SecurityEnabled:     true,
			BugEnabled:          true,
			LintEnabled:         true,
			PerformanceEnabled:  true,
			ArchitectureEnabled: true,
		}
	} else if err != nil {
		fmt.Printf("❌ DB Error fetching settings for repo %s: %v. Falling back to defaults.\n", repoID, err)
		settings = models.RepoSettings{IsActive: true, SecurityEnabled: true, BugEnabled: true} // Basic fallback
	}

	if !settings.IsActive {
		fmt.Printf("ℹ️  Repo %s is disabled for AI review. Skipping.\n", repo.GetFullName())
		return
	}

	// API Key fallback to environment variable
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			fmt.Printf("⚠️  Skipping PR #%d: No API key found in DB or environment (GEMINI_API_KEY).\n", pr.GetNumber())
			return
		}
		fmt.Printf("ℹ️  Using default system API key for review.\n")
	}

	// 2. Auth as GitHub App
	appIDStr := os.Getenv("GITHUB_APP_ID")
	var appID int64
	fmt.Sscanf(appIDStr, "%d", &appID)
	pkPath := os.Getenv("GITHUB_PRIVATE_KEY_PATH")
	pkBytes, err := os.ReadFile(pkPath)
	if err != nil {
		fmt.Printf("❌ Failed to read private key: %v\n", err)
		return
	}

	client, err := internalGH.GetInstallationClient(ctx, appID, installationID, pkBytes)
	if err != nil {
		fmt.Printf("❌ GitHub App Auth Failed: %v\n", err)
		return
	}

	// 2.5 Start Check Run
	checkRunID, err := internalGH.CreateCheckRun(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), commitSHA)
	if err != nil {
		fmt.Printf("⚠️ Failed to create check run: %v\n", err)
	}

	// 3. Fetch PR Commits for context
	fmt.Printf("📜 Fetching commits for PR #%d...\n", pr.GetNumber())
	commits, _, err := client.PullRequests.ListCommits(ctx, repo.GetOwner().GetLogin(), repo.GetName(), pr.GetNumber(), &github.ListOptions{PerPage: 20})
	var commitMessages []string
	if err != nil {
		fmt.Printf("⚠️ Could not fetch commits: %v (continuing without commit context)\n", err)
	} else {
		for _, c := range commits {
			if c.Commit != nil && c.Commit.Message != nil {
				// Take first line of commit message
				msg := *c.Commit.Message
				if idx := strings.Index(msg, "\n"); idx != -1 {
					msg = msg[:idx]
				}
				commitMessages = append(commitMessages, msg)
			}
		}
	}

	// Build PR Context
	prContext := models.PRContext{
		Title:          pr.GetTitle(),
		Body:           pr.GetBody(),
		CommitMessages: commitMessages,
	}
	fmt.Printf("📋 PR Context Log:\n  Title: %s\n  Body (len): %d\n  Commits:\n",
		prContext.Title, len(prContext.Body))
	for _, commit := range prContext.CommitMessages {
		fmt.Printf("    - %s\n", commit)
	}

	// 4. Fetch Diff
	fmt.Printf("🔍 Fetching diff for PR #%d...\n", pr.GetNumber())
	diff, err := internalGH.FetchPRDiff(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), pr.GetNumber())
	if err != nil {
		fmt.Printf("❌ Failed to fetch diff: %v\n", err)
		return
	}

	if diff == "" {
		fmt.Printf("ℹ️  Empty diff for PR #%d, skipping review.\n", pr.GetNumber())
		return
	}

	// 4.5 Fetch Repo Structure
	fmt.Printf("📂 Fetching repo structure for %s...\n", repo.GetFullName())
	// Use default branch or the PR base branch. Using PR base is better.
	baseBranch := pr.GetBase().GetRef()
	repoStructure, err := internalGH.GetRepoTree(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), baseBranch)
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch repo structure: %v (continuing without it)\n", err)
		repoStructure = ""
	} else {
		fmt.Printf("✅ Repo structure fetched (%d chars)\n", len(repoStructure))
	}

	// 4.6 Fetch Changed Files Content
	fmt.Printf("📄 Fetching content of changed files...\n")
	changedFiles, err := internalGH.GetChangedFilesContent(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), pr.GetNumber(), commitSHA)
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch changed files content: %v\n", err)
		changedFiles = make(map[string]string)
	}

	// 4.7 Scout Pass: Analyze Dependencies
	dependencies := make(map[string]string)
	fmt.Printf("🕵️‍♀️ Scout Pass: Analyzing dependency needs...\n")
	depPaths, err := llm.AnalyzeDependencyNeeds(ctx, nil, diff, changedFiles, repoStructure, prContext, apiKey)
	if err != nil {
		fmt.Printf("⚠️ Scout Pass failed: %v\n", err)
	} else {
		fmt.Printf("🔍 Scout identified %d dependencies: %v\n", len(depPaths), depPaths)
		for _, path := range depPaths {
			// Skip if already in changedFiles
			if _, exists := changedFiles[path]; exists {
				continue
			}
			fmt.Printf("  📥 Fetching dependency: %s\n", path)
			content, err := internalGH.GetFileContent(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), path, commitSHA)
			if err != nil {
				fmt.Printf("  ⚠️ Failed to fetch dependency %s: %v\n", path, err)
			} else {
				dependencies[path] = content
			}
		}
	}

	// 5. Run LLM Review (With PR Context & Dependencies)
	fmt.Printf("🧠 Running LLM review for PR #%d (with %d changed files, %d deps)...\n", pr.GetNumber(), len(changedFiles), len(dependencies))
	review, err := llm.RunReview(ctx, nil, diff, changedFiles, dependencies, settings, repoStructure, apiKey, prContext)
	if err != nil {
		fmt.Printf("❌ LLM Review Failed: %v\n", err)
		if checkRunID != 0 {
			internalGH.UpdateCheckRun(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), checkRunID, "failure", "LLM Review Failed")
		}
		return
	}

	// 5. Post Inline Comments
	fmt.Printf("💬 Posting %d comments to GitHub...\n", len(review.Comments))

	emojiMap := map[string]string{
		"security":     "🔐 Security",
		"bug":          "🐛 Bug",
		"performance":  "⚡ Performance",
		"lint":         "🧹 Lint",
		"architecture": "🏗️ Architecture",
	}

	successCount := 0
	for _, comment := range review.Comments {
		// Resolve emoji prefix (case-insensitive)
		prefix, ok := emojiMap[comment.Layer] // Assuming layer from LLM matches keys or we normalize
		// If strict matching fails, try lower case or just use text
		if !ok {
			// Try basic normalization
			switch {
			case contains(comment.Layer, "security"):
				prefix = "🔐 Security"
			case contains(comment.Layer, "bug"):
				prefix = "🐛 Bug"
			case contains(comment.Layer, "performance"):
				prefix = "⚡ Performance"
			case contains(comment.Layer, "lint"):
				prefix = "🧹 Lint"
			case contains(comment.Layer, "architecture"):
				prefix = "🏗️ Architecture"
			default:
				prefix = comment.Layer
			}
		}

		body := fmt.Sprintf("%s\n%s", prefix, comment.Message)

		ghComment := &github.PullRequestComment{
			Body:     github.String(body),
			Path:     github.String(comment.File),
			Line:     github.Int(comment.Line),
			Side:     github.String("RIGHT"),
			CommitID: github.String(commitSHA),
		}
		err := internalGH.PostComment(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), pr.GetNumber(), ghComment)
		if err != nil {
			fmt.Printf("  ⚠️  Failed to post comment on %s:L%d: %v\n", comment.File, comment.Line, err)
			// Fallback: Post as general comment if validation failed (likely line outside diff)
			if strings.Contains(err.Error(), "422") || strings.Contains(err.Error(), "Validation Failed") {
				fmt.Printf("  🔄 Retrying as general comment...\n")
				generalBody := fmt.Sprintf("failed to comment on %s:L%d (likely outside diff):\n\n%s", comment.File, comment.Line, body)
				genErr := internalGH.PostGeneralComment(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), pr.GetNumber(), generalBody)
				if genErr != nil {
					fmt.Printf("  ❌ Failed to post general comment fallback: %v\n", genErr)
				} else {
					successCount++
					fmt.Printf("  ✅ Posted as general comment fallback.\n")
				}
			}
		} else {
			successCount++
		}
	}
	fmt.Printf("✅ Review Complete | Posted %d/%d comments successfully\n", successCount, len(review.Comments))

	if checkRunID != 0 {
		summary := fmt.Sprintf("Review complete. Posted %d comments.", successCount)
		internalGH.UpdateCheckRun(ctx, client, repo.GetOwner().GetLogin(), repo.GetName(), checkRunID, "success", summary)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func updateSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RepoID   string `json:"repo_id"`
			GitHubID string `json:"github_id"`
			APIKey   string `json:"api_key"`
			Layers   struct {
				Security     bool `json:"security"`
				Bug          bool `json:"bug"`
				Lint         bool `json:"lint"`
				Performance  bool `json:"performance"`
				Architecture bool `json:"architecture"`
			} `json:"layers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Update or Insert User
		var userID int
		err := db.QueryRow("SELECT id FROM users WHERE github_id = ?", req.GitHubID).Scan(&userID)
		if err == sql.ErrNoRows {
			res, _ := db.Exec("INSERT INTO users (github_id, llm_api_key) VALUES (?, ?)", req.GitHubID, req.APIKey)
			id, _ := res.LastInsertId()
			userID = int(id)
		} else {
			db.Exec("UPDATE users SET llm_api_key = ? WHERE id = ?", req.APIKey, userID)
		}

		// Update or Insert Repo Settings
		_, err = db.Exec(`
			INSERT INTO repo_settings (repo_id, user_id, security_enabled, bug_enabled, lint_enabled, performance_enabled, architecture_enabled)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_id) DO UPDATE SET
				security_enabled=excluded.security_enabled,
				bug_enabled=excluded.bug_enabled,
				lint_enabled=excluded.lint_enabled,
				performance_enabled=excluded.performance_enabled,
				architecture_enabled=excluded.architecture_enabled
		`, req.RepoID, userID, req.Layers.Security, req.Layers.Bug, req.Layers.Lint, req.Layers.Performance, req.Layers.Architecture)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func getSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID := chi.URLParam(r, "repoID")
		var settings struct {
			Security     bool   `json:"security"`
			Bug          bool   `json:"bug"`
			Lint         bool   `json:"lint"`
			Performance  bool   `json:"performance"`
			Architecture bool   `json:"architecture"`
			APIKey       string `json:"api_key"`
		}
		err := db.QueryRow(`
			SELECT rs.security_enabled, rs.bug_enabled, rs.lint_enabled, rs.performance_enabled, rs.architecture_enabled, u.llm_api_key
			FROM repo_settings rs
			JOIN users u ON rs.user_id = u.id
			WHERE rs.repo_id = ?
		`, repoID).Scan(&settings.Security, &settings.Bug, &settings.Lint, &settings.Performance, &settings.Architecture, &settings.APIKey)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Settings not found", http.StatusNotFound)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		json.NewEncoder(w).Encode(settings)
	}
}
