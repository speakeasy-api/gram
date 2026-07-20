package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSkillManifestMinimal(t *testing.T) {
	t.Parallel()

	content := "---\nname: deploy-helper\ndescription: Deploys the current application.\n---\n\n# Deploy helper\n"

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.Equal(t, content, manifest.RawContent)
	require.Equal(t, "deploy-helper", manifest.Name)
	require.Equal(t, "deploy-helper", manifest.DisplayName)
	require.NotNil(t, manifest.Description)
	require.Equal(t, "Deploys the current application.", *manifest.Description)
	require.Empty(t, manifest.Metadata)
	require.True(t, manifest.SpecValid)
	require.Empty(t, manifest.ValidationErrors)
	require.Len(t, manifest.RawSHA256, 64)
	require.Len(t, manifest.CanonicalSHA256, 64)
}

func TestParseSkillManifestFullMetadata(t *testing.T) {
	t.Parallel()

	content := `---
name: release-notes
description: Produces release notes.
compatibility: Requires git 2.40 or newer.
license: Apache-2.0
allowed-tools: Bash(git:*) Read
metadata:
  author: platform
  version: "1"
x-gram-setting: tolerated
---
Write concise release notes.
`

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
	require.Equal(t, map[string]any{"author": "platform", "version": "1"}, manifest.Metadata)
	require.Equal(t, map[string]any{
		"name":           "release-notes",
		"description":    "Produces release notes.",
		"compatibility":  "Requires git 2.40 or newer.",
		"license":        "Apache-2.0",
		"allowed-tools":  "Bash(git:*) Read",
		"metadata":       map[string]any{"author": "platform", "version": "1"},
		"x-gram-setting": "tolerated",
	}, manifest.Frontmatter)
	require.NotNil(t, manifest.Description)
	require.Equal(t, "Produces release notes.", *manifest.Description)
}

func TestParseSkillManifestUnknownTopLevelKeysAreTolerated(t *testing.T) {
	t.Parallel()

	content := "---\nname: unknown-keys\ndescription: Valid description.\nfuture:\n  nested: [one, two]\n---\nbody\n"

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
	require.Empty(t, manifest.ValidationErrors)
	require.Equal(t, map[string]any{
		"name":        "unknown-keys",
		"description": "Valid description.",
		"future":      map[string]any{"nested": []any{"one", "two"}},
	}, manifest.Frontmatter)
}

func TestParseSkillManifestFatalInputs(t *testing.T) {
	t.Parallel()

	fatalInputs := []struct {
		label   string
		content string
	}{
		{label: "empty", content: ""},
		{label: "invalid UTF-8", content: string([]byte{'-', '-', '-', '\n', 0xff})},
		{label: "NUL", content: "---\nname: nul\x00name\n---\n"},
		{label: "missing opening delimiter", content: "name: absent\n---\n"},
		{label: "opening delimiter is not first line", content: "\n---\nname: absent\n---\n"},
		{label: "opening delimiter is not exact", content: "----\nname: absent\n---\n"},
		{label: "missing closing delimiter", content: "---\nname: absent\n"},
		{label: "malformed YAML", content: "---\nname: [\n---\n"},
		{label: "multiple YAML documents", content: "---\nname: first\n...\nname: second\n---\n"},
		{label: "non-mapping YAML", content: "---\n- name\n- description\n---\n"},
		{label: "duplicate key", content: "---\nname: first\nname: second\n---\n"},
		{label: "duplicate normalized key", content: "---\nname: first\nmetadata:\n  café: one\n  café: two\n---\n"},
		{label: "non-string key", content: "---\nname: first\nmetadata:\n  12: value\n---\n"},
		{label: "anchor", content: "---\nname: first\nmetadata: &values\n  owner: team\n---\n"},
		{label: "alias", content: "---\nname: first\ndescription: &description value\nlicense: *description\n---\n"},
		{label: "merge key", content: "---\nname: first\ndescription: valid\nmetadata:\n  <<: {owner: team}\n---\n"},
		{label: "custom tag", content: "---\nname: first\ndescription: !custom value\n---\n"},
		{label: "positive infinity", content: "---\nname: first\ndescription: valid\nfuture: .inf\n---\n"},
		{label: "not a number", content: "---\nname: first\ndescription: valid\nfuture: .NaN\n---\n"},
		{label: "malformed explicit null", content: "---\nname: first\ndescription: valid\nfuture: !!null nope\n---\n"},
		{label: "missing name", content: "---\ndescription: missing name\n---\n"},
		{label: "non-string name", content: "---\nname: 12\n---\n"},
		{label: "empty name", content: "---\nname: \"  \"\n---\n"},
		{label: "unsupported name character", content: "---\nname: skill/name\n---\n"},
		{label: "non-ASCII name", content: "---\nname: skíll\n---\n"},
		{label: "leading separator", content: "---\nname: -skill\n---\n"},
		{label: "trailing separator", content: "---\nname: skill_\n---\n"},
		{label: "normalized name too long", content: "---\nname: " + strings.Repeat("a", 65) + "\n---\n"},
	}

	for _, test := range fatalInputs {
		_, err := parseSkillManifest(test.content)
		require.Error(t, err, test.label)
	}
}

