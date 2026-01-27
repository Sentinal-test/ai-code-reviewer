package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"code-review/backend/internal/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestCallbackHandler_Success(t *testing.T) {
	// 1. Setup Mock GitHub API
	mockGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			// Token exchange response
			w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
			w.Write([]byte("access_token=mock-token&scope=user:email&token_type=bearer"))
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 12345, "login": "testuser"}`))
		case "/user/emails":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"email": "test@appointy.com", "verified": true, "primary": true}]`))
		default:
			t.Errorf("Unexpected call to %s", r.URL.Path)
			http.Error(w, "Not found", http.StatusNotFound)
		}
	}))
	defer mockGitHub.Close()

	// 2. Override Package Variables
	originalUserAPI := githubUserAPI
	originalEmailsAPI := githubEmailsAPI
	defer func() {
		githubUserAPI = originalUserAPI
		githubEmailsAPI = originalEmailsAPI
		oauthConf = nil // Reset
	}()

	githubUserAPI = mockGitHub.URL + "/user"
	githubEmailsAPI = mockGitHub.URL + "/user/emails"

	oauthConf = &oauth2.Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:  mockGitHub.URL + "/login/oauth/authorize",
			TokenURL: mockGitHub.URL + "/login/oauth/access_token",
		},
	}

	// 3. Setup Test DB
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// 4. Create Request
	req := httptest.NewRequest("GET", "/callback?code=valid-code", nil)
	w := httptest.NewRecorder()

	// 5. Run Handler
	handler := CallbackHandler(db)
	handler.ServeHTTP(w, req)

	// 6. Assertions
	resp := w.Result()
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "http://localhost:3000", resp.Header.Get("Location"))

	// Verify Cookie
	cookies := resp.Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "auth_session", cookies[0].Name)
	assert.Equal(t, "12345", cookies[0].Value)

	// Verify DB Insert
	var token string
	err = db.QueryRow("SELECT github_access_token FROM users WHERE github_id = ?", "12345").Scan(&token)
	require.NoError(t, err)
	assert.Equal(t, "mock-token", token)
}

func TestCallbackHandler_InvalidEmail(t *testing.T) {
	mockGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
			w.Write([]byte("access_token=mock-token&scope=user:email&token_type=bearer"))
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 67890, "login": "outsider"}`))
		case "/user/emails":
			w.Header().Set("Content-Type", "application/json")
			// Not an appointy.com email
			w.Write([]byte(`[{"email": "hacker@example.com", "verified": true, "primary": true}]`))
		}
	}))
	defer mockGitHub.Close()

	// Setup vars
	githubUserAPI = mockGitHub.URL + "/user"
	githubEmailsAPI = mockGitHub.URL + "/user/emails"
	oauthConf = &oauth2.Config{
		Endpoint: oauth2.Endpoint{TokenURL: mockGitHub.URL + "/login/oauth/access_token"},
	}

	db, _ := database.InitDB(":memory:")
	defer db.Close()

	req := httptest.NewRequest("GET", "/callback?code=valid-code", nil)
	w := httptest.NewRecorder()

	handler := CallbackHandler(db)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
}

func TestAuthMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := AuthMiddleware(nextHandler)

	t.Run("No Cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("Valid Cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "auth_session", Value: "123"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
}
