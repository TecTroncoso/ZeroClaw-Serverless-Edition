package memory

import (
	"reflect"
	"testing"
)

func TestChunkText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		overlap  int
		expected []string
	}{
		{
			name:     "Empty text",
			text:     "",
			maxLen:   100,
			overlap:  10,
			expected: nil,
		},
		{
			name:     "Text shorter than maxLen",
			text:     "Hello, this is a short text.",
			maxLen:   100,
			overlap:  10,
			expected: []string{"Hello, this is a short text."},
		},
		{
			name:     "Text exactly maxLen",
			text:     "1234567890",
			maxLen:   10,
			overlap:  2,
			expected: []string{"1234567890"},
		},
		{
			name:     "Basic split without natural breaks",
			text:     "abcdefghi",
			maxLen:   5,
			overlap:  2,
			expected: []string{"abcde", "defgh", "ghi"},
		},
		{
			name:     "Natural split at space",
			text:     "hello world test",
			maxLen:   8,
			overlap:  2,
			expected: []string{"hello ", "lo world", "ld test"},
		},
		{
			name:     "Natural split at punctuation",
			text:     "Hello world. How are you today? I am fine.",
			maxLen:   18,
			overlap:  5,
			expected: []string{"Hello world. ", "rld. How are you ", "you today? I am ", "am fine."},
		},
		{
			name:     "Overlap too big",
			text:     "abcdefghi",
			maxLen:   4,
			overlap:  10, // Will be capped at 2
			expected: []string{"abcd", "cdef", "efgh", "ghi"},
		},
		{
			name:     "Unicode characters",
			text:     "Hola 🌍, cómo estás el día de hoy.",
			maxLen:   15,
			overlap:  3,
			expected: []string{"Hola 🌍, cómo ", "mo estás el día", "día de hoy."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ChunkText(tt.text, tt.maxLen, tt.overlap)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("\nExpected: %q\nGot:      %q", tt.expected, result)
			}
		})
	}
}
