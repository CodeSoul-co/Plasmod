package storage

import (
	"regexp"
	"strings"
)

var datasetFileRE = regexp.MustCompile(`(?:^|\s)dataset=([^\s]+)`)

// ExtractDatasetFileNameFromText parses "dataset=<file>" from free text.
// It trims common trailing punctuation so "dataset=foo.ibin," resolves to "foo.ibin".
func ExtractDatasetFileNameFromText(text string) string {
	m := datasetFileRE.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return NormalizeDatasetFileName(m[1])
}

// NormalizeDatasetFileName trims spaces/quotes and trailing punctuation.
func NormalizeDatasetFileName(s string) string {
	v := strings.TrimSpace(s)
	v = strings.Trim(v, "\"'`")
	v = strings.TrimRight(v, ".,;:!?)]}>")
	return strings.TrimSpace(v)
}
