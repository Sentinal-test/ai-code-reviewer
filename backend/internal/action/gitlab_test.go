package action

import (
	"bytes"
	"code-review/backend/internal/models"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string, headers map[string]string) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestGitLabClientFetchPreviousFindings(t *testing.T) {
	marker := "<!-- ai-reviewer:v1 file=Zm9vLmdv line=12 severity=warning layer=bug hash=abc123 --> body"

	client := NewGitLabClient("token", "https://gitlab.example/api/v4", "123", "")
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v4/projects/123/merge_requests/7/discussions":
			return jsonResponse(http.StatusOK, `[{"id":"disc-1","notes":[{"id":101,"body":"`+marker+`","position":{"new_path":"foo.go","new_line":12}}]}]`, map[string]string{}), nil
		case "/api/v4/projects/123/merge_requests/7/notes":
			return jsonResponse(http.StatusOK, `[{"id":202,"body":"`+marker+`"}]`, map[string]string{}), nil
		default:
			return jsonResponse(http.StatusNotFound, `{"message":"not found"}`, map[string]string{}), nil
		}
	})}

	findings, err := client.FetchPreviousFindings(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, findings, 2)

	assert.Equal(t, "foo.go", findings[0].File)
	assert.Equal(t, 12, findings[0].Line)
	assert.Equal(t, "disc-1", findings[0].ThreadID)
	assert.EqualValues(t, 101, findings[0].CommentID)
	assert.EqualValues(t, 202, findings[1].CommentID)
}

func TestGitLabClientResolveDiscussionsDeduplicatesThreads(t *testing.T) {
	var requests []string

	client := NewGitLabClient("token", "https://gitlab.example/api/v4", "123", "")
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Method+" "+req.URL.Path)
		return jsonResponse(http.StatusOK, `{"id":"disc-1","resolved":true}`, map[string]string{}), nil
	})}

	client.ResolveDiscussions(context.Background(), 7,
		[]models.Resolution{
			{CommentID: 101, Status: "resolved"},
			{CommentID: 102, Status: "resolved"},
		},
		[]models.PreviousFinding{
			{CommentID: 101, ThreadID: "disc-1"},
			{CommentID: 102, ThreadID: "disc-1"},
		},
	)

	require.Len(t, requests, 1)
	assert.Equal(t, "PUT /api/v4/projects/123/merge_requests/7/discussions/disc-1", requests[0])
}

func TestGitLabClientPostReviewFallsBackToGeneralNotesWithoutVersion(t *testing.T) {
	var requests []string

	client := NewGitLabClient("token", "https://gitlab.example/api/v4", "123", "")
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Method+" "+req.URL.Path)
		if strings.HasSuffix(req.URL.Path, "/versions") {
			return jsonResponse(http.StatusBadRequest, `{"message":"no versions"}`, map[string]string{}), nil
		}
		return jsonResponse(http.StatusCreated, `{}`, map[string]string{}), nil
	})}

	err := client.PostReview(context.Background(), 7, &models.ReviewResult{
		Summary: "summary",
		Comments: []models.ReviewComment{{
			File: "foo.go", Line: 12, Severity: "warning", Layer: "bug", Message: "message",
		}},
	}, "deadbeef", "diff --git a/foo.go b/foo.go\n@@ -1 +12 @@\n+line\n", nil)
	require.NoError(t, err)

	assert.Contains(t, requests, "GET /api/v4/projects/123/merge_requests/7/versions")
	assert.Contains(t, requests, "POST /api/v4/projects/123/merge_requests/7/notes")
}