func TestParseSkillManifestContentByteLimit(t *testing.T) {
	t.Parallel()

	prefix := "---\nname: byte-limit\ndescription: valid\n---\n"
	exact := prefix + strings.Repeat("x", maxSkillContentBytes-len(prefix)-2) + "  "

	manifest, err := parseSkillManifest(exact)
	require.NoError(t, err)
	require.Len(t, manifest.RawContent, maxSkillContentBytes)
	require.Len(t, manifest.canonicalContent, maxSkillContentBytes)

	_, err = parseSkillManifest(exact + "x")
	require.Error(t, err)
}

func TestParseSkillManifestUnicodeCodePointLimits(t *testing.T) {
	t.Parallel()

	validDescription := "---\nname: rune-limits\ndescription: " + strings.Repeat("界", 1024) + "\ncompatibility: " + strings.Repeat("界", 500) + "\n---\n"
	manifest, err := parseSkillManifest(validDescription)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)

	longDescription := "---\nname: rune-limits\ndescription: " + strings.Repeat("界", 1025) + "\n---\n"
	manifest, err = parseSkillManifest(longDescription)
	require.NoError(t, err)
	require.Equal(t, []validationError{{
		Code:    "too_long",
		Field:   "description",
		Message: "description must be at most 1024 Unicode code points",
	}}, manifest.ValidationErrors)

	longCompatibility := "---\nname: rune-limits\ndescription: valid\ncompatibility: " + strings.Repeat("界", 501) + "\n---\n"
	manifest, err = parseSkillManifest(longCompatibility)
	require.NoError(t, err)
	require.Equal(t, []validationError{{
		Code:    "too_long",
		Field:   "compatibility",
		Message: "compatibility must be at most 500 Unicode code points",
	}}, manifest.ValidationErrors)
}

func TestParseSkillManifestPaddedUnicodeCodePointLimits(t *testing.T) {
	t.Parallel()

	valid := "---\nname: padded-limits\ndescription: \" " + strings.Repeat("界", 1022) + " \"\ncompatibility: \" " + strings.Repeat("界", 498) + " \"\n---\n"
	manifest, err := parseSkillManifest(valid)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
	require.NotNil(t, manifest.Description)
	require.Equal(t, strings.Repeat("界", 1022), *manifest.Description)

	tooLong := "---\nname: padded-limits\ndescription: \" " + strings.Repeat("界", 1023) + " \"\ncompatibility: \" " + strings.Repeat("界", 499) + " \"\n---\n"
	manifest, err = parseSkillManifest(tooLong)
	require.NoError(t, err)
	require.False(t, manifest.SpecValid)
	require.Equal(t, []validationError{
		{Code: "too_long", Field: "compatibility", Message: "compatibility must be at most 500 Unicode code points"},
		{Code: "too_long", Field: "description", Message: "description must be at most 1024 Unicode code points"},
	}, manifest.ValidationErrors)
}

func TestParseSkillManifestRejectsExcessiveYAMLDepth(t *testing.T) {
	t.Parallel()

	content := "---\nname: deep\ndescription: valid\nfuture: " + strings.Repeat("[", maxSkillYAMLDepth) + "value" + strings.Repeat("]", maxSkillYAMLDepth) + "\n---\n"
	_, err := parseSkillManifest(content)
	require.ErrorContains(t, err, "maximum nesting depth")
}

func TestParseSkillManifestRejectsExcessiveYAMLNodes(t *testing.T) {
	t.Parallel()

	content := "---\nname: many-nodes\ndescription: valid\nfuture: [" + strings.Repeat("x,", maxSkillYAMLNodes) + "]\n---\n"
	require.Less(t, len(content), maxSkillContentBytes)
	_, err := parseSkillManifest(content)
	require.ErrorContains(t, err, "maximum node count")
}

