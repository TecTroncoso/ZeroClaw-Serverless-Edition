// Package tools implements agent-callable tools for ZeroClaw.
// This module provides the HTTPRequest tool for making generic HTTP requests to external APIs.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zeroclaw/zeroclaw-go/internal/core"
)

// ============================================================================
// HTTP REQUEST TOOL
// ============================================================================

// HTTPRequestTool implements the Tool interface for making generic HTTP requests.
// It supports GET and POST methods with custom headers and body.
type HTTPRequestTool struct {
	httpClient *http.Client
}

// NewHTTPRequestTool creates a new HTTP request tool.
// Configuration is loaded from environment variables:
// - HTTPREQUEST_TIMEOUT: HTTP timeout in seconds (default: 8)
func NewHTTPRequestTool() *HTTPRequestTool {
	timeout := 8 // Default 8 seconds for serverless
	if t := os.Getenv("HTTPREQUEST_TIMEOUT"); t != "" {
		if parsed, err := parseHTTPTimeout(t); err == nil {
			timeout = parsed
		}
	}

	return &HTTPRequestTool{
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow more than 5 redirects
				if len(via) >= 5 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// parseHTTPTimeout parses timeout from string
func parseHTTPTimeout(s string) (int, error) {
	var t int
	_, err := fmt.Sscanf(s, "%d", &t)
	if err != nil || t < 1 || t > 30 {
		return 8, fmt.Errorf("invalid timeout, using default 8")
	}
	return t, nil
}

// Name returns the tool name.
func (t *HTTPRequestTool) Name() string {
	return "http_request"
}

// Description returns the tool description.
func (t *HTTPRequestTool) Description() string {
	return "Makes HTTP requests to external APIs. Supports GET and POST methods with custom headers and body. Useful for interacting with REST APIs, webhooks, or fetching JSON data."
}

// ParametersSchema returns the JSON Schema for tool parameters.
func (t *HTTPRequestTool) ParametersSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to request (must start with http:// or https://)",
			},
			"method": map[string]interface{}{
				"type":        "string",
				"description": "HTTP method: GET or POST (default: GET)",
				"enum":        []string{"GET", "POST"},
				"default":     "GET",
			},
			"body": map[string]interface{}{
				"type":        "string",
				"description": "Request body for POST requests (JSON string or plain text)",
			},
			"headers": map[string]interface{}{
				"type":        "object",
				"description": "Custom headers as key-value pairs",
				"additionalProperties": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"required": []string{"url"},
	}
}

// Execute runs the HTTP request tool.
func (t *HTTPRequestTool) Execute(ctx context.Context, args map[string]interface{}) (*core.ToolResult, error) {
	// Extract URL
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("url is required")
	}

	// Validate URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("url must start with http:// or https://")
	}

	// Extract method (default: GET)
	method := "GET"
	if m, ok := args["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
		if method != "GET" && method != "POST" {
			return nil, fmt.Errorf("method must be GET or POST")
		}
	}

	// Extract body (for POST)
	var body io.Reader
	if b, ok := args["body"].(string); ok && b != "" {
		body = bytes.NewBufferString(b)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	req.Header.Set("User-Agent", "ZeroClaw/1.0 (AI Assistant)")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	// If body is present and content-type not set, try to detect
	if body != nil {
		hasContentType := false
		if headers, ok := args["headers"].(map[string]interface{}); ok {
			for k := range headers {
				if strings.ToLower(k) == "content-type" {
					hasContentType = true
					break
				}
			}
		}
		if !hasContentType {
			// Try to detect if body is JSON
			var js json.RawMessage
			if json.Unmarshal([]byte(args["body"].(string)), &js) == nil {
				req.Header.Set("Content-Type", "application/json")
			} else {
				req.Header.Set("Content-Type", "text/plain")
			}
		}
	}

	// Extract and set custom headers
	if headers, ok := args["headers"].(map[string]interface{}); ok {
		for key, value := range headers {
			if strVal, ok := value.(string); ok {
				req.Header.Set(key, strVal)
			}
		}
	}

	// Execute request
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Build output
	var output strings.Builder
	output.WriteString(fmt.Sprintf("HTTP %d %s\n", resp.StatusCode, resp.Status))

	// Add response headers
	output.WriteString("\n--- Response Headers ---\n")
	for key, values := range resp.Header {
		for _, value := range values {
			output.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}

	// Add response body
	output.WriteString("\n--- Response Body ---\n")
	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "application/json") {
		// Pretty print JSON if possible
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, respBody, "", "  "); err == nil {
			output.WriteString(prettyJSON.String())
		} else {
			output.WriteString(string(respBody))
		}
	} else {
		output.WriteString(string(respBody))
	}

	// If status is error, include in output
	if resp.StatusCode >= 400 {
		output.WriteString(fmt.Sprintf("\n\n[ERROR: HTTP %d]", resp.StatusCode))
	}

	return core.NewSuccessResult(output.String()), nil
}

// Spec returns the full tool specification.
func (t *HTTPRequestTool) Spec() core.ToolSpec {
	return core.ToolSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.ParametersSchema(),
	}
}

// ============================================================================
// CONVENIENCE FUNCTIONS
// ============================================================================

// HTTPRequestWithAuth creates an HTTP request tool with pre-configured authentication.
// This is useful for APIs that require API keys or Bearer tokens.
func HTTPRequestWithAuth(apiKey string) *HTTPRequestTool {
	tool := NewHTTPRequestTool()
	// Note: In a full implementation, we'd store the apiKey and add it to requests
	// For now, users can pass it in the headers argument
	_ = apiKey
	return tool
}

// HTTPRequestJSON creates a quick JSON POST request helper.
func HTTPRequestJSON(url string, data interface{}) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal data: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	tool := NewHTTPRequestTool()
	result, err := tool.Execute(ctx, map[string]interface{}{
		"url":    url,
		"method": "POST",
		"body":   string(jsonData),
		"headers": map[string]string{
			"Content-Type": "application/json",
		},
	})

	if err != nil {
		return "", err
	}

	if !result.Success {
		if result.Error != nil {
			return "", fmt.Errorf(*result.Error)
		}
		return "", fmt.Errorf("request failed")
	}

	return result.Output, nil
}
