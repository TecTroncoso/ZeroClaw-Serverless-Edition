// Package tools implements agent-callable tools for ZeroClaw.
// This module provides the WebSearch tool for real-time information retrieval.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

// ============================================================================
// WEB SEARCH TOOL
// ============================================================================

// WebSearchTool implements the Tool interface for web search.
// Supports multiple search providers: DuckDuckGo (free), Tavily, and Brave Search.
type WebSearchTool struct {
	apiKey     string
	provider   string // "duckduckgo", "tavily", "brave"
	httpClient *http.Client
}

// WebSearchConfig holds configuration for the web search tool.
type WebSearchConfig struct {
	// APIKey for Tavily or Brave Search (DuckDuckGo is free).
	APIKey string
	// Provider: "duckduckgo" (default), "tavily", "brave".
	Provider string
	// Timeout for HTTP requests.
	Timeout time.Duration
}

// NewWebSearchTool creates a new web search tool.
// Configuration is loaded from environment variables:
// - SEARCH_API_KEY: API key for Tavily/Brave (optional for DuckDuckGo)
// - SEARCH_PROVIDER: "duckduckgo", "tavily", or "brave" (default: duckduckgo)
func NewWebSearchTool() *WebSearchTool {
	return NewWebSearchToolWithConfig(nil)
}

// NewWebSearchToolWithConfig creates a web search tool with explicit config.
func NewWebSearchToolWithConfig(cfg *WebSearchConfig) *WebSearchTool {
	if cfg == nil {
		cfg = &WebSearchConfig{}
	}

	// Load from environment if not set
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("SEARCH_API_KEY")
	}
	if cfg.Provider == "" {
		cfg.Provider = os.Getenv("SEARCH_PROVIDER")
		if cfg.Provider == "" {
			cfg.Provider = "duckduckgo"
		}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}

	return &WebSearchTool{
		apiKey:   cfg.APIKey,
		provider: strings.ToLower(cfg.Provider),
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Name returns the tool name.
func (t *WebSearchTool) Name() string {
	return "web_search"
}

// Description returns a human-readable description.
func (t *WebSearchTool) Description() string {
	return "Search the web for current information. Use this when you need up-to-date information about recent events, facts, or topics that may have changed."
}

// ParametersSchema returns the JSON Schema for parameters.
func (t *WebSearchTool) ParametersSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query string. Be specific and concise for best results.",
			},
			"num_results": map[string]interface{}{
				"type":        "integer",
				"description": "Number of results to return (default: 5, max: 10)",
				"default":     5,
				"minimum":     1,
				"maximum":     10,
			},
		},
		"required": []string{"query"},
	}
}

// Execute performs the web search.
func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (*core.ToolResult, error) {
	// Extract query
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return core.NewErrorResult("Missing required parameter: query"), nil
	}

	// Extract num_results (optional)
	numResults := 5
	if n, ok := args["num_results"].(float64); ok {
		numResults = int(n)
		if numResults < 1 {
			numResults = 1
		}
		if numResults > 10 {
			numResults = 10
		}
	}

	// Perform search based on provider
	var results []SearchResult
	var err error

	switch t.provider {
	case "tavily":
		results, err = t.searchTavily(ctx, query, numResults)
	case "brave":
		results, err = t.searchBrave(ctx, query, numResults)
	default:
		results, err = t.searchDuckDuckGo(ctx, query, numResults)
	}

	if err != nil {
		return core.NewErrorResult(fmt.Sprintf("Search failed: %v", err)), nil
	}

	if len(results) == 0 {
		return core.NewSuccessResult("No results found for query: " + query), nil
	}

	// Format results
	output := t.formatResults(results)
	return core.NewSuccessResult(output), nil
}

// Spec returns the tool specification.
func (t *WebSearchTool) Spec() core.ToolSpec {
	return core.ToolSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.ParametersSchema(),
	}
}

// ============================================================================
// SEARCH RESULT TYPE
// ============================================================================

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// ============================================================================
// DUCKDUCKGO (FREE, NO API KEY REQUIRED)
// ============================================================================

