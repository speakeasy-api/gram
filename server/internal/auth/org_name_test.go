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
		validated, err := validateOrgName(name)
		require.NoError(t, err, "generated name %q must pass validateOrgName", name)
		require.Equal(t, name, validated, "generated name %q must survive normalization unchanged", name)
	}
}

func TestValidateOrgName_Accepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "simple", input: "Acme Inc"},
		{name: "hyphens and underscores", input: "acme-corp_2"},
		{name: "comma and period", input: "Acme, Inc."},
		{name: "apostrophe", input: "Bob's Bakery"},
		{name: "curly apostrophe", input: "Bob\u2019s Bakery"},
		{name: "ampersand", input: "Acme & Sons"},
		{name: "exclamation mark", input: "Acme!"},
		{name: "parentheses", input: "Acme Inc (US)"},
		{name: "double quotes", input: `Acme "Quality" Goods`},
		{name: "slash", input: "Acme/Zenith Holdings"},
		{name: "accented latin", input: "Café Zoë"},
		{name: "german umlaut and eszett", input: "Grünwald Straße"},
		{name: "han", input: "顶尖科技"},
		{name: "cyrillic", input: "Акме"},
		{name: "katakana", input: "アクメ株式会社"},
		{name: "arabic", input: "شركة أكمي"},
		{name: "letter numbers", input: "ⅫⅡ"},
		{name: "emoji alongside letters", input: "Acme 🚀"},
		{name: "zero-width joiner between letters", input: "अ\u200dब"},
		{name: "at the length limit", input: strings.Repeat("a", maxOrgNameLength)},
		{name: "at the length limit in runes not bytes", input: strings.Repeat("字", maxOrgNameLength)},
		{name: "two letters", input: "ab"},
		{name: "two letters separated", input: "a-b"},
	}

	for _, tt := range tests {
		validated, err := validateOrgName(tt.input)
		require.NoError(t, err, tt.name)
		require.Equal(t, tt.input, validated, "%s: an already-normalized name must come back unchanged", tt.name)
	}
}

func TestValidateOrgName_Rejects(t *testing.T) {
	t.Parallel()

	shortOrgName := fmt.Sprintf(shortOrgNameFormat, minOrgNameLettersOrNumbers)

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "empty", input: "", wantErr: "org name is required"},
		{name: "spaces only", input: "   ", wantErr: "org name is required"},
		{name: "unicode spaces only", input: "\u00a0\u3000", wantErr: "org name is required"},
		{name: "newline", input: "Acme\n\nInc", wantErr: "organization name contains invalid characters"},
		{name: "tab", input: "Acme\tInc", wantErr: "organization name contains invalid characters"},
		{name: "line separator", input: "Acme\u2028Inc", wantErr: "organization name contains invalid characters"},
		{name: "right-to-left override", input: "Acme\u202eInc", wantErr: "organization name contains invalid characters"},
		{name: "zero-width space", input: "Acme\u200bInc", wantErr: "organization name contains invalid characters"},
		{name: "private use", input: "Acme\uf8ffInc", wantErr: "organization name contains invalid characters"},
		{name: "invalid utf-8", input: "Acme\xffInc", wantErr: "organization name contains invalid characters"},
		{name: "over the length limit", input: strings.Repeat("a", maxOrgNameLength+1), wantErr: "organization name is too long"},
		{name: "over the length limit in runes", input: strings.Repeat("字", maxOrgNameLength+1), wantErr: "organization name is too long"},
		{name: "only hyphens", input: "-----", wantErr: shortOrgName},
		{name: "only underscores", input: "___", wantErr: shortOrgName},
		{name: "punctuation with spaces", input: "- _ -", wantErr: shortOrgName},
		{name: "only symbols", input: "€ £ ¥", wantErr: shortOrgName},
		{name: "only emoji", input: "🚀🚀", wantErr: shortOrgName},
		{name: "one letter", input: "a", wantErr: shortOrgName},
		{name: "one letter with hyphen", input: "A-", wantErr: shortOrgName},
		{name: "one letter with punctuation", input: "A _ -", wantErr: shortOrgName},
	}

	for _, tt := range tests {
		validated, err := validateOrgName(tt.input)
		require.Error(t, err, tt.name)
		require.Contains(t, err.Error(), tt.wantErr, tt.name)
		require.Empty(t, validated, "%s: a rejected name must not come back as something to store", tt.name)
	}
}

func TestValidateOrgName_Normalizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trims", input: "  Acme Inc  ", want: "Acme Inc"},
		{name: "collapses runs of spaces", input: "Acme   Inc", want: "Acme Inc"},
		{name: "non-breaking space", input: "Acme\u00a0Inc", want: "Acme Inc"},
		{name: "ideographic space", input: "字节\u3000跳动", want: "字节 跳动"},
		{name: "narrow no-break space", input: "Acme\u202fInc", want: "Acme Inc"},
		{name: "mixed space separators", input: "\u00a0 Acme \u2003 Inc \u00a0", want: "Acme Inc"},
	}

	for _, tt := range tests {
		validated, err := validateOrgName(tt.input)
		require.NoError(t, err, tt.name)
		require.Equal(t, tt.want, validated, tt.name)
	}
}

// The signup parameter is unauthenticated, so an oversized value must be
// refused on sight rather than normalized first.
func TestValidateOrgName_RejectsOversizedInputBeforeNormalizing(t *testing.T) {
	t.Parallel()

	validated, err := validateOrgName(strings.Repeat(" ", maxRawOrgNameBytes+1))
	require.ErrorContains(t, err, "organization name is too long")
	require.Empty(t, validated)
}

func TestValidateOrgName_LengthLimitCountsRunes(t *testing.T) {
	t.Parallel()

	name := strings.Repeat("𝕏", maxOrgNameLength)
	require.Len(t, name, maxOrgNameLength*4, "expected a four-byte rune")

	validated, err := validateOrgName(name)
	require.NoError(t, err)
	require.Equal(t, name, validated)
}