func TestParseSkillManifestRejectsCanonicalExpansion(t *testing.T) {
	t.Parallel()

	pairs := make([]string, 1000)
	for i := range pairs {
		pairs[i] = "k" + strconv.Itoa(i) + ": value"
	}
	deepFlowMapping := strings.Repeat("{nested: ", 48) + "{" + strings.Join(pairs, ", ") + "}" + strings.Repeat("}", 48)
	content := "---\nname: expanded\ndescription: valid\nfuture: " + deepFlowMapping + "\n---\n"
	require.Less(t, len(content), maxSkillContentBytes)
	_, err := parseSkillManifest(content)
	require.ErrorContains(t, err, "canonical skill manifest exceeds 65536 bytes")
	require.ErrorIs(t, err, errCanonicalDocumentTooLarge)
}

func TestParseSkillManifestPreflightRejectsDeepFlowCollections(t *testing.T) {
	t.Parallel()

	contents := []string{
		"---\nname: deep-flow\ndescription: valid\nfuture: " + strings.Repeat("[", maxSkillYAMLDepth+1) + "value" + strings.Repeat("]", maxSkillYAMLDepth+1) + "\n---\n",
		"---\nname: deep-flow\ndescription: valid\nfuture: " + strings.Repeat("{key: ", maxSkillYAMLDepth+1) + "value" + strings.Repeat("}", maxSkillYAMLDepth+1) + "\n---\n",
		"---\nname: deep-flow\ndescription: it's valid\nfuture: " + strings.Repeat("[", maxSkillYAMLDepth+1) + "value" + strings.Repeat("]", maxSkillYAMLDepth+1) + "\n---\n",
		"---\nname: deep-flow\ndescription: valid\nfuture:\n child: " + strings.Repeat("[", maxSkillYAMLDepth) + "value" + strings.Repeat("]", maxSkillYAMLDepth) + "\n---\n",
	}
	for _, content := range contents {
		_, err := parseSkillManifest(content)
		require.ErrorIs(t, err, errYAMLSourceTooDeep)
		require.ErrorContains(t, err, "preflight skill manifest frontmatter")
	}
}

func TestParseSkillManifestPreflightRejectsDeepBlockIndentation(t *testing.T) {
	t.Parallel()

	var nested strings.Builder
	nested.WriteString("---\nname: deep-block\ndescription: valid\nfuture:\n")
	for i := 1; i <= maxSkillYAMLDepth+1; i++ {
		nested.WriteString(strings.Repeat(" ", i))
		nested.WriteString("key")
		nested.WriteString(strconv.Itoa(i))
		nested.WriteString(":\n")
	}
	nested.WriteString("---\n")

	_, err := parseSkillManifest(nested.String())
	require.ErrorIs(t, err, errYAMLSourceTooDeep)
	require.ErrorContains(t, err, "preflight skill manifest frontmatter")
}

func TestParseSkillManifestPreflightRejectsDeepCompactSequences(t *testing.T) {
	t.Parallel()

	contents := []string{
		"---\nname: deep-sequence\ndescription: valid\nfuture:\n" + strings.Repeat("- ", maxSkillYAMLDepth+1) + "value\n---\n",
		"---\nname: deep-sequence\ndescription: valid\nfuture:\n child:\n  " + strings.Repeat("- ", maxSkillYAMLDepth-1) + "value\n---\n",
	}
	for _, content := range contents {
		_, err := parseSkillManifest(content)
		require.ErrorIs(t, err, errYAMLSourceTooDeep)
		require.ErrorContains(t, err, "preflight skill manifest frontmatter")
	}
}

func TestPreflightYAMLSourceRejectsDepthAcrossUnicodeLineBreaks(t *testing.T) {
	t.Parallel()

	for _, separator := range []string{"\u0085", "\u2028", "\u2029"} {
		frontmatter := strings.Join([]string{
			"name: deep-sequence",
			"description: valid",
			"future: " + strings.Repeat("- ", maxSkillYAMLDepth+1) + "value",
		}, separator)

		require.ErrorIs(t, preflightYAMLSource(frontmatter), errYAMLSourceTooDeep)
	}
}

func TestParseSkillManifestPreflightAcceptsNormalSequences(t *testing.T) {
	t.Parallel()

	content := "---\nname: normal-sequences\ndescription: valid\nfuture:\n- one\n- - two\n  - three\n- hyphen-inside-plain-scalar\n---\n"
	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
}