// searchDuckDuckGo performs a search using DuckDuckGo HTML scraping.
func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string, numResults int) ([]SearchResult, error) {
	// DuckDuckGo HTML search endpoint
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}

	// Set headers to appear as a regular browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DuckDuckGo returned status %d", resp.StatusCode)
	}

	// Parse HTML response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return t.parseDuckDuckGoHTML(string(body), numResults), nil
}

// parseDuckDuckGoHTML extracts results from DuckDuckGo HTML.
func (t *WebSearchTool) parseDuckDuckGoHTML(html string, maxResults int) []SearchResult {
	results := []SearchResult{}

	// Simple HTML parsing - find result class
	// DuckDuckGo HTML format: <a class="result__a" href="...">Title</a>
	// followed by <a class="result__snippet">Snippet</a>

	lines := strings.Split(html, "\n")
	var currentTitle string
	var currentURL string
	var currentSnippet string

	for _, line := range lines {
		// Extract title and URL from result__a class
		if strings.Contains(line, "class=\"result__a\"") {
			// Extract URL
			if start := strings.Index(line, "href=\""); start != -1 {
				start += 6
				end := strings.Index(line[start:], "\"")
				if end != -1 {
					currentURL = line[start : start+end]
					// DuckDuckGo uses redirect URLs, extract the actual URL
					if strings.Contains(currentURL, "uddg=") {
						if uddgStart := strings.Index(currentURL, "uddg="); uddgStart != -1 {
							actualURL := currentURL[uddgStart+5:]
							if decoded, err := url.QueryUnescape(actualURL); err == nil {
								currentURL = decoded
							}
						}
					}
				}
			}

			// Extract title (between > and <)
			if start := strings.Index(line, ">"); start != -1 {
				rest := line[start+1:]
				if end := strings.Index(rest, "<"); end != -1 {
					currentTitle = strings.TrimSpace(rest[:end])
				}
			}
		}

		// Extract snippet from result__snippet class
		if strings.Contains(line, "class=\"result__snippet\"") {
			if start := strings.Index(line, ">"); start != -1 {
				rest := line[start+1:]
				if end := strings.Index(rest, "<"); end != -1 {
					currentSnippet = strings.TrimSpace(rest[:end])
				}
			}

			// Save result if we have all components
			if currentTitle != "" && currentURL != "" && currentSnippet != "" {
				results = append(results, SearchResult{
					Title:   currentTitle,
					URL:     currentURL,
					Snippet: currentSnippet,
				})

				if len(results) >= maxResults {
					break
				}

				// Reset for next result
				currentTitle = ""
				currentURL = ""
				currentSnippet = ""
			}
		}
	}

	return results
}

// ============================================================================
// TAVILY SEARCH API
// ============================================================================

// searchTavily performs a search using Tavily API.
func (t *WebSearchTool) searchTavily(ctx context.Context, query string, numResults int) ([]SearchResult, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("Tavily requires SEARCH_API_KEY")
	}

	// Tavily API endpoint
	apiURL := "https://api.tavily.com/search"

	// Build request body
	reqBody := map[string]interface{}{
		"api_key":        t.apiKey,
		"query":          query,
		"search_depth":   "basic",
		"max_results":    numResults,
		"include_answer": false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Tavily API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tavilyResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tavilyResp); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(tavilyResp.Results))
	for _, r := range tavilyResp.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}

	return results, nil
}

// ============================================================================
// BRAVE SEARCH API
// ============================================================================

// searchBrave performs a search using Brave Search API.
func (t *WebSearchTool) searchBrave(ctx context.Context, query string, numResults int) ([]SearchResult, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("Brave Search requires SEARCH_API_KEY")
	}

	// Brave Search API endpoint
	apiURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), numResults)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Brave API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var braveResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&braveResp); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(braveResp.Web.Results))
	for _, r := range braveResp.Web.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Description,
		})
	}

	return results, nil
}

// ============================================================================
// RESULT FORMATTING
// ============================================================================

// formatResults formats search results for the agent.
func (t *WebSearchTool) formatResults(results []SearchResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		sb.WriteString(fmt.Sprintf("   %s\n\n", r.Snippet))
	}

	return sb.String()
}
