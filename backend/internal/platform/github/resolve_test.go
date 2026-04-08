package github

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReviewThreadsPageFindsMatchesAndPagination(t *testing.T) {
	resp := map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"pullRequest": map[string]interface{}{
					"reviewThreads": map[string]interface{}{
						"pageInfo": map[string]interface{}{
							"hasNextPage": true,
							"endCursor":   "cursor-2",
						},
						"nodes": []interface{}{
							map[string]interface{}{
								"id":         "thread-1",
								"isResolved": false,
								"comments": map[string]interface{}{
									"nodes": []interface{}{
										map[string]interface{}{"id": "comment-a"},
										map[string]interface{}{"id": "comment-b"},
									},
								},
							},
							map[string]interface{}{
								"id":         "thread-2",
								"isResolved": true,
								"comments": map[string]interface{}{
									"nodes": []interface{}{
										map[string]interface{}{"id": "comment-c"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	page, err := parseReviewThreadsPage(resp, map[string]struct{}{
		"comment-b": {},
		"comment-c": {},
		"missing":   {},
	})
	require.NoError(t, err)

	assert.True(t, page.HasNextPage)
	assert.Equal(t, "cursor-2", page.EndCursor)
	assert.Equal(t, reviewThreadLookup{ThreadID: "thread-1", IsResolved: false}, page.Matches["comment-b"])
	assert.Equal(t, reviewThreadLookup{ThreadID: "thread-2", IsResolved: true}, page.Matches["comment-c"])
	_, ok := page.Matches["missing"]
	assert.False(t, ok)
}

func TestParseReviewThreadsPageRejectsMissingRepository(t *testing.T) {
	_, err := parseReviewThreadsPage(map[string]interface{}{
		"data": map[string]interface{}{},
	}, map[string]struct{}{"comment-a": {}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "repository")
}

func TestIsGraphQLForbidden(t *testing.T) {
	assert.True(t, isGraphQLForbidden(fmt.Errorf("GraphQL errors: [map[message:Resource not accessible by integration type:FORBIDDEN]]")))
	assert.True(t, isGraphQLForbidden(fmt.Errorf("GraphQL errors: [map[message:Resource not accessible by personal access token type:FORBIDDEN]]")))
	assert.False(t, isGraphQLForbidden(fmt.Errorf("GraphQL errors: [map[message:some other failure type:UNPROCESSABLE]]")))
}
