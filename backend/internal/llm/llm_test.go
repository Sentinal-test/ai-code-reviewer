package llm

import (
	"bytes"
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockRoundTripper struct {
	RoundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.RoundTripFunc(req)
}

func TestRunReview_Success(t *testing.T) {
	// Prepare expected review result
	expectedResult := models.ReviewResult{
		Summary: "Good code",
		Comments: []models.ReviewComment{
			{File: "main.go", Line: 10, Message: "Fix this", Severity: "warning"},
		},
	}
	resultJSON, _ := json.Marshal(expectedResult)

	// Mock Gemini response structure
	geminiRespObj := map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"parts": []map[string]interface{}{
						{"text": string(resultJSON)},
					},
				},
			},
		},
	}
	geminiRespBytes, _ := json.Marshal(geminiRespObj)

	mockClient := &http.Client{
		Transport: &MockRoundTripper{
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				assert.Equal(t, "POST", req.Method)
				assert.Contains(t, req.URL.String(), geminiURL) // basic check
				assert.Contains(t, req.URL.String(), "key=test-api-key")

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(geminiRespBytes)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	result, err := RunReview(context.Background(), mockClient, "diff content", nil, nil, models.RepoSettings{}, "", "test-api-key", models.PRContext{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Good code", result.Summary)
	assert.Len(t, result.Comments, 1)
}

func TestRunReview_APIError(t *testing.T) {
	mockClient := &http.Client{
		Transport: &MockRoundTripper{
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(bytes.NewBufferString("Internal Server Error")),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	_, err := RunReview(context.Background(), mockClient, "diff", nil, nil, models.RepoSettings{}, "", "key", models.PRContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM returned status 500")
}

func TestRunReview_EmptyResponse(t *testing.T) {
	// Mock empty candidates
	geminiRespObj := map[string]interface{}{
		"candidates": []interface{}{},
	}
	geminiRespBytes, _ := json.Marshal(geminiRespObj)

	mockClient := &http.Client{
		Transport: &MockRoundTripper{
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(geminiRespBytes)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	_, err := RunReview(context.Background(), mockClient, "diff", nil, nil, models.RepoSettings{}, "", "key", models.PRContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM returned empty response")
}

func TestRunReview_MalformedJSON(t *testing.T) {
	// Mock response where text is not valid JSON
	geminiRespObj := map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"parts": []map[string]interface{}{
						{"text": "not valid json"},
					},
				},
			},
		},
	}
	geminiRespBytes, _ := json.Marshal(geminiRespObj)

	mockClient := &http.Client{
		Transport: &MockRoundTripper{
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(geminiRespBytes)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	_, err := RunReview(context.Background(), mockClient, "diff", nil, nil, models.RepoSettings{}, "", "key", models.PRContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal JSON content")
}
