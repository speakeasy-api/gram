package contenttypes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{
			name:        "standard application/json",
			contentType: "application/json",
			expected:    true,
		},
		{
			name:        "application/json with charset",
			contentType: "application/json; charset=utf-8",
			expected:    true,
		},
		{
			name:        "vendor specific json",
			contentType: "application/vnd.api+json",
			expected:    true,
		},
		{
			name:        "text/json",
			contentType: "text/json",
			expected:    true,
		},
		{
			name:        "application/hal+json",
			contentType: "application/hal+json",
			expected:    true,
		},
		{
			name:        "application/ld+json",
			contentType: "application/ld+json",
			expected:    true,
		},
		{
			name:        "application/xml",
			contentType: "application/xml",
			expected:    false,
		},
		{
			name:        "text/plain",
			contentType: "text/plain",
			expected:    false,
		},
		{
			name:        "application/yaml",
			contentType: "application/yaml",
			expected:    false,
		},
		{
			name:        "empty string",
			contentType: "",
			expected:    false,
		},
		{
			name:        "case insensitive - uppercase should pass",
			contentType: "APPLICATION/JSON",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsJSON(tt.contentType)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{
			name:        "application/yaml",
			contentType: "application/yaml",
			expected:    true,
		},
		{
			name:        "text/yaml",
			contentType: "text/yaml",
			expected:    true,
		},
		{
			name:        "application/x-yaml",
			contentType: "application/x-yaml",
			expected:    true,
		},
		{
			name:        "text/x-yaml",
			contentType: "text/x-yaml",
			expected:    true,
		},
		{
			name:        "application/yaml with charset",
			contentType: "application/yaml; charset=utf-8",
			expected:    true,
		},
		{
			name:        "application/json",
			contentType: "application/json",
			expected:    false,
		},
		{
			name:        "text/plain",
			contentType: "text/plain",
			expected:    false,
		},
		{
			name:        "application/xml",
			contentType: "application/xml",
			expected:    false,
		},
		{
			name:        "empty string",
			contentType: "",
			expected:    false,
		},
		{
			name:        "case insensitive - uppercase should pass",
			contentType: "APPLICATION/YAML",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsYAML(tt.contentType)
			require.Equal(t, tt.expected, result)
		})
	}
}