func TestParseSkillManifestPreflightPlainPunctuationCannotMaskDepth(t *testing.T) {
	t.Parallel()

	deepSequence := strings.Repeat("- ", maxSkillYAMLDepth+1) + "value"
	contents := []string{
		"---\nname: punctuation-mask\ndescription: valid\nmask: text [\nfuture:\n" + deepSequence + "\n---\n",
		"---\nname: punctuation-mask\ndescription: valid\nmask: foo:'unterminated\nfuture:\n" + deepSequence + "\n---\n",
	}
	for _, content := range contents {
		_, err := parseSkillManifest(content)
		require.ErrorIs(t, err, errYAMLSourceTooDeep)
		require.ErrorContains(t, err, "preflight skill manifest frontmatter")
	}
}

func TestParseSkillManifestPreflightIgnoresNonStructuralBraces(t *testing.T) {
	t.Parallel()

	braces := strings.Repeat("[{", maxSkillYAMLDepth+10)
	blockSample := strings.Repeat("[{#", maxSkillYAMLDepth+10)
	content := "---\n" +
		"name: preflight-literals\n" +
		"description: |\n  " + blockSample + "\n" +
		"plain: text " + braces + braces + "\n" +
		"single: '" + braces + "''quoted''" + braces + "'\n" +
		"double: \"" + braces + "\\\"quoted\\\"" + braces + "\"\n" +
		"comment: value # " + braces + "\n" +
		"---\n"

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
}

func TestParseSkillManifestPreflightRecognizesTaggedBlockScalars(t *testing.T) {
	t.Parallel()

	braces := strings.Repeat("[{#", maxSkillYAMLDepth+10)
	valid := "---\nname: tagged-block\ndescription: valid\nmask: !!str |\n  " + braces + "\n---\n"
	manifest, err := parseSkillManifest(valid)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)

	deep := "---\nname: tagged-block\ndescription: valid\nmask: !!str |\n  " + braces + "\nfuture:\n" + strings.Repeat("- ", maxSkillYAMLDepth+1) + "value\n---\n"
	_, err = parseSkillManifest(deep)
	require.ErrorIs(t, err, errYAMLSourceTooDeep)

	anchored := "---\nname: tagged-block\ndescription: valid\nmask: &sample !!str |\n  " + braces + "\n---\n"
	_, err = parseSkillManifest(anchored)
	require.Error(t, err)
	require.NotErrorIs(t, err, errYAMLSourceTooDeep)
}

func TestPreflightYAMLSourceRejectsDeepFlowSiblingAfterCompactMappingBlockScalar(t *testing.T) {
	t.Parallel()

	frontmatter := "future:\n- key: !!str |1\n  sibling: " + strings.Repeat("[", maxSkillYAMLDepth+1) + "value" + strings.Repeat("]", maxSkillYAMLDepth+1) + "\n"
	require.ErrorIs(t, preflightYAMLSource(frontmatter), errYAMLSourceTooDeep)
}

func TestParseSkillManifestPreflightAcceptsBlockScalarContentAndSiblingMappings(t *testing.T) {
	t.Parallel()

	deepScalarContent := strings.Repeat("[", maxSkillYAMLDepth+1)
	frontmatters := []string{
		"future: |1\n " + deepScalarContent + "\nsibling: value\n",
		"future:\n  key: |1\n   " + deepScalarContent + "\n  sibling: value\n",
		"future:\n- key: |1\n   " + deepScalarContent + "\n  sibling: value\n",
	}
	for _, frontmatter := range frontmatters {
		content := "---\nname: block-scalar\ndescription: valid\n" + frontmatter + "---\n"
		manifest, err := parseSkillManifest(content)
		require.NoError(t, err)
		require.True(t, manifest.SpecValid)
	}
}

func TestParseSkillManifestPreflightScansFlowMapNodeProperties(t *testing.T) {
	t.Parallel()

	attack := "---\nname: flow-properties\ndescription: valid\nfuture: {\"k\":!!seq " + strings.Repeat("[", maxSkillYAMLDepth) + "value" + strings.Repeat("]", maxSkillYAMLDepth) + "}\n---\n"
	_, err := parseSkillManifest(attack)
	require.ErrorIs(t, err, errYAMLSourceTooDeep)
	require.ErrorContains(t, err, "preflight skill manifest frontmatter")

	valid := "---\nname: flow-properties\ndescription: valid\nfuture: {\"scalar\":!!str tagged, plain:!!str value, \"sequence\":!!seq [one, two]}\n---\n"
	manifest, err := parseSkillManifest(valid)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
}

