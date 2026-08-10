package auth

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateLegibleOrgName_Format(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][a-z]+ [a-z1-9]{4}$`)
	for range 100 {
		name := generateLegibleOrgName()
		require.True(t, pattern.MatchString(name), "name %q does not match %s", name, pattern)
	}
}

func TestGenerateLegibleOrgName_Distribution(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 200)
	for range 200 {
		seen[generateLegibleOrgName()] = struct{}{}
	}
	require.Greater(t, len(seen), 100, "expected diverse names, got %d unique of 200", len(seen))
}

func TestGenerateLegibleOrgName_PassesValidation(t *testing.T) {
	t.Parallel()

	for range 200 {
		name := generateLegibleOrgName()
		require.NoError(t, validateOrgName(name), "generated name %q must pass validateOrgName", name)
	}
}

func TestValidateOrgName(t *testing.T) {
	t.Parallel()

	shortOrgName := fmt.Sprintf(shortOrgNameFormat, minOrgNameSlugChars)

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "simple", input: "Acme Inc", wantErr: ""},
		{name: "hyphens and underscores", input: "acme-corp_2", wantErr: ""},
		{name: "at the length limit", input: strings.Repeat("a", 100), wantErr: ""},
		{name: "empty", input: "", wantErr: "org name is required"},
		{name: "whitespace only", input: "   ", wantErr: "org name is required"},
		{name: "apostrophe", input: "Bob's Bakery", wantErr: "organization name contains invalid characters"},
		{name: "over the length limit", input: strings.Repeat("a", 101), wantErr: "organization name is too long"},
		{name: "two slug chars", input: "ab", wantErr: ""},
		{name: "two slug chars separated", input: "a-b", wantErr: ""},
		{name: "only hyphens", input: "-----", wantErr: shortOrgName},
		{name: "only underscores", input: "___", wantErr: shortOrgName},
		{name: "punctuation with spaces", input: "- _ -", wantErr: shortOrgName},
		{name: "one slug char", input: "a", wantErr: shortOrgName},
		{name: "one slug char with hyphen", input: "A-", wantErr: shortOrgName},
		{name: "one slug char with punctuation", input: "A _ -", wantErr: shortOrgName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateOrgName(tt.input)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
