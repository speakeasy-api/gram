package plog

import (
	"path/filepath"
)

// OmitMatcher matches field names against glob patterns with caching.
type OmitMatcher struct {
	patterns []string
}

// NewOmitMatcher creates a new OmitMatcher with the given patterns.
func NewOmitMatcher(patterns []string) *OmitMatcher {
	return &OmitMatcher{
		patterns: patterns,
	}
}

// Match returns true if the field name matches any of the omit patterns.
func (m *OmitMatcher) Match(field string) bool {
	if len(m.patterns) == 0 {
		return false
	}

	for _, pattern := range m.patterns {
		if matched, _ := filepath.Match(pattern, field); matched {
			return true
		}
	}
	return false
}
