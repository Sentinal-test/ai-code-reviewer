package action

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"code-review/backend/internal/models"
)

const githubGraphQLEndpoint = "https://api.github.com/graphql"

// ResolveThreads resolves GitHub review threads for findings the Consolidator marked as resolved.
// It uses the GraphQL API because the REST API does not support thread resolution.
func ResolveThreads(token string, resolutions []models.Resolution, commentNodeMap map[int64]string) {
	if len(resolutions) == 0 || len(commentNodeMap) == 0 {
		return
	}

	resolvedCount := 0
	for _, res := range resolutions {
		if !strings.EqualFold(res.Status, "resolved") {
			continue
		}

		commentID := int64(res.CommentID)
		nodeID, ok := commentNodeMap[commentID]
		if !ok || nodeID == "" {
			fmt.Printf("  ℹ️ [Resolve] No NodeID for comment %d — skipping\n", commentID)
			continue
		}

		threadID, err := findThreadNodeID(token, nodeID)
		if err != nil {
			fmt.Printf("  ⚠️ [Resolve] Failed to find thread for comment %d: %v\n", commentID, err)
			continue
		}
		if threadID == "" {
			fmt.Printf("  ℹ️ [Resolve] Comment %d has no review thread (general comment?) — skipping\n", commentID)
			continue
		}

		if err := resolveThread(token, threadID); err != nil {
			fmt.Printf("  ⚠️ [Resolve] Failed to resolve thread %s: %v\n", threadID, err)
		} else {
			fmt.Printf("  ✅ [Resolve] Resolved thread for comment %d\n", commentID)
			resolvedCount++
		}
	}

	if resolvedCount > 0 {
		fmt.Printf("  🎯 [Resolve] Resolved %d review threads\n", resolvedCount)
	}
}

// findThreadNodeID queries the GraphQL API to find the PullRequestReviewThread
// that contains the given comment (identified by its GraphQL node ID).
func findThreadNodeID(token, commentNodeID string) (string, error) {
	query := `query($id: ID!) {
		node(id: $id) {
			... on PullRequestReviewComment {
				pullRequestReview {
					pullRequest {
						reviewThreads(last: 100) {
							nodes {
								id
								comments(first: 1) {
									nodes { id }
								}
							}
						}
					}
				}
			}
		}
	}`

	// Simpler approach: just get the thread directly from the comment
	query = `query($id: ID!) {
		node(id: $id) {
			... on PullRequestReviewComment {
				pullRequestReviewThread { id isResolved }
			}
		}
	}`

	variables := map[string]interface{}{"id": commentNodeID}
	resp, err := graphqlRequest(token, query, variables)
	if err != nil {
		return "", err
	}

	// Parse: { "data": { "node": { "pullRequestReviewThread": { "id": "...", "isResolved": false } } } }
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response format")
	}
	node, ok := data["node"].(map[string]interface{})
	if !ok {
		return "", nil // Comment not found or not a review comment
	}
	thread, ok := node["pullRequestReviewThread"].(map[string]interface{})
	if !ok {
		return "", nil // No thread (general comment)
	}

	// Skip if already resolved
	if isResolved, ok := thread["isResolved"].(bool); ok && isResolved {
		return "", nil // Already resolved, nothing to do
	}

	threadID, _ := thread["id"].(string)
	return threadID, nil
}

// resolveThread calls the resolveReviewThread GraphQL mutation.
func resolveThread(token, threadID string) error {
	query := `mutation($threadId: ID!) {
		resolveReviewThread(input: { threadId: $threadId }) {
			thread { isResolved }
		}
	}`

	variables := map[string]interface{}{"threadId": threadID}
	_, err := graphqlRequest(token, query, variables)
	return err
}

// graphqlRequest sends a GraphQL request to the GitHub API.
func graphqlRequest(token, query string, variables map[string]interface{}) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest("POST", githubGraphQLEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GraphQL request failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	// Check for GraphQL errors
	if errors, ok := result["errors"]; ok {
		return nil, fmt.Errorf("GraphQL errors: %v", errors)
	}

	return result, nil
}

// BuildCommentNodeMap creates a lookup map from REST comment ID to GraphQL NodeID.
func BuildCommentNodeMap(findings []models.PreviousFinding) map[int64]string {
	m := make(map[int64]string, len(findings))
	for _, f := range findings {
		if int64(f.CommentID) > 0 && f.NodeID != "" {
			m[int64(f.CommentID)] = f.NodeID
		}
	}
	return m
}