func TestParseSkillManifestPreflightAnchorPropertyCannotMaskFlowDepth(t *testing.T) {
	t.Parallel()

	attack := "---\nname: anchor-mask\ndescription: valid\nfuture: [&a," + strings.Repeat("[", maxSkillYAMLDepth) + "value" + strings.Repeat("]", maxSkillYAMLDepth) + "]\n---\n"
	_, err := parseSkillManifest(attack)
	require.ErrorIs(t, err, errYAMLSourceTooDeep)
	require.ErrorContains(t, err, "preflight skill manifest frontmatter")
}

func TestParseSkillManifestPreflightIgnoresPlainScalarContinuationIndentation(t *testing.T) {
	t.Parallel()

	var content strings.Builder
	content.WriteString("---\nname: scalar-continuation\ndescription: valid\nmask: first line\n")
	for i := 1; i <= maxSkillYAMLDepth+10; i++ {
		content.WriteString(strings.Repeat(" ", i))
		content.WriteString("continuation line ")
		content.WriteString(strconv.Itoa(i))
		content.WriteByte('\n')
	}
	content.WriteString("---\n")

	manifest, err := parseSkillManifest(content.String())
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
}

func TestParseSkillManifestPreflightLeavesMalformedQuotesToYAML(t *testing.T) {
	t.Parallel()

	content := "---\nname: malformed-quote\ndescription: valid\nfuture: \"" + strings.Repeat("[", maxSkillYAMLDepth+10) + "\n---\n"
	_, err := parseSkillManifest(content)
	require.Error(t, err)
	require.NotErrorIs(t, err, errYAMLSourceTooDeep)
}

func TestParseSkillManifestNameNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		displayName string
		name        string
		specValid   bool
	}{
		{displayName: "My_Skill", name: "my-skill", specValid: false},
		{displayName: "my...skill", name: "my-skill", specValid: false},
		{displayName: "my-._-skill", name: "my-skill", specValid: false},
		{displayName: "Skill42", name: "skill42", specValid: false},
		{displayName: "skill-42", name: "skill-42", specValid: true},
		{displayName: strings.Repeat("a", 64), name: strings.Repeat("a", 64), specValid: true},
	}

	for _, test := range tests {
		content := "---\nname: \"  " + test.displayName + "  \"\ndescription: valid\n---\n"
		manifest, err := parseSkillManifest(content)
		require.NoError(t, err, test.displayName)
		require.Equal(t, test.displayName, manifest.DisplayName, test.displayName)
		require.Equal(t, test.name, manifest.Name, test.displayName)
		require.Equal(t, test.specValid, manifest.SpecValid, test.displayName)
	}
}

func TestParseSkillManifestNonfatalValidationErrorsAreSorted(t *testing.T) {
	t.Parallel()

	content := `---
name: My_Skill
description: []
compatibility: 12
license: false
allowed-tools: []
metadata:
  z: [one]
  a: true
---
`

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.False(t, manifest.SpecValid)
	require.Nil(t, manifest.Description)
	require.Equal(t, []validationError{
		{Code: "invalid_type", Field: "allowed-tools", Message: "allowed-tools must be a string"},
		{Code: "invalid_type", Field: "compatibility", Message: "compatibility must be a string"},
		{Code: "invalid_type", Field: "description", Message: "description must be a string"},
		{Code: "invalid_type", Field: "license", Message: "license must be a string"},
		{Code: "invalid_type", Field: "metadata.a", Message: "metadata values must be strings"},
		{Code: "invalid_type", Field: "metadata.z", Message: "metadata values must be strings"},
		{Code: "invalid_format", Field: "name", Message: "name must contain only lowercase letters, digits, and single hyphens, with alphanumeric boundaries, and be at most 64 characters"},
	}, manifest.ValidationErrors)
}

func TestParseSkillManifestEmptyOptionalValidation(t *testing.T) {
	t.Parallel()

	content := "---\nname: empty-values\ndescription: \" \"\ncompatibility: \"\t\"\nlicense: \"\"\nallowed-tools: \"\"\nmetadata: []\n---\n"

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.False(t, manifest.SpecValid)
	require.NotNil(t, manifest.Description)
	require.Empty(t, *manifest.Description)
	require.Equal(t, []validationError{
		{Code: "required", Field: "compatibility", Message: "compatibility must not be empty"},
		{Code: "required", Field: "description", Message: "description must not be empty"},
		{Code: "invalid_type", Field: "metadata", Message: "metadata must be a mapping"},
	}, manifest.ValidationErrors)
}

