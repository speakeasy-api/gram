package catalog_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
)

const validPlatform = `
key: example
display_name: Example
description: An example platform.
icon: /access-hub/example.svg
enabled: true
issuer:
  value: https://issuer.example.com
  visibility: hidden
jwks_uri:
  value: https://issuer.example.com/jwks.json
  visibility: read-only
variables:
  - key: org_id
    tier: rule
    label: Organization ID
    pattern: "[a-z0-9-]+"
subject:
  template: "wimse://issuer.example.com/org/{org_id}/agent/"
  wildcard: true
`

func TestEmbeddedPlatformsAreValid(t *testing.T) {
	t.Parallel()

	platforms, err := catalog.Embedded().Platforms(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, platforms)
}

// embeddedPlatform is the embedded catalog's entry for key; the test fails
// when there is none.
func embeddedPlatform(t *testing.T, key string) *catalog.Platform {
	t.Helper()

	platforms, err := catalog.Embedded().Platforms(t.Context())
	require.NoError(t, err)
	for i := range platforms {
		if platforms[i].Key == key {
			return &platforms[i]
		}
	}
	require.FailNow(t, "no embedded platform", key)
	return nil
}

func TestEmbeddedClaudeTagPinsOneOrganization(t *testing.T) {
	t.Parallel()

	claudeTag := embeddedPlatform(t, "claude-tag")
	require.Equal(t, "https://identity.anthropic.com/agents", claudeTag.Issuer.Value)
	require.Equal(t, catalog.VisibilityHidden, claudeTag.Issuer.Visibility)
	require.Equal(t, "wimse://identity.anthropic.com/org/{org_id}/agent/", claudeTag.Subject.Template)
	require.True(t, claudeTag.Subject.Wildcard)
}

func TestEmbeddedGKEPinsOneClusterAndServiceAccount(t *testing.T) {
	t.Parallel()

	gke := embeddedPlatform(t, "gke")
	cluster := "https://container.googleapis.com/v1/projects/{project}/locations/{location}/clusters/{cluster}"
	require.Equal(t, cluster, gke.Issuer.Value)
	require.Equal(t, cluster+"/jwks", gke.JWKSURI.Value)
	require.Equal(t, "system:serviceaccount:{namespace}:{service_account}", gke.Subject.Template)
	require.False(t, gke.Subject.Wildcard)

	// A value holding a delimiter could reach into a neighboring segment, such
	// as a namespace of "prod:admin" naming another namespace's account.
	for _, variable := range gke.Variables {
		pattern := regexp.MustCompile("^(?:" + variable.Pattern + ")$")
		for _, delimiter := range []string{":", "/", "*"} {
			require.False(t, pattern.MatchString("ab"+delimiter+"cd"), "%s admits %q", variable.Key, delimiter)
		}
	}
}

