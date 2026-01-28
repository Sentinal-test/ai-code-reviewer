package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

var (
	oauthConf       *oauth2.Config
	githubUserAPI   = "https://api.github.com/user"
	githubEmailsAPI = "https://api.github.com/user/emails"
)

func InitAuth() {
	oauthConf = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		Scopes:       []string{"user:email"},
		Endpoint:     github.Endpoint,
		RedirectURL:  "http://localhost:8080/auth/callback",
	}
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if oauthConf.ClientID == "" || oauthConf.ClientSecret == "" {
		http.Error(w, "OAuth not configured (GITHUB_CLIENT_ID/SECRET missing)", http.StatusInternalServerError)
		return
	}
	url := oauthConf.AuthCodeURL("state", oauth2.AccessTypeOnline)
	fmt.Printf("🔐 Initiating Login Redirect: %s\n", url)
	http.Redirect(w, r, url, http.StatusFound)
}

func CallbackHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			fmt.Println("❌ No code provided in callback")
			http.Error(w, "No code provided", http.StatusBadRequest)
			return
		}

		ctx := context.Background()
		token, err := oauthConf.Exchange(ctx, code)
		if err != nil {
			fmt.Printf("❌ OAuth exchange failed: %v\n", err)
			http.Error(w, "OAuth exchange failed", http.StatusInternalServerError)
			return
		}

		client := oauthConf.Client(ctx, token)

		// 1. Get User Info
		userResp, err := client.Get(githubUserAPI)
		if err != nil {
			fmt.Printf("❌ Failed to get user info: %v\n", err)
			http.Error(w, "Failed to get user info: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer userResp.Body.Close()

		var ghUser struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		}
		if err := json.NewDecoder(userResp.Body).Decode(&ghUser); err != nil {
			fmt.Printf("❌ Failed to decode user info: %v\n", err)
			http.Error(w, "Failed to decode user info", http.StatusInternalServerError)
			return
		}

		// 2. Validate Email Domain (@appointy.com)
		emailsResp, err := client.Get(githubEmailsAPI)
		if err != nil {
			fmt.Printf("❌ Failed to get user emails: %v\n", err)
			http.Error(w, "Failed to get user emails: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer emailsResp.Body.Close()

		if emailsResp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(emailsResp.Body)
			fmt.Printf("❌ GitHub Email API Error: Status %s | Body: %s\n", emailsResp.Status, string(bodyBytes))
			http.Error(w, "GitHub Email API Error: "+emailsResp.Status, http.StatusInternalServerError)
			return
		}

		// Read body for debug if needed
		// Since we have a stream, we should decode directly, but to debug failure we can readAll or use tee reader.
		// Let's decode logic carefully.
		var emails []struct {
			Email    string `json:"email"`
			Verified bool   `json:"verified"`
			Primary  bool   `json:"primary"`
		}
		// If decoding fails, it means we didn't get an array.
		if err := json.NewDecoder(emailsResp.Body).Decode(&emails); err != nil {
			fmt.Printf("❌ Failed to decode emails (expected array): %v\n", err)
			// At this point we can't read body again easily unless we had peeked.
			// But the error likely means we got an object (error response) despite 200 OK? Or maybe not 200 OK.
			// Added specific status check above which handles most cases.
			http.Error(w, "Failed to decode emails", http.StatusInternalServerError)
			return
		}

		validDomain := false
		for _, e := range emails {
			if e.Verified && strings.HasSuffix(e.Email, "@appointy.com") {
				validDomain = true
				break
			}
		}

		if !validDomain {
			http.Error(w, "Access Denied: You must have a verified @appointy.com email address linked to your GitHub account.", http.StatusForbidden)
			return
		}

		// 3. Create/Update User in DB
		// Upsert user to save the new Access Token (useful for API calls later)
		_, err = db.Exec(`
			INSERT INTO users (github_id, github_access_token) VALUES (?, ?)
			ON CONFLICT(github_id) DO UPDATE SET github_access_token = excluded.github_access_token
		`, fmt.Sprintf("%d", ghUser.ID), token.AccessToken)
		if err != nil {
			fmt.Printf("Warning: Failed to upsert user in DB: %v\n", err)
		}

		// 4. Set Session Cookie
		// Simple session: GitHubID in cookie (In prod, use a signed JWT or session store)
		// For MVP: Plain cookie (secure, httpOnly)
		http.SetCookie(w, &http.Cookie{
			Name:     "auth_session",
			Value:    fmt.Sprintf("%d", ghUser.ID),
			Path:     "/",
			HttpOnly: true,
			Secure:   false,                // Set to true in prod (HTTPS)
			SameSite: http.SameSiteLaxMode, // Lax is usually fine for localhost top-level nav, but let's be explicit
			MaxAge:   3600 * 24,            // 1 day
		})

		// 5. Redirect to Dashboard
		http.Redirect(w, r, "http://localhost:3000", http.StatusFound)
	}
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_session")
		if err != nil || cookie.Value == "" {
			// fmt.Println("🔒 Auth Middleware: Unauthorized (No Cookie)") // Comment out to reduce noise if needed, but useful for debug
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Basic validation pass
		next.ServeHTTP(w, r)
	})
}

func GetMeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_session")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		githubID := cookie.Value

		// Check if user exists and get details
		var user struct {
			GitHubID string `json:"github_id"`
			ApiKey   string `json:"api_key"`
		}
		// We need to fetch from DB to see if they have settings
		err = db.QueryRow("SELECT github_id, COALESCE(llm_api_key, '') FROM users WHERE github_id = ?", githubID).Scan(&user.GitHubID, &user.ApiKey)
		if err == sql.ErrNoRows {
			// Valid session but not in DB yet? Should have been inserted in login.
			// Just return basic info
			user.GitHubID = githubID
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user)
	}
}