func TestParseSkillManifestMissingDescriptionIsNonfatal(t *testing.T) {
	t.Parallel()

	manifest, err := parseSkillManifest("---\nname: missing-description\n---\n")
	require.NoError(t, err)
	require.False(t, manifest.SpecValid)
	require.Nil(t, manifest.Description)
	require.Equal(t, []validationError{{
		Code:    "required",
		Field:   "description",
		Message: "description is required",
	}}, manifest.ValidationErrors)
}

func TestParseSkillManifestQuotedMergeKeyIsOrdinaryString(t *testing.T) {
	t.Parallel()

	content := "---\nname: quoted-merge\ndescription: valid\nmetadata:\n  \"<<\": value\n---\n"
	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
	require.Equal(t, map[string]any{"<<": "value"}, manifest.Metadata)
	canonical, err := parseSkillManifest(manifest.canonicalContent)
	require.NoError(t, err)
	require.Equal(t, manifest.canonicalContent, canonical.canonicalContent)
}

func TestParseSkillManifestExplicitNullEquivalence(t *testing.T) {
	t.Parallel()

	contents := []string{
		"---\nname: explicit-null\ndescription: valid\nfuture: null\n---\n",
		"---\nname: explicit-null\ndescription: valid\nfuture: !!null NULL\n---\n",
		"---\nname: explicit-null\ndescription: valid\nfuture: !!null \"\"\n---\n",
	}
	var canonicalSHA256 string
	for i, content := range contents {
		manifest, err := parseSkillManifest(content)
		require.NoError(t, err)
		if i == 0 {
			canonicalSHA256 = manifest.CanonicalSHA256
		}
		require.Equal(t, canonicalSHA256, manifest.CanonicalSHA256)
	}
}

func TestParseSkillManifestRejectsTimestampUTCRollover(t *testing.T) {
	t.Parallel()

	content := "---\nname: timestamp-rollover\ndescription: valid\nfuture: 9999-12-31T23:30:00-01:00\n---\n"
	_, err := parseSkillManifest(content)
	require.ErrorContains(t, err, "outside supported year range 0000..9999")
}

func TestParseSkillManifestRejectsIncompatibleTagKinds(t *testing.T) {
	t.Parallel()

	contents := []string{
		"---\nname: bad-tag\ndescription: valid\nfuture: !!map scalar\n---\n",
		"---\nname: bad-tag\ndescription: valid\nfuture: !!str {key: value}\n---\n",
		"---\nname: bad-tag\ndescription: valid\nfuture: !!map [one, two]\n---\n",
	}
	for _, content := range contents {
		_, err := parseSkillManifest(content)
		require.ErrorContains(t, err, "is incompatible with node kind")
	}
}

func TestParseSkillManifestRawSHA256UsesExactInput(t *testing.T) {
	t.Parallel()

	content := "\ufeff---\r\nname: raw-hash\r\ndescription: Exact bytes.  \r\n---\r\nbody\r\n"
	digest := sha256.Sum256([]byte(content))

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(digest[:]), manifest.RawSHA256)
	require.Equal(t, "2d5b2d82f906318ea31b7ba0a4d72fb5bef451a34e097f65a1532f784ac95817", manifest.RawSHA256)
}

func TestParseSkillManifestCanonicalGoldenHash(t *testing.T) {
	t.Parallel()

	content := "---\nname: golden-skill\ndescription: Golden manifest.\nmetadata:\n  owner: platform\n---\n\n# Golden\n"

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.Equal(t, "---\ndescription: Golden manifest.\nmetadata:\n  owner: platform\nname: golden-skill\n---\n\n# Golden\n", manifest.canonicalContent)
	require.Equal(t, "4e5b99304619cbcf26a69a54c4bf23d14476715403fa9c1c79df75b4b817ec78", manifest.CanonicalSHA256)
}

