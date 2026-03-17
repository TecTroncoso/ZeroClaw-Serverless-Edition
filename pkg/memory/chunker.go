package memory

import (
	"strings"
	"unicode"
)

// ChunkText splits a long text into smaller segments of roughly maxLen characters,
// overlapping each segment by 'overlap' characters to preserve context.
// It attempts to split at natural word boundaries to avoid cutting sentences/words in half.
func ChunkText(text string, maxLen int, overlap int) []string {
	if text == "" {
		return nil
	}
	if maxLen <= 0 {
		return []string{text} // Invalid maxLen
	}
	// Cap overlap at 50% of maxLen to prevent infinite loops or uselessly small progression
	if overlap >= maxLen/2 {
		overlap = maxLen / 2
	}

	text = strings.TrimSpace(text)
	rtext := []rune(text)
	if len(rtext) <= maxLen {
		return []string{text}
	}

	var chunks []string
	length := len(rtext)
	start := 0

	for start < length {
		end := start + maxLen

		if end >= length {
			chunks = append(chunks, string(rtext[start:]))
			break
		}

		// Try to find a natural break near the end limit
		breakPoint := findNaturalBreak(rtext, start, end)
		if breakPoint == -1 {
			// No natural break found, hard split
			breakPoint = end
		}

		chunks = append(chunks, string(rtext[start:breakPoint]))

		// Move start forward to breakPoint, then backtrack by overlap
		nextStart := breakPoint - overlap
		
		// Prevent infinite loops where we don't advance at all
		if nextStart <= start {
			nextStart = start + 1 // Force at least 1 character progression
		}

		start = nextStart
	}

	return chunks
}

// findNaturalBreak looks backwards from 'end' for whitespace or punctuation.
// It searches backwards up to 30% of the chunk length.
func findNaturalBreak(runes []rune, start, end int) int {
	limit := start + int(float64(end-start)*0.7)
	
	// First priority: Punctuation followed by space (., !, ?, \n)
	for i := end - 1; i > limit; i-- {
		if (runes[i] == ' ' || runes[i] == '\n') && i > start {
			if isPunctuation(runes[i-1]) {
				return i + 1 // Break after the space
			}
		}
	}

	// Second priority: Any whitespace (space, newline, tab)
	for i := end - 1; i > limit; i-- {
		if unicode.IsSpace(runes[i]) {
			return i + 1 // Break after the space
		}
	}

	// No good break point found in the search window
	return -1
}

func isPunctuation(r rune) bool {
	return r == '.' || r == '!' || r == '?' || r == ';' || r == ':'
}
