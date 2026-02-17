package openapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTruncateWithHash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		maxLength int
		expected  struct {
			length    int
			hasHash   bool
			unchanged bool
		}
	}{
		{
			name:      "short string unchanged",
			input:     "short",
			maxLength: 100,
			expected: struct {
				length    int
				hasHash   bool
				unchanged bool
			}{
				length:    5,
				hasHash:   false,
				unchanged: true,
			},
		},
		{
			name:      "exact length unchanged",
			input:     strings.Repeat("a", 100),
			maxLength: 100,
			expected: struct {
				length    int
				hasHash   bool
				unchanged bool
			}{
				length:    100,
				hasHash:   false,
				unchanged: true,
			},
		},
		{
			name:      "long string truncated with hash",
			input:     strings.Repeat("a", 150),
			maxLength: 100,
			expected: struct {
				length    int
				hasHash   bool
				unchanged bool
			}{
				length:    100,
				hasHash:   true,
				unchanged: false,
			},
		},
		{
			name:      "very long operation ID",
			input:     "GET_/api/v1/organizations/{orgId}/projects/{projectId}/deployments/{deploymentId}/tools/{toolId}/variations/{variationId}/settings",
			maxLength: 255,
			expected: struct {
				length    int
				hasHash   bool
				unchanged bool
			}{
				length:    130, // original length, should be unchanged
				hasHash:   false,
				unchanged: true,
			},
		},
		{
			name:      "extremely long operation ID requiring truncation",
			input:     strings.Repeat("GET_/api/v1/very/long/path/", 20), // ~540 characters
			maxLength: 255,
			expected: struct {
				length    int
				hasHash   bool
				unchanged bool
			}{
				length:    255,
				hasHash:   true,
				unchanged: false,
			},
		},
		{
			name:      "tool name with long slug and opID",
			input:     "very_long_document_slug_name_" + strings.Repeat("operation_id_", 10),
			maxLength: 100,
			expected: struct {
				length    int
				hasHash   bool
				unchanged bool
			}{
				length:    100,
				hasHash:   true,
				unchanged: false,
			},
		},
		{
			name:      "edge case - maxLength smaller than hash",
			input:     "some_long_string",
			maxLength: 4, // smaller than 8-char hash
			expected: struct {
				length    int
				hasHash   bool
				unchanged bool
			}{
				length:    8, // should return just the hash
				hasHash:   true,
				unchanged: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := truncateWithHash(tt.input, tt.maxLength)

			// Check length
			if tt.expected.unchanged {
				require.Len(t, result, len(tt.input), "unchanged string should have same length")
				require.Equal(t, tt.input, result, "unchanged string should be identical")
			} else {
				// Special case: when maxLength is smaller than hash, we return just the hash
				if tt.maxLength < 8 {
					require.Len(t, result, 8, "when maxLength < 8, should return 8-char hash")
				} else {
					require.LessOrEqual(t, len(result), tt.maxLength, "result should not exceed maxLength")
				}
				require.Len(t, result, tt.expected.length, "result should have expected length")
			}

			// Check if hash is present
			if tt.expected.hasHash {
				// Hash should be at the end and be 8 characters of hex
				if len(result) >= 8 {
					hash := result[len(result)-8:]
					require.Regexp(t, "^[a-f0-9]{8}$", hash, "should end with 8-character hex hash")
				}
			}

			// Ensure uniqueness - same input should always produce same output
			result2 := truncateWithHash(tt.input, tt.maxLength)
			require.Equal(t, result, result2, "function should be deterministic")
		})
	}
}

func TestTruncateWithHashUniqueness(t *testing.T) {
	t.Parallel()
	// Test that different long strings produce different truncated results
	maxLength := 50

	input1 := strings.Repeat("a", 100) + "different_ending_1"
	input2 := strings.Repeat("a", 100) + "different_ending_2"

	result1 := truncateWithHash(input1, maxLength)
	result2 := truncateWithHash(input2, maxLength)

	require.NotEqual(t, result1, result2, "different inputs should produce different truncated results")
	require.Len(t, result1, maxLength, "result1 should be exactly maxLength")
	require.Len(t, result2, maxLength, "result2 should be exactly maxLength")
}

func TestTruncateWithHashConsistency(t *testing.T) {
	t.Parallel()
	// Test that the same input always produces the same hash
	input := "this_is_a_very_long_operation_id_that_needs_to_be_truncated_with_a_hash_for_uniqueness"
	maxLength := 50

	results := make([]string, 10)
	for i := range 10 {
		results[i] = truncateWithHash(input, maxLength)
	}

	// All results should be identical
	for i := 1; i < len(results); i++ {
		require.Equal(t, results[0], results[i], "function should be deterministic across multiple calls")
	}
}

func TestRealWorldScenarios(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		scenario string
		input    string
		limit    int
	}{
		{
			name:     "tool name with long document slug",
			scenario: "document slug + operation ID forming tool name",
			input:    "my_very_long_api_document_name_v2_final_GET_/api/v1/users/{userId}/preferences/{preferenceId}/settings/{settingId}",
			limit:    100,
		},
		{
			name:     "openapi operation ID",
			scenario: "very long operation ID from OpenAPI spec",
			input:    "getUserPreferencesSettingsWithAdvancedFilteringAndPaginationAndSortingOptions",
			limit:    255,
		},
		{
			name:     "generated operation ID from method and path",
			scenario: "operation ID generated from HTTP method and long path",
			input:    "POST_/api/v1/organizations/{orgId}/projects/{projectId}/deployments/{deploymentId}/tools/{toolId}/variations/{variationId}/settings/{settingId}/overrides",
			limit:    255,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := truncateWithHash(tt.input, tt.limit)

			require.LessOrEqual(t, len(result), tt.limit, "result should not exceed limit")

			if len(tt.input) > tt.limit {
				// Should be truncated with hash
				require.Len(t, result, tt.limit, "truncated result should be exactly at limit")
				require.NotEqual(t, tt.input, result, "should be different from original")

				// Should end with 8-character hex hash
				hash := result[len(result)-8:]
				require.Regexp(t, "^[a-f0-9]{8}$", hash, "should end with hex hash")
			} else {
				// Should be unchanged
				require.Equal(t, tt.input, result, "short input should be unchanged")
			}
		})
	}
}

func TestConstraintLimits(t *testing.T) {
	t.Parallel()
	// Test the actual constraint limits we've set
	tests := []struct {
		name  string
		limit int
		field string
	}{
		{
			name:  "tool name constraint",
			limit: 100,
			field: "name",
		},
		{
			name:  "openapi operation constraint",
			limit: 255,
			field: "openapiv3_operation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test at boundary
			exactLimit := strings.Repeat("a", tt.limit)
			result := truncateWithHash(exactLimit, tt.limit)
			require.Equal(t, exactLimit, result, "string at exact limit should be unchanged")
			require.Len(t, result, tt.limit)

			// Test over boundary
			overLimit := strings.Repeat("a", tt.limit+50)
			result = truncateWithHash(overLimit, tt.limit)
			require.Len(t, result, tt.limit, "over-limit string should be truncated to exact limit")
			require.NotEqual(t, overLimit, result, "should be different from original")
		})
	}
}