func TestEmbeddedGitHubActionsPinsOneRepository(t *testing.T) {
	t.Parallel()

	github := embeddedPlatform(t, "github-actions")
	// Listed but not offered until a stable speakeasyd release supports
	// GitHub Actions, except to organizations its feature flag is on for.
	// The rest of this test, the API tests that read the catalog, and the
	// dashboard's own fixtures keep the setup covered.
	require.False(t, github.Enabled)
	require.Equal(t, feature.FlagWorkloadPlatformGitHubActions, github.FeatureFlag)
	require.Equal(t, "https://token.actions.githubusercontent.com", github.Issuer.Value)
	require.Equal(t, "https://token.actions.githubusercontent.com/.well-known/jwks", github.JWKSURI.Value)
	require.Equal(t, "repo:{owner}/{repo}:", github.Subject.Template)
	require.True(t, github.Subject.Wildcard)
	require.Equal(t, "Whole repository", github.Subject.Label)
	require.Equal(t, []catalog.SubjectAlternative{
		{Key: "branch", Label: "One branch", Template: "repo:{owner}/{repo}:ref:refs/heads/{branch}", Wildcard: false},
		{Key: "environment", Label: "One environment", Template: "repo:{owner}/{repo}:environment:{environment}", Wildcard: false},
	}, github.Subject.Alternatives)

	patterns := make(map[string]*regexp.Regexp, len(github.Variables))
	for _, variable := range github.Variables {
		require.Equal(t, catalog.TierRule, variable.Tier, "%s varies per repository, not per platform", variable.Key)
		patterns[variable.Key] = regexp.MustCompile("^(?:" + variable.Pattern + ")$")
	}
	require.Len(t, patterns, 4)
	owner, repo, branch, environment := patterns["owner"], patterns["repo"], patterns["branch"], patterns["environment"]

	// A value holding a delimiter could reach past its segment: an owner of
	// "acme/app" or a repository of "app:ref" would move the stem, and a "*"
	// would read as a second wildcard.
	for key, pattern := range map[string]*regexp.Regexp{"owner": owner, "repo": repo} {
		for _, delimiter := range []string{":", "/", "*", " "} {
			require.False(t, pattern.MatchString("ab"+delimiter+"cd"), "%s admits %q", key, delimiter)
		}
		// GitHub names never contain "@", so it may only introduce an ID.
		require.False(t, pattern.MatchString("ab@cd"), "%s admits a non-numeric ID", key)
		require.False(t, pattern.MatchString("ab@"), "%s admits an empty ID", key)
		require.False(t, pattern.MatchString("ab@1@2"), "%s admits two IDs", key)
	}

	for _, valid := range []string{"octo-org", "Octo-Org", "a", "octo-org@123456", strings.Repeat("a", 39)} {
		require.True(t, owner.MatchString(valid), "owner refuses %q", valid)
	}
	for _, invalid := range []string{"", "-octo", "octo_org", "octo.org", strings.Repeat("a", 40)} {
		require.False(t, owner.MatchString(invalid), "owner admits %q", invalid)
	}
	for _, valid := range []string{"octo-repo", "Octo.Repo_2", ".github", "octo-repo@456789", strings.Repeat("a", 100)} {
		require.True(t, repo.MatchString(valid), "repository refuses %q", valid)
	}
	for _, invalid := range []string{"", strings.Repeat("a", 101)} {
		require.False(t, repo.MatchString(invalid), "repository admits %q", invalid)
	}

	// A branch and an environment end an exact rule, so a "/" cannot reach
	// another segment, but a ":" would forge a context and a "*" a wildcard.
	for key, pattern := range map[string]*regexp.Regexp{"branch": branch, "environment": environment} {
		for _, refused := range []string{"", "main:x", "main*", "release/*", "main\n", "main\x00"} {
			require.False(t, pattern.MatchString(refused), "%s admits %q", key, refused)
		}
	}
	for _, valid := range []string{"main", "release/v2.1", "renovate/@types-node-20.x", "feat+1", "!main", "Feature_X"} {
		require.True(t, branch.MatchString(valid), "branch refuses %q", valid)
	}
	// Git refuses these in a branch name, so a rule naming one admits nothing.
	for _, invalid := range []string{"/main", "main/", "a//b", "feat?", "[x]", "a\\b", "a^b", "a~1", "my branch"} {
		require.False(t, branch.MatchString(invalid), "branch admits %q", invalid)
	}
	for _, valid := range []string{"production", "Production EU", "review/pr-12", "Production%3AV1", strings.Repeat("e", 255)} {
		require.True(t, environment.MatchString(valid), "environment refuses %q", valid)
	}
	require.False(t, environment.MatchString(strings.Repeat("e", 256)), "environment admits 256 characters")
}

func TestParseAcceptsAValidPlatform(t *testing.T) {
	t.Parallel()

	platform, err := catalog.Parse([]byte(validPlatform))
	require.NoError(t, err)
	require.Equal(t, "example", platform.Key)
}

func TestParseAcceptsAFeatureFlagOnAnEntryNotEnabled(t *testing.T) {
	t.Parallel()

	platform, err := catalog.Parse([]byte(strings.Replace(validPlatform, "enabled: true", "enabled: false\nfeature_flag: "+string(feature.FlagWorkloadPlatformGitHubActions), 1)))
	require.NoError(t, err)
	require.False(t, platform.Enabled)
	require.Equal(t, feature.FlagWorkloadPlatformGitHubActions, platform.FeatureFlag)
}