func TestParseSkillManifestCanonicalEquivalence(t *testing.T) {
	t.Parallel()

	base := "---\nname: My_Skill\ndescription: café\nmetadata:\n  owner: team\n---\n\n# Café\n"
	variants := []struct {
		label   string
		content string
	}{
		{label: "base", content: base},
		{label: "BOM", content: "\ufeff" + base},
		{label: "CRLF", content: strings.ReplaceAll(base, "\n", "\r\n")},
		{label: "lone CR", content: strings.ReplaceAll(base, "\n", "\r")},
		{label: "NFD", content: strings.ReplaceAll(base, "é", "é")},
		{label: "trailing whitespace", content: strings.ReplaceAll(base, "\n", " \t\n")},
		{
			label: "YAML order style and comments",
			content: `---
# comment
metadata: {owner: "team"}
description: 'café'
name: "My_Skill"
---

# Café
`,
		},
	}

	var canonicalSHA256 string
	for i, variant := range variants {
		manifest, err := parseSkillManifest(variant.content)
		require.NoError(t, err, variant.label)
		if i == 0 {
			canonicalSHA256 = manifest.CanonicalSHA256
			require.Contains(t, manifest.canonicalContent, "name: My_Skill\n")
		}
		require.Equal(t, canonicalSHA256, manifest.CanonicalSHA256, variant.label)
	}
}

func TestParseSkillManifestUnicodeTrailingWhitespaceEquivalence(t *testing.T) {
	t.Parallel()

	content := "---\nname: unicode-whitespace\ndescription: Canonical summary.\n---\n\n# Body\n"
	withTrailingWhitespace := strings.ReplaceAll(content, "\n", "\u00a0\u2003\n")

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	withUnicodeWhitespace, err := parseSkillManifest(withTrailingWhitespace)
	require.NoError(t, err)
	require.Equal(t, manifest.canonicalContent, withUnicodeWhitespace.canonicalContent)
	require.Equal(t, manifest.CanonicalSHA256, withUnicodeWhitespace.CanonicalSHA256)
}

func TestParseSkillManifestSemanticDifferencesChangeHash(t *testing.T) {
	t.Parallel()

	contents := []string{
		"---\nname: semantic\ndescription: first\nmetadata:\n  owner: team\n---\n\nbody\n",
		"---\nname: semantic\ndescription: second\nmetadata:\n  owner: team\n---\n\nbody\n",
		"---\nname: semantic\ndescription: first\nmetadata:\n  owner: other\n---\n\nbody\n",
		"---\nname: semantic\ndescription: first\nmetadata:\n  owner: team\n---\n\nother body\n",
	}
	hashes := make(map[string]struct{}, len(contents))
	for _, content := range contents {
		manifest, err := parseSkillManifest(content)
		require.NoError(t, err)
		hashes[manifest.CanonicalSHA256] = struct{}{}
	}
	require.Len(t, hashes, len(contents))
}

func TestParseSkillManifestPrimitiveScalarEquivalence(t *testing.T) {
	t.Parallel()

	base := `---
name: scalar-equivalence
description: valid
values:
  boolean: true
  integer: 16
  float: 1.2300
  nothing: null
  timestamp: 2026-07-15T00:00:00Z
  binary: !!binary SGVsbG8=
---
`
	equivalent := `---
description: valid
name: scalar-equivalence
values:
  binary: !!binary |
    SGVs
    bG8=
  timestamp: 2026-07-14T20:00:00-04:00
  nothing: ~
  float: 123e-2
  integer: 0x10
  boolean: TRUE
---
`

	first, err := parseSkillManifest(base)
	require.NoError(t, err)
	second, err := parseSkillManifest(equivalent)
	require.NoError(t, err)
	require.Equal(t, first.CanonicalSHA256, second.CanonicalSHA256)
	require.Equal(t, first.canonicalContent, second.canonicalContent)
	canonical, err := parseSkillManifest(first.canonicalContent)
	require.NoError(t, err)
	require.Equal(t, first.canonicalContent, canonical.canonicalContent)
}

func TestParseSkillManifestPrimitiveScalarDifferencesChangeHash(t *testing.T) {
	t.Parallel()

	base := "---\nname: scalar-differences\ndescription: valid\nvalues: {boolean: true, integer: 16, float: 1.23, nothing: null}\n---\n"
	variants := []string{
		strings.Replace(base, "boolean: true", "boolean: false", 1),
		strings.Replace(base, "integer: 16", "integer: 17", 1),
		strings.Replace(base, "float: 1.23", "float: 1.24", 1),
		strings.Replace(base, "nothing: null", "nothing: \"null\"", 1),
	}

	manifest, err := parseSkillManifest(base)
	require.NoError(t, err)
	for _, variant := range variants {
		different, err := parseSkillManifest(variant)
		require.NoError(t, err)
		require.NotEqual(t, manifest.CanonicalSHA256, different.CanonicalSHA256)
	}
}

