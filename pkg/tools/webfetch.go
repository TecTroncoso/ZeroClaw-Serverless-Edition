// Package tools implements agent-callable tools for ZeroClaw.
// This module provides the WebFetch tool for retrieving and cleaning web page content.
package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

// ============================================================================
// WEB FETCH TOOL
// ============================================================================

var (
	scriptPattern     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	stylePattern      = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	navPattern        = regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	footerPattern     = regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	headerPattern     = regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	asidePattern      = regexp.MustCompile(`(?is)<aside[^>]*>.*?</aside>`)
	commentPattern    = regexp.MustCompile(`(?is)<!--.*?-->`)
	blockElements     = regexp.MustCompile(`(?i)</?(div|p|br|h[1-6]|li|tr|td|th|article|section|main|address)`)
	tagPattern        = regexp.MustCompile(`<[^>]+>`)
	numPattern        = regexp.MustCompile(`&#(\d+);`)
	hexPattern        = regexp.MustCompile(`&#x([0-9a-fA-F]+);`)
	whitespacePattern = regexp.MustCompile(`[ \t]+`)
)

// WebFetchTool implements the Tool interface for fetching web page content.
// It retrieves URLs, cleans HTML, and returns text content suitable for LLM consumption.
type WebFetchTool struct {
	httpClient *http.Client
	maxChars   int
}

// NewWebFetchTool creates a new web fetch tool.
// Configuration is loaded from environment variables:
// - WEBFETCH_TIMEOUT: HTTP timeout in seconds (default: 8)
// - WEBFETCH_MAX_CHARS: Maximum characters to return (default: 4000)
func NewWebFetchTool() *WebFetchTool {
	timeout := 5 // Default 5 seconds for aggressive serverless performance
	if t := os.Getenv("WEBFETCH_TIMEOUT"); t != "" {
		if parsed, err := parseTimeout(t); err == nil {
			timeout = parsed
		}
	}

	maxChars := 4000 // Default max chars
	if m := os.Getenv("WEBFETCH_MAX_CHARS"); m != "" {
		if parsed, err := parseMaxChars(m); err == nil {
			maxChars = parsed
		}
	}

	return &WebFetchTool{
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
		maxChars: maxChars,
	}
}

// parseTimeout parses timeout from string
func parseTimeout(s string) (int, error) {
	var t int
	_, err := fmt.Sscanf(s, "%d", &t)
	if err != nil || t < 1 || t > 30 {
		return 8, fmt.Errorf("invalid timeout, using default 8")
	}
	return t, nil
}

// parseMaxChars parses max chars from string
func parseMaxChars(s string) (int, error) {
	var m int
	_, err := fmt.Sscanf(s, "%d", &m)
	if err != nil || m < 100 || m > 10000 {
		return 4000, fmt.Errorf("invalid max chars, using default 4000")
	}
	return m, nil
}

// Name returns the tool name.
func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

// Description returns the tool description.
func (t *WebFetchTool) Description() string {
	return "Fetches web pages and extracts text content. Supports multiple URLs with automatic fallback: if the first URL fails or times out, the next one is tried. Automatically cleans HTML to extract readable text."
}

// ParametersSchema returns the JSON Schema for tool parameters.
func (t *WebFetchTool) ParametersSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"urls": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "List of URLs ordered by priority. If the first fails or times out, the system will automatically try the next one.",
			},
			"url": map[string]interface{}{
				"type":        "string",
				"description": "Single URL to fetch (alternative to urls array).",
			},
		},
	}
}

// Execute runs the web fetch tool with multi-URL fallback.
func (t *WebFetchTool) Execute(ctx context.Context, args map[string]interface{}) (*core.ToolResult, error) {
	// Build the URL list: support both "urls" array and legacy "url" string
	var urls []string

	// Try "urls" array first
	if rawURLs, ok := args["urls"]; ok {
		switch v := rawURLs.(type) {
		case []interface{}:
			for _, u := range v {
				if s, ok := u.(string); ok && s != "" {
					urls = append(urls, s)
				}
			}
		case []string:
			urls = v
		}
	}

	// Fallback to single "url" string (backward compatibility)
	if len(urls) == 0 {
		if singleURL, ok := args["url"].(string); ok && singleURL != "" {
			urls = []string{singleURL}
		}
	}

	if len(urls) == 0 {
		return core.NewErrorResult("At least one URL is required (use 'urls' array or 'url' string)"), nil
	}

	// Try each URL in order until one succeeds
	// Limit fallback to max 1 retry (total 2 URLs) to save LLM tokens and prevent provider rate limits
	if len(urls) > 2 {
		urls = urls[:2]
	}

	var lastErr error
	for i, targetURL := range urls {
		// Validate URL
		if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
			log.Printf("Warning: skipping invalid URL (no http/https): %s", targetURL)
			lastErr = fmt.Errorf("invalid URL: %s", targetURL)
			continue
		}

		text, err := t.fetchSingleURL(ctx, targetURL)
		if err != nil {
			lastErr = err
			log.Printf("Warning: failed to fetch URL %d/%d (%s): %v, trying next...", i+1, len(urls), targetURL, err)
			continue
		}

		// Success! Return immediately
		return core.NewSuccessResult(text), nil
	}

	// All URLs failed
	return core.NewErrorResult(fmt.Sprintf("All %d URLs failed. Last error: %v", len(urls), lastErr)), nil
}

