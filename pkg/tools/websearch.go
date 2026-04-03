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
	"regexp"
	"strings"
	"time"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

// ============================================================================
// WEB SEARCH TOOL
// ============================================================================

// Compiled regexes for DuckDuckGo parsing to avoid recompilation overhead
var (
	htmlTagStripper   = regexp.MustCompile(`<[^>]+>`)
	ddgLinkPattern    = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	ddgSnippetPattern = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</(?:a|td)>`)
)

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
		cfg.Timeout = 5 * time.Second // Aggressive timeout for serverless
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
				"description": "Number of results to return (default: 1, max: 2)",
				"default":     1,
				"minimum":     1,
				"maximum":     2,
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
	numResults := 1
	if n, ok := args["num_results"].(float64); ok {
		numResults = int(n)
		if numResults < 1 {
			numResults = 1
		}
	}
	// Force maximum to 2 to save LLM tokens and prevent provider rate limits
	if numResults > 2 {
		numResults = 2
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // 512KB limit to prevent massive memory allocations
	if err != nil {
		return nil, err
	}

	return t.parseDuckDuckGoHTML(string(body), numResults), nil
}

// parseDuckDuckGoHTML extracts results from DuckDuckGo HTML.
func (t *WebSearchTool) parseDuckDuckGoHTML(html string, maxResults int) []SearchResult {
	results := []SearchResult{}

	// Extract result links: <a class="result__a" href="...">Title (may contain <b> tags)</a>
	linkMatches := ddgLinkPattern.FindAllStringSubmatch(html, maxResults*2)

	// Extract snippets: <a class="result__snippet" ...>Snippet (may contain <b> tags)</a>
	// Also try <td class="result__snippet"> for alternate format
	snippetMatches := ddgSnippetPattern.FindAllStringSubmatch(html, maxResults*2)

	// Pair up links and snippets
	for i := 0; i < len(linkMatches) && len(results) < maxResults; i++ {
		rawURL := linkMatches[i][1]
		rawTitle := linkMatches[i][2]

		// Decode DuckDuckGo redirect URLs
		actualURL := rawURL
		if strings.Contains(actualURL, "uddg=") {
			if uddgStart := strings.Index(actualURL, "uddg="); uddgStart != -1 {
				extracted := actualURL[uddgStart+5:]
				// Cut at the next & parameter if present
				if ampIdx := strings.Index(extracted, "&"); ampIdx != -1 {
					extracted = extracted[:ampIdx]
				}
				if decoded, err := url.QueryUnescape(extracted); err == nil {
					actualURL = decoded
				}
			}
		}

		// Skip non-http URLs (ads, DDG internal links)
		if !strings.HasPrefix(actualURL, "http://") && !strings.HasPrefix(actualURL, "https://") {
			continue
		}

		// Strip HTML tags from title
		cleanTitle := htmlTagStripper.ReplaceAllString(rawTitle, "")
		cleanTitle = strings.TrimSpace(cleanTitle)

		// Get corresponding snippet (if available)
		snippet := ""
		if i < len(snippetMatches) {
			snippet = htmlTagStripper.ReplaceAllString(snippetMatches[i][1], "")
			snippet = strings.TrimSpace(snippet)
		}

		if cleanTitle == "" {
			continue
		}

		// If no snippet, use a placeholder so we don't lose the result entirely
		if snippet == "" {
			snippet = "(no preview available)"
		}

		results = append(results, SearchResult{
			Title:   cleanTitle,
			URL:     actualURL,
			Snippet: snippet,
		})
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