func TestParseRefusesInvalidPlatforms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    string
		to      string
		wantErr string
	}{
		{name: "unknown field", from: "enabled: true", to: "enabled: true\nenabeld: true", wantErr: "enabeld"},
		{name: "setup block", from: "enabled: true", to: "enabled: true\nsetup:\n  steps: []", wantErr: "setup"},
		{name: "http issuer", from: "value: https://issuer.example.com\n", to: "value: http://issuer.example.com\n", wantErr: "https"},
		{name: "bad visibility", from: "visibility: hidden", to: "visibility: secret", wantErr: "visibility"},
		{name: "undeclared variable", from: "{org_id}/agent/", to: "{tenant}/agent/", wantErr: "undeclared variable"},
		{name: "wildcard stem not on a delimiter", from: "{org_id}/agent/\"", to: "{org_id}/agent\"", wantErr: "wildcard stem"},
		{name: "star in template", from: "{org_id}/agent/\"", to: "{org_id}/agent/*\"", wantErr: `"*"`},
		{name: "missing pattern", from: "    pattern: \"[a-z0-9-]+\"\n", to: "", wantErr: "pattern is required"},
		{name: "rule variable in issuer", from: "value: https://issuer.example.com\n", to: "value: https://{org_id}.issuer.example.com\n", wantErr: "platform-tier"},
		{name: "external icon", from: "icon: /access-hub/example.svg", to: "icon: https://evil.example.com/x.svg", wantErr: "dashboard's origin"},
		{name: "tab-split icon", from: "icon: /access-hub/example.svg", to: "icon: \"/\\t/evil.example.com/x.svg\"", wantErr: "dashboard's origin"},
		{name: "issuer with a query", from: "value: https://issuer.example.com\n", to: "value: https://issuer.example.com?tenant=x\n", wantErr: "no userinfo, query or fragment"},
		{name: "jwks_uri with a fragment", from: "value: https://issuer.example.com/jwks.json", to: "value: https://issuer.example.com/jwks.json#keys", wantErr: "no userinfo, query or fragment"},
		{name: "unclosed placeholder", from: "{org_id}/agent/", to: "{org_id/agent/", wantErr: "brace"},
		{name: "named group in pattern", from: `pattern: "[a-z0-9-]+"`, to: `pattern: "(?P<id>[a-z0-9-]+)"`, wantErr: "(?:...) groups"},
		{name: "flag group in pattern", from: `pattern: "[a-z0-9-]+"`, to: `pattern: "(?i)[a-z0-9-]+"`, wantErr: "(?:...) groups"},
		{name: "unicode class in pattern", from: `pattern: "[a-z0-9-]+"`, to: `pattern: "\\pL+"`, wantErr: "Go and JavaScript"},
		{name: "posix class in pattern", from: `pattern: "[a-z0-9-]+"`, to: `pattern: "[[:alnum:]-]+"`, wantErr: "POSIX"},
		{name: "feature flag on an enabled entry", from: "enabled: true", to: "enabled: true\nfeature_flag: " + string(feature.FlagWorkloadPlatformGitHubActions), wantErr: "only for an entry that is not enabled"},
		{name: "undeclared feature flag", from: "enabled: true", to: "enabled: false\nfeature_flag: workload-platform-example", wantErr: "not a workload platform flag"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, validPlatform, tc.from, "test case must change the fixture")
			_, err := catalog.Parse([]byte(strings.Replace(validPlatform, tc.from, tc.to, 1)))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// withAlternatives is validPlatform offering an exact rule for one agent
// beside its wildcard over the whole organization.
var withAlternatives = strings.NewReplacer(
	"    pattern: \"[a-z0-9-]+\"\n", `    pattern: "[a-z0-9-]+"
  - key: agent_id
    tier: rule
    label: Agent ID
    pattern: "[a-z0-9]+"
`,
	"  wildcard: true\n", `  wildcard: true
  label: Every agent
  alternatives:
    - key: one_agent
      label: One agent
      template: "wimse://issuer.example.com/org/{org_id}/agent/{agent_id}"
      wildcard: false
`,
).Replace(validPlatform)

// extraAlternative replaces the end of withAlternatives' one alternative with
// that end followed by another exact alternative.
func extraAlternative(key, template string) string {
	return fmt.Sprintf("      wildcard: false\n    - key: %s\n      label: %s\n      template: %q\n      wildcard: false\n", key, key, template)
}

func TestParseAcceptsSubjectAlternatives(t *testing.T) {
	t.Parallel()

	platform, err := catalog.Parse([]byte(withAlternatives))
	require.NoError(t, err)
	require.Equal(t, "Every agent", platform.Subject.Label)
	require.Equal(t, []catalog.SubjectAlternative{{
		Key:      "one_agent",
		Label:    "One agent",
		Template: "wimse://issuer.example.com/org/{org_id}/agent/{agent_id}",
		Wildcard: false,
	}}, platform.Subject.Alternatives)
}

func TestParseRefusesInvalidSubjectAlternatives(t *testing.T) {
	t.Parallel()

	const alternativeTemplate = `"wimse://issuer.example.com/org/{org_id}/agent/{agent_id}"`
	tests := []struct {
		name    string
		from    string
		to      string
		wantErr string
	}{
		{name: "default without a label", from: "  label: Every agent\n", to: "", wantErr: "subject.label is required"},
		{name: "alternative without a label", from: "      label: One agent\n", to: "", wantErr: `"one_agent": label is required`},
		{name: "alternative key not snake_case", from: "key: one_agent", to: "key: One-Agent", wantErr: "lowercase snake_case"},
		{name: "same rule as the default", from: alternativeTemplate + "\n      wildcard: false", to: "\"wimse://issuer.example.com/org/{org_id}/agent/\"\n      wildcard: true", wantErr: "could match the same subject as the default"},
		{name: "wildcard alternative inside the default", from: alternativeTemplate + "\n      wildcard: false", to: "\"wimse://issuer.example.com/org/{org_id}/agent/{agent_id}/\"\n      wildcard: true", wantErr: "could match the same subject as the default"},
		{name: "alternatives differing only in a variable", from: "      wildcard: false\n", to: extraAlternative("other_agent", "wimse://issuer.example.com/org/{org_id}/agent/{org_id}"), wantErr: `could match the same subject as alternative "one_agent"`},
		{name: "alternative extending another", from: "      wildcard: false\n", to: extraAlternative("agent_run", "wimse://issuer.example.com/org/{org_id}/agent/{agent_id}/run"), wantErr: `could match the same subject as alternative "one_agent"`},
		{name: "star in an alternative", from: alternativeTemplate, to: `"wimse://issuer.example.com/org/{org_id}/agent/*"`, wantErr: `"*"`},
		{name: "wildcard alternative ending on a variable", from: alternativeTemplate + "\n      wildcard: false", to: alternativeTemplate + "\n      wildcard: true", wantErr: "wildcard stem"},
		{name: "undeclared variable in an alternative", from: "{agent_id}\"", to: "{agent}\"", wantErr: "undeclared variable"},
		{name: "alternative declared twice", from: "      wildcard: false\n", to: extraAlternative("one_agent", "wimse://issuer.example.com/org/{org_id}/agents/{agent_id}"), wantErr: "declared twice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, withAlternatives, tc.from, "test case must change the fixture")
			_, err := catalog.Parse([]byte(strings.Replace(withAlternatives, tc.from, tc.to, 1)))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// Two exact rules are told apart by literal text both readings of a subject
// reach at the same place, which holds only while every variable before it
// refuses the character that ends it.
func TestParseTellsAlternativesApartByTextNoVariableReaches(t *testing.T) {
	t.Parallel()

	team := strings.Replace(withAlternatives, "      wildcard: false\n", extraAlternative("one_team", "wimse://issuer.example.com/org/{org_id}/team/{agent_id}"), 1)
	_, err := catalog.Parse([]byte(team))
	require.NoError(t, err, `organization IDs refuse "/", so "/agent/" and "/team/" start at the same place`)

	// An organization ID that may hold "/" could reach past "/team", so the
	// two can no longer be shown apart and the catalog is refused.
	_, err = catalog.Parse([]byte(strings.Replace(team, `pattern: "[a-z0-9-]+"`, `pattern: "[a-z0-9/-]+"`, 1)))
	require.ErrorContains(t, err, `alternative "one_team" could match the same subject as alternative "one_agent"`)
}

func TestLoadRequiresTheFileNameToBeTheKey(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"platforms/other.yaml": {Data: []byte(validPlatform)}}
	_, err := catalog.Load(fsys, "platforms")
	require.ErrorContains(t, err, "file name must be the key")
}

func TestLoadOrdersPlatformsByKey(t *testing.T) {
	t.Parallel()

	second := strings.Replace(validPlatform, "key: example", "key: another", 1)
	fsys := fstest.MapFS{
		"platforms/example.yaml": {Data: []byte(validPlatform)},
		"platforms/another.yaml": {Data: []byte(second)},
	}
	platforms, err := catalog.Load(fsys, "platforms")
	require.NoError(t, err)
	require.Equal(t, "another", platforms[0].Key)
	require.Equal(t, "example", platforms[1].Key)
}