// fetchSingleURL fetches and cleans content from a single URL.
func (t *WebFetchTool) fetchSingleURL(ctx context.Context, targetURL string) (string, error) {
	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set common headers to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ZeroClaw/1.0; +https://zeroclaw.ai)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	// Execute request
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP error: %s", resp.Status)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Clean HTML and extract text
	text := t.cleanHTML(string(body))

	// Truncate to max chars
	if len(text) > t.maxChars {
		text = text[:t.maxChars] + "\n\n[... content truncated ...]"
	}

	return text, nil
}

// Spec returns the full tool specification.
func (t *WebFetchTool) Spec() core.ToolSpec {
	return core.ToolSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.ParametersSchema(),
	}
}

// cleanHTML removes HTML tags and extracts readable text content.
func (t *WebFetchTool) cleanHTML(html string) string {
	// Convert to lowercase for tag matching
	_ = strings.ToLower(html) // htmlLower unused but kept for reference

	// Remove script tags and their content
	html = scriptPattern.ReplaceAllString(html, "")

	// Remove style tags and their content
	html = stylePattern.ReplaceAllString(html, "")

	// Remove nav tags and their content
	html = navPattern.ReplaceAllString(html, "")

	// Remove footer tags and their content
	html = footerPattern.ReplaceAllString(html, "")

	// Remove header tags and their content
	html = headerPattern.ReplaceAllString(html, "")

	// Remove aside tags and their content
	html = asidePattern.ReplaceAllString(html, "")

	// Remove comments
	html = commentPattern.ReplaceAllString(html, "")

	// Replace common block elements with newlines
	html = blockElements.ReplaceAllString(html, "\n")

	// Remove all remaining HTML tags
	html = tagPattern.ReplaceAllString(html, "")

	// Decode HTML entities
	html = decodeHTMLEntities(html)

	// Clean up whitespace
	html = cleanWhitespace(html)

	return html
}

// decodeHTMLEntities converts common HTML entities to characters.
func decodeHTMLEntities(s string) string {
	// Common HTML entities
	replacements := map[string]string{
		"&nbsp;":    " ",
		"&amp;":     "&",
		"&lt;":      "<",
		"&gt;":      ">",
		"&quot;":     "\"",
		"&apos;":   "'",
		"&mdash;":   "—",
		"&ndash;":   "–",
		"&copy;":    "©",
		"&reg;":     "®",
		"&trade;":   "™",
		"&lsquo;":   "'",
		"&rsquo;":   "'",
		"&ldquo;":   "\"",
		"&rdquo;":   "\"",
		"&hellip;":  "…",
		"&bull;":   "•",
		"&middot;": "·",
	}

	for entity, char := range replacements {
		s = strings.ReplaceAll(s, entity, char)
	}

	// Handle numeric entities (&#nnn; and &#xhh;)
	s = numPattern.ReplaceAllStringFunc(s, func(match string) string {
		var code int
		fmt.Sscanf(match, "&#%d;", &code)
		return string(rune(code))
	})

	s = hexPattern.ReplaceAllStringFunc(s, func(match string) string {
		var code int
		fmt.Sscanf(match, "&#x%x;", &code)
		return string(rune(code))
	})

	return s
}

// cleanWhitespace reduces multiple whitespace to single spaces and trims.
func cleanWhitespace(s string) string {
	// Replace multiple whitespace with single space
	s = whitespacePattern.ReplaceAllString(s, " ")

	// Split into lines, clean each, rejoin
	lines := strings.Split(s, "\n")
	var cleanedLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 0 {
			cleanedLines = append(cleanedLines, line)
		}
	}

	// Join with double newlines to separate paragraphs
	return strings.Join(cleanedLines, "\n\n")
}

// ============================================================================
// ALTERNATIVE: Plain HTTP Client for simple content
// ============================================================================

// simpleCleanHTML provides a simple HTML cleaning without regex complexity
func simpleCleanHTML(html string) string {
	var buf bytes.Buffer
	inTag := false
	inEntity := false
	entityBuf := make([]rune, 0)

	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			// Add newline after block tags
			if len(entityBuf) > 0 {
				buf.WriteString(string(entityBuf))
				entityBuf = entityBuf[:0]
			}
		case r == '&':
			inEntity = true
			entityBuf = append(entityBuf, r)
		case inEntity:
			entityBuf = append(entityBuf, r)
			if r == ';' {
				// Complete entity - simplified handling
				entityStr := string(entityBuf)
				switch entityStr {
				case "&nbsp;", "&amp;", "&lt;", "&gt;", "&quot;", "&apos;":
					buf.WriteRune(' ')
				}
				entityBuf = entityBuf[:0]
				inEntity = false
			}
		case !inTag && !inEntity:
			buf.WriteRune(r)
		}
	}

	return buf.String()
}
