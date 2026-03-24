package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"code-review/backend/internal/models"
)

const githubGraphQLEndpoint = "https://api.github.com/graphql"

type reviewThreadLookup struct {
	ThreadID   string
	IsResolved bool
}

type reviewThreadsPage struct {
	Matches     map[string]reviewThreadLookup
	HasNextPage bool
	EndCursor   string
}

// ResolveThreads resolves GitHub review threads for findings the Consolidator marked as resolved.
// It uses the GraphQL API because the REST API does not support thread resolution.
func ResolveThreads(token, owner, repo string, prNumber int, resolutions []models.Resolution, commentNodeMap map[int64]string) {
	resolvedResolutions := 0
	for _, res := range resolutions {
		if strings.EqualFold(res.Status, "resolved") {
			resolvedResolutions++
		}
	}
	fmt.Printf("  🔍 [Resolve] Starting: %d total resolutions (%d resolved), %d comment NodeIDs available\n",
		len(resolutions), resolvedResolutions, len(commentNodeMap))

	if len(resolutions) == 0 || len(commentNodeMap) == 0 {
		if len(resolutions) > 0 && len(commentNodeMap) == 0 {
			fmt.Println("  ⚠️ [Resolve] Resolutions exist but commentNodeMap is empty — no previous inline comments found")
		}
		return
	}

	targetNodeIDs := make(map[string]struct{}, resolvedResolutions)
	for _, res := range resolutions {
		if !strings.EqualFold(res.Status, "resolved") {
			continue
		}
		nodeID, ok := commentNodeMap[int64(res.CommentID)]
		if ok && nodeID != "" {
			targetNodeIDs[nodeID] = struct{}{}
		}
	}
	if len(targetNodeIDs) == 0 {
		fmt.Println("  ℹ️ [Resolve] No GraphQL node IDs matched resolved comment IDs — skipping")
		return
	}

	threadLookup, err := findThreadNodeIDs(token, owner, repo, prNumber, targetNodeIDs)
	if err != nil {
		fmt.Printf("  ⚠️ [Resolve] Failed to load review threads for %s/%s#%d: %v\n", owner, repo, prNumber, err)
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

		match, ok := threadLookup[nodeID]
		if !ok {
			fmt.Printf("  ℹ️ [Resolve] Comment %d has no matching review thread in %s/%s#%d — skipping\n", commentID, owner, repo, prNumber)
			continue
		}
		if match.IsResolved {
			fmt.Printf("  ℹ️ [Resolve] Thread for comment %d is already resolved\n", commentID)
			continue
		}
		threadID := match.ThreadID

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

// findThreadNodeIDs queries the PR review threads and finds the thread for each
// targeted review comment node ID.
func findThreadNodeIDs(token, owner, repo string, prNumber int, targetNodeIDs map[string]struct{}) (map[string]reviewThreadLookup, error) {
	if len(targetNodeIDs) == 0 {
		return map[string]reviewThreadLookup{}, nil
	}

	matches := make(map[string]reviewThreadLookup, len(targetNodeIDs))
	var after string

	for {
		query := `query($owner: String!, $repo: String!, $number: Int!, $after: String) {
			repository(owner: $owner, name: $repo) {
				pullRequest(number: $number) {
					reviewThreads(first: 100, after: $after) {
						pageInfo { hasNextPage endCursor }
						nodes {
							id
							isResolved
							comments(first: 100) {
								nodes { id }
							}
						}
					}
				}
			}
		}`

		variables := map[string]interface{}{
			"owner":  owner,
			"repo":   repo,
			"number": prNumber,
			"after":  nil,
		}
		if after != "" {
			variables["after"] = after
		}

		resp, err := graphqlRequest(token, query, variables)
		if err != nil {
			return nil, err
		}

		page, err := parseReviewThreadsPage(resp, targetNodeIDs)
		if err != nil {
			return nil, err
		}
		for commentNodeID, match := range page.Matches {
			matches[commentNodeID] = match
		}
		if len(matches) == len(targetNodeIDs) || !page.HasNextPage {
			return matches, nil
		}
		after = page.EndCursor
	}
}

func parseReviewThreadsPage(resp map[string]interface{}, targetNodeIDs map[string]struct{}) (reviewThreadsPage, error) {
	page := reviewThreadsPage{
		Matches: make(map[string]reviewThreadLookup),
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return page, fmt.Errorf("unexpected response format")
	}
	repository, ok := data["repository"].(map[string]interface{})
	if !ok || repository == nil {
		return page, fmt.Errorf("repository not found in response")
	}
	pullRequest, ok := repository["pullRequest"].(map[string]interface{})
	if !ok || pullRequest == nil {
		return page, fmt.Errorf("pull request not found in response")
	}
	reviewThreads, ok := pullRequest["reviewThreads"].(map[string]interface{})
	if !ok || reviewThreads == nil {
		return page, fmt.Errorf("reviewThreads not found in response")
	}

	pageInfo, ok := reviewThreads["pageInfo"].(map[string]interface{})
	if ok && pageInfo != nil {
		if hasNextPage, ok := pageInfo["hasNextPage"].(bool); ok {
			page.HasNextPage = hasNextPage
		}
		if endCursor, ok := pageInfo["endCursor"].(string); ok {
			page.EndCursor = endCursor
		}
	}

	nodes, ok := reviewThreads["nodes"].([]interface{})
	if !ok {
		return page, fmt.Errorf("reviewThreads.nodes missing or invalid")
	}

	for _, rawThread := range nodes {
		thread, ok := rawThread.(map[string]interface{})
		if !ok || thread == nil {
			continue
		}
		threadID, _ := thread["id"].(string)
		isResolved, _ := thread["isResolved"].(bool)

		comments, ok := thread["comments"].(map[string]interface{})
		if !ok || comments == nil {
			continue
		}
		commentNodes, ok := comments["nodes"].([]interface{})
		if !ok {
			continue
		}

		for _, rawComment := range commentNodes {
			comment, ok := rawComment.(map[string]interface{})
			if !ok || comment == nil {
				continue
			}
			commentNodeID, _ := comment["id"].(string)
			if _, wanted := targetNodeIDs[commentNodeID]; !wanted {
				continue
			}
			page.Matches[commentNodeID] = reviewThreadLookup{
				ThreadID:   threadID,
				IsResolved: isResolved,
			}
		}
	}

	return page, nil
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

// graphqlRequest sends a GraphQL request to the GitHub API with a 30-second timeout.
func graphqlRequest(token, query string, variables map[string]interface{}) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", githubGraphQLEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
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
