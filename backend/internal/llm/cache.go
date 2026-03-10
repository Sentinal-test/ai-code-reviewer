package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CachedContentResponse represents the response from the Gemini API when creating a cache
type CachedContentResponse struct {
	Name       string    `json:"name"`
	Model      string    `json:"model"`
	CreateTime time.Time `json:"createTime"`
	UpdateTime time.Time `json:"updateTime"`
	ExpireTime time.Time `json:"expireTime"`
}

// CreateCachedContent uploads static context to Gemini and returns the cache name.
// This is used to optimize multi-turn agent loops.
func CreateCachedContent(
	ctx context.Context,
	client *http.Client,
	apiKey string,
	systemInstruction string,
	staticContext string,
	tools []map[string]interface{},
) (string, error) {
	// API endpoint for caching
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/cachedContents?key=%s", apiKey)

	// TTL for the cache (15 minutes is plenty for our max 8 iteration loop)
	expireTime := time.Now().Add(15 * time.Minute).Format(time.RFC3339Nano)

	reqBody := map[string]interface{}{
		// Map the specific model we're caching against
		"model": fmt.Sprintf("models/%s", geminiModel),

		"expireTime": expireTime,

		// The system instructions
		"systemInstruction": map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": systemInstruction},
			},
		},

		// The large static context (code graph, structure)
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": staticContext},
				},
			},
		},
	}

	// Add tools if supported
	if len(tools) > 0 {
		reqBody["tools"] = tools
		reqBody["tool_config"] = map[string]interface{}{
			"function_calling_config": map[string]interface{}{
				"mode": "AUTO",
			},
		}
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cache request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create cache request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cache request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("cache returned status %d: %s", resp.StatusCode, string(body))
	}

	var cacheResp CachedContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&cacheResp); err != nil {
		return "", fmt.Errorf("failed to decode cache response: %w", err)
	}

	return cacheResp.Name, nil
}

// DeleteCachedContent removes an existing cache from Gemini to free resources.
func DeleteCachedContent(ctx context.Context, client *http.Client, apiKey string, cacheName string) error {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s?key=%s", cacheName, apiKey)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete cache request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete cache returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