func TestGitLabClientGetMergeRequest(t *testing.T) {
	client := NewGitLabClient("token", "https://gitlab.example/api/v4", "42", "")
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v4/projects/42/merge_requests/5":
			return jsonResponse(http.StatusOK, `{"title":"Fix auth flow","description":"Refactors the auth module"}`, map[string]string{}), nil
		case "/api/v4/projects/42/merge_requests/5/commits":
			return jsonResponse(http.StatusOK, `[{"message":"fix: handle nil token","title":"fix: handle nil token"},{"message":"refactor: simplify auth check","title":"refactor: simplify auth check"}]`, map[string]string{}), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, map[string]string{}), nil
		}
	})}

	prCtx, err := client.GetMergeRequest(context.Background(), 5)
	require.NoError(t, err)

	assert.Equal(t, "Fix auth flow", prCtx.Title)
	assert.Equal(t, "Refactors the auth module", prCtx.Body)
	require.Len(t, prCtx.CommitMessages, 2)
	assert.Equal(t, "fix: handle nil token", prCtx.CommitMessages[0])
	assert.Equal(t, "refactor: simplify auth check", prCtx.CommitMessages[1])
}

func TestGitLabClientPostReviewWithInlineDiscussions(t *testing.T) {
	var requests []string
	var postedBodies []string

	client := NewGitLabClient("token", "https://gitlab.example/api/v4", "99", "")
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reqPath := req.Method + " " + req.URL.Path
		requests = append(requests, reqPath)

		if strings.HasSuffix(req.URL.Path, "/versions") {
			return jsonResponse(http.StatusOK, `[{"id":1,"head_commit_sha":"abc123","base_commit_sha":"def456","start_commit_sha":"ghi789"}]`, map[string]string{}), nil
		}
		if strings.Contains(req.URL.Path, "/discussions") && req.Method == "POST" {
			body, _ := io.ReadAll(req.Body)
			postedBodies = append(postedBodies, string(body))
			return jsonResponse(http.StatusCreated, `{"id":"disc-new"}`, map[string]string{}), nil
		}
		if strings.Contains(req.URL.Path, "/notes") && req.Method == "POST" {
			return jsonResponse(http.StatusCreated, `{}`, map[string]string{}), nil
		}
		return jsonResponse(http.StatusNotFound, `{}`, map[string]string{}), nil
	})}

	diff := "diff --git a/main.go b/main.go\n@@ -10,3 +10,5 @@\n context\n+newline1\n+newline2\n"

	err := client.PostReview(context.Background(), 3, &models.ReviewResult{
		Summary: "All good",
		Comments: []models.ReviewComment{
			{File: "main.go", Line: 11, Severity: "warning", Layer: "bug", Message: "potential nil"},
		},
	}, "abc123", diff, nil)
	require.NoError(t, err)

	// Should have fetched version and posted inline discussion
	assert.Contains(t, requests, "GET /api/v4/projects/99/merge_requests/3/versions")
	assert.Contains(t, requests, "POST /api/v4/projects/99/merge_requests/3/discussions")

	// Verify position fields in the inline discussion post
	require.NotEmpty(t, postedBodies)
	body := postedBodies[0]
	assert.Contains(t, body, "position%5Bbase_sha%5D=def456")
	assert.Contains(t, body, "position%5Bhead_sha%5D=abc123")
	assert.Contains(t, body, "position%5Bstart_sha%5D=ghi789")
	assert.Contains(t, body, "position%5Bnew_path%5D=main.go")
	assert.Contains(t, body, "position%5Bnew_line%5D=11")
	assert.Contains(t, body, "position%5Bposition_type%5D=text")

	// Should also post summary note
	assert.Contains(t, requests, "POST /api/v4/projects/99/merge_requests/3/notes")
}

