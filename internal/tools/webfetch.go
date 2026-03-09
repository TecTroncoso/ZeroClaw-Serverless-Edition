// Package tools implements agent-callable tools for ZeroClaw.
// This module provides the WebFetch tool for retrieving and cleaning web page content.
package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/zeroclaw/zeroclaw-go/internal/core"
)

// ============================================================================
// WEB FETCH TOOL
// ============================================================================

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
	timeout := 8 // Default 8 seconds for serverless
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
	return "Fetches a web page and extracts its text content. Useful for retrieving information from URLs. Automatically cleans HTML to extract readable text."
}

// ParametersSchema returns the JSON Schema for tool parameters.
func (t *WebFetchTool) ParametersSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch (must start with http:// or https://)",
			},
		},
		"required": []string{"url"},
	}
}

// Execute runs the web fetch tool.
func (t *WebFetchTool) Execute(ctx context.Context, args map[string]interface{}) (*core.ToolResult, error) {
	// Extract URL
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("url is required")
	}

	// Validate URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("url must start with http:// or https://")
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set common headers to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ZeroClaw/1.0; +https://zeroclaw.ai)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	// Execute request
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Clean HTML and extract text
	text := t.cleanHTML(string(body))

	// Truncate to max chars
	if len(text) > t.maxChars {
		text = text[:t.maxChars] + "\n\n[... content truncated ...]"
	}

	return core.NewSuccessResult(text), nil
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
	htmlLower := strings.ToLower(html)

	// Remove script tags and their content
	scriptPattern := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	html = scriptPattern.ReplaceAllString(html, "")

	// Remove style tags and their content
	stylePattern := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = stylePattern.ReplaceAllString(html, "")

	// Remove nav tags and their content
	navPattern := regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	html = navPattern.ReplaceAllString(html, "")

	// Remove footer tags and their content
	footerPattern := regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	html = footerPattern.ReplaceAllString(html, "")

	// Remove header tags and their content
	headerPattern := regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	html = headerPattern.ReplaceAllString(html, "")

	// Remove aside tags and their content
	asidePattern := regexp.MustCompile(`(?is)<aside[^>]*>.*?</aside>`)
	html = asidePattern.ReplaceAllString(html, "")

	// Remove comments
	commentPattern := regexp.MustCompile(`(?is)<!--.*?-->`)
	html = commentPattern.ReplaceAllString(html, "")

	// Replace common block elements with newlines
	blockElements := regexp.MustCompile(`(?i)</?(div|p|br|h[1-6]|li|tr|td|th|article|section|main|address)`)
	html = blockElements.ReplaceAllString(html, "\n")

	// Remove all remaining HTML tags
	tagPattern := regexp.MustCompile(`<[^>]+>`)
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
		"&":     "&",
		"<":      "<",
		">":      ">",
		""":     "\"",
		"'":    "'",
		"'":     "'",
		"&mdash;":   "—",
		"&ndash;":   "–",
		"&copy;":    "©",
		"&reg;":     "®",
		"&trade;":   "™",
		"&lsquo;":   "'",
		"&rsquo;":   "'",
		"&ldquo;":   """,
		"&rdquo;":   """,
		"&hellip;":  "…",
		"&bull;":    "•",
		"&middot;":  "·",
	}

	for entity, char := range replacements {
		s = strings.ReplaceAll(s, entity, char)
	}

	// Handle numeric entities (&#nnn; and &#xhh;)
	numPattern := regexp.MustCompile(`&#(\d+);`)
	s = numPattern.ReplaceAllStringFunc(s, func(match string) string {
		var code int
		fmt.Sscanf(match, "&#%d;", &code)
		return string(rune(code))
	})

	hexPattern := regexp.MustCompile(`&#x([0-9a-fA-F]+);`)
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
	whitespacePattern := regexp.MustCompile(`\s+`)
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
				case "&nbsp;", "&", "<", ">", """, "'":
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
