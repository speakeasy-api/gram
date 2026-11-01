package catalog_test

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

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
	require.True(t, github.Enabled)
	require.Equal(t, "https://token.actions.githubusercontent.com", github.Issuer.Value)
	require.Equal(t, "https://token.actions.githubusercontent.com/.well-known/jwks", github.JWKSURI.Value)
	require.Equal(t, "repo:{owner}/{repo}:", github.Subject.Template)
	require.True(t, github.Subject.Wildcard)

	patterns := make(map[string]*regexp.Regexp, len(github.Variables))
	for _, variable := range github.Variables {
		require.Equal(t, catalog.TierRule, variable.Tier, "%s varies per repository, not per platform", variable.Key)
		patterns[variable.Key] = regexp.MustCompile("^(?:" + variable.Pattern + ")$")
	}
	require.Len(t, patterns, 2)

	// A value holding a delimiter could reach past its segment: an owner of
	// "acme/app" or a repository of "app:ref" would move the stem, and a "*"
	// would read as a second wildcard.
	for key, pattern := range patterns {
		for _, delimiter := range []string{":", "/", "*", " "} {
			require.False(t, pattern.MatchString("ab"+delimiter+"cd"), "%s admits %q", key, delimiter)
		}
		// GitHub names never contain "@", so it may only introduce an ID.
		require.False(t, pattern.MatchString("ab@cd"), "%s admits a non-numeric ID", key)
		require.False(t, pattern.MatchString("ab@"), "%s admits an empty ID", key)
		require.False(t, pattern.MatchString("ab@1@2"), "%s admits two IDs", key)
	}

	owner, repo := patterns["owner"], patterns["repo"]
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
}

func TestParseAcceptsAValidPlatform(t *testing.T) {
	t.Parallel()

	platform, err := catalog.Parse([]byte(validPlatform))
	require.NoError(t, err)
	require.Equal(t, "example", platform.Key)
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