func TestGitLabClientPostReviewMixedInlineAndFallback(t *testing.T) {
	var discussionPosts, notePosts int

	client := NewGitLabClient("token", "https://gitlab.example/api/v4", "55", "")
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/versions") {
			return jsonResponse(http.StatusOK, `[{"id":1,"head_commit_sha":"aaa","base_commit_sha":"bbb","start_commit_sha":"ccc"}]`, map[string]string{}), nil
		}
		if strings.Contains(req.URL.Path, "/discussions") && req.Method == "POST" {
			discussionPosts++
			// First inline succeeds, second fails to simulate diff mismatch
			if discussionPosts == 2 {
				return jsonResponse(http.StatusBadRequest, `{"message":"400 Bad Request - Note {:type=>\"DiffNote\"} is invalid"}`, map[string]string{}), nil
			}
			return jsonResponse(http.StatusCreated, `{"id":"disc-1"}`, map[string]string{}), nil
		}
		if strings.Contains(req.URL.Path, "/notes") && req.Method == "POST" {
			notePosts++
			return jsonResponse(http.StatusCreated, `{}`, map[string]string{}), nil
		}
		return jsonResponse(http.StatusNotFound, `{}`, map[string]string{}), nil
	})}

	diff := "diff --git a/a.go b/a.go\n@@ -1,3 +1,5 @@\n ctx\n+line1\n+line2\ndiff --git a/b.go b/b.go\n@@ -10,2 +10,3 @@\n old\n+new\n"

	err := client.PostReview(context.Background(), 8, &models.ReviewResult{
		Summary: "Mixed results",
		Comments: []models.ReviewComment{
			{File: "a.go", Line: 2, Severity: "critical", Layer: "security", Message: "SQL injection"},
			{File: "b.go", Line: 11, Severity: "warning", Layer: "bug", Message: "nil deref"},
			{File: "c.go", Line: 0, Severity: "info", Layer: "lint", Message: "unused import"},
		},
	}, "aaa", diff, nil)
	require.NoError(t, err)

	// 1 inline success + 1 inline fail + 1 zero-line → 2 discussion attempts + multiple note posts
	assert.Equal(t, 2, discussionPosts)
	// Notes: 1 fallback for failed inline + 1 fallback for Line=0 + 1 summary = 3
	assert.Equal(t, 3, notePosts)
}

func TestGitLabClientFetchPreviousFindingsPagination(t *testing.T) {
	marker1 := `<!-- ai-reviewer:v1 file=YS5nbw line=10 severity=warning layer=bug hash=hash111 --> body1`
	marker2 := `<!-- ai-reviewer:v1 file=Yi5nbw line=20 severity=critical layer=security hash=hash222 --> body2`

	callCount := 0
	client := NewGitLabClient("token", "https://gitlab.example/api/v4", "77", "")
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/discussions") {
			callCount++
			page := req.URL.Query().Get("page")
			if page == "" || page == "1" {
				return jsonResponse(http.StatusOK,
					`[{"id":"disc-1","notes":[{"id":301,"body":"`+marker1+`","position":{"new_path":"a.go","new_line":10}}]}]`,
					map[string]string{"X-Next-Page": "2"},
				), nil
			}
			// Page 2 — last page
			return jsonResponse(http.StatusOK,
				`[{"id":"disc-2","notes":[{"id":302,"body":"`+marker2+`","position":{"new_path":"b.go","new_line":20}}]}]`,
				map[string]string{},
			), nil
		}
		if strings.Contains(req.URL.Path, "/notes") {
			return jsonResponse(http.StatusOK, `[]`, map[string]string{}), nil
		}
		return jsonResponse(http.StatusNotFound, `{}`, map[string]string{}), nil
	})}

	findings, err := client.FetchPreviousFindings(context.Background(), 9)
	require.NoError(t, err)
	require.Len(t, findings, 2)

	assert.Equal(t, "a.go", findings[0].File)
	assert.Equal(t, 10, findings[0].Line)
	assert.Equal(t, "disc-1", findings[0].ThreadID)
	assert.EqualValues(t, 301, findings[0].CommentID)

	assert.Equal(t, "b.go", findings[1].File)
	assert.Equal(t, 20, findings[1].Line)
	assert.Equal(t, "disc-2", findings[1].ThreadID)
	assert.EqualValues(t, 302, findings[1].CommentID)
}