func TestParseSkillManifestBlockScalarChompingChangesHash(t *testing.T) {
	t.Parallel()

	clip := "---\nname: block-chomping\ndescription: |\n  first line\n---\n"
	strip := "---\nname: block-chomping\ndescription: |-\n  first line\n---\n"
	clipped, err := parseSkillManifest(clip)
	require.NoError(t, err)
	stripped, err := parseSkillManifest(strip)
	require.NoError(t, err)
	require.NotEqual(t, clipped.CanonicalSHA256, stripped.CanonicalSHA256)
	require.NotEqual(t, clipped.canonicalContent, stripped.canonicalContent)
}

func TestParseSkillManifestEquivalentBlockScalarsAreStable(t *testing.T) {
	t.Parallel()

	contents := []string{
		"---\nname: block-stability\ndescription: |\n  first line\n  second line\n---\n",
		"---\nname: block-stability\ndescription: |2\n  first line  \n  second line\t\n---\n",
		"---\nname: block-stability\ndescription: \"first line\\nsecond line\\n\"\n---\n",
	}
	var canonicalContent string
	var canonicalSHA256 string
	for i, content := range contents {
		manifest, err := parseSkillManifest(content)
		require.NoError(t, err)
		if i == 0 {
			canonicalContent = manifest.canonicalContent
			canonicalSHA256 = manifest.CanonicalSHA256
		}
		require.Equal(t, canonicalContent, manifest.canonicalContent)
		require.Equal(t, canonicalSHA256, manifest.CanonicalSHA256)
	}

	canonical, err := parseSkillManifest(canonicalContent)
	require.NoError(t, err)
	require.Equal(t, canonicalContent, canonical.canonicalContent)
	require.Equal(t, canonicalSHA256, canonical.CanonicalSHA256)
}

func TestParseSkillManifestCanonicalizationIsIdempotent(t *testing.T) {
	t.Parallel()

	content := "\ufeff---\r\nmetadata: {z: two, a: one}\r\ndescription: 'café' # comment\r\nname: My__Skill\r\n---\r\n\r\n# Café  \r\n"

	first, err := parseSkillManifest(content)
	require.NoError(t, err)
	second, err := parseSkillManifest(first.canonicalContent)
	require.NoError(t, err)
	require.Equal(t, first.canonicalContent, second.canonicalContent)
	require.Equal(t, first.CanonicalSHA256, second.CanonicalSHA256)
}

func TestParseSkillManifestMetadataJSONCompatibility(t *testing.T) {
	t.Parallel()

	content := `---
name: metadata-json
description: Preserves metadata.
metadata:
  string: value
  bool: true
  integer: 123456789012345678901234567890
  float: 1.2300
  date: 2026-07-15
  nothing: null
  nested:
    list: [one, 2, false, null]
---
`

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.False(t, manifest.SpecValid)
	require.Equal(t, "value", manifest.Metadata["string"])
	require.Equal(t, true, manifest.Metadata["bool"])
	require.Equal(t, json.Number("1.2345678901234567890123456789e29"), manifest.Metadata["integer"])
	require.Equal(t, json.Number("1.23e0"), manifest.Metadata["float"])
	require.Equal(t, "2026-07-15T00:00:00Z", manifest.Metadata["date"])
	require.Nil(t, manifest.Metadata["nothing"])
	require.Equal(t, map[string]any{
		"list": []any{"one", json.Number("2"), false, nil},
	}, manifest.Metadata["nested"])
	encoded, err := json.Marshal(manifest.Metadata)
	require.NoError(t, err)
	require.JSONEq(t, `{
      "string": "value",
      "bool": true,
      "integer": 123456789012345678901234567890,
      "float": 1.2300,
      "date": "2026-07-15T00:00:00Z",
      "nothing": null,
      "nested": {"list": ["one", 2, false, null]}
    }`, string(encoded))
}

func TestParseSkillManifestCanonicalHashPreimage(t *testing.T) {
	t.Parallel()

	content := "---\nname: preimage\ndescription: Hash preimage.\n---\n"

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	fileDigest := sha256.Sum256([]byte(manifest.canonicalContent))
	preimage := append([]byte("skill-manifest-v1\x00SKILL.md\x00"), fileDigest[:]...)
	preimage = append(preimage, 0)
	digest := sha256.Sum256(preimage)
	require.Equal(t, hex.EncodeToString(digest[:]), manifest.CanonicalSHA256)
}
