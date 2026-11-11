package runtimepolicy

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Stored delegated policies outlive the code that issued them: a credential
// keeps its policy bytes and every replica decodes them again, recomputing
// the implication closure for the policy's version. The fixtures under
// testdata/stored_policies are those bytes, one file per policy version,
// written once when the version was added and never regenerated. If a change
// to the authz model (an Implies edge, a block, an agent-safe version) makes
// one of them stop decoding, the change must be versioned the way
// delegatedScopeClosure versions mcp:connect, not the fixture updated.
//
// Adding a policy version: run
//
//	go test ./internal/agents/runtimepolicy -run StoredPolicyFixtures -args -write-stored-policy-fixture=<version>
//
// which writes the new version's file and refuses to overwrite an existing
// one. Then regenerate the runtime grants golden with -args -update.
var writeStoredPolicyFixture = flag.Int("write-stored-policy-fixture", 0, "write the stored delegated policy fixture for this version, if it does not exist")

const storedPolicyFixtureDir = "testdata/stored_policies"

func storedPolicyFixturePath(version DelegatedPolicyVersion) string {
	return filepath.Join(storedPolicyFixtureDir, fmt.Sprintf("v%d.json", version))
}

func TestStoredPolicyFixtures_DecodeAtEveryVersion(t *testing.T) {
	t.Parallel()

	if *writeStoredPolicyFixture != 0 {
		writeStoredPolicyFixtureFile(t, DelegatedPolicyVersion(*writeStoredPolicyFixture))
	}

	authorized := make(map[string]map[string][]string)
	for version := DelegatedPolicyVersion1; version <= CurrentDelegatedPolicyVersion; version++ {
		raw, err := os.ReadFile(storedPolicyFixturePath(version))
		require.NoError(t, err, "every supported policy version needs a stored fixture; see the comment at the top of this file")

		var policies map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &policies))
		require.NotEmpty(t, policies)

		byName := make(map[string][]string, len(policies))
		for name, stored := range policies {
			var compact bytes.Buffer
			require.NoError(t, json.Compact(&compact, stored))

			policy, err := DecodeDelegatedPolicy(version, compact.Bytes())
			require.NoError(t, err, "v%d %q no longer decodes; version the closure change instead of editing the fixture", version, name)

			encoded, err := EncodeDelegatedPolicy(version, policy)
			require.NoError(t, err, "v%d %q", version, name)
			require.JSONEq(t, compact.String(), string(encoded), "v%d %q no longer re-encodes to its stored bytes", version, name)

			grants := make([]string, 0, len(policy.RuntimeGrants()))
			for _, grant := range policy.RuntimeGrants() {
				grants = append(grants, describeRuntimeGrant(grant))
			}
			slices.Sort(grants)
			byName[name] = grants
		}
		authorized[fmt.Sprintf("v%d", version)] = byName
	}

	data, err := json.Marshal(authorized)
	require.NoError(t, err)
	testenv.RequireYAMLGolden(t, "testdata/stored_policies.runtime_grants.golden.yaml", data)
}

func TestStoredPolicyFixtures_NoneBeyondCurrentVersion(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(storedPolicyFixtureDir)
	require.NoError(t, err)
	for _, entry := range entries {
		var version DelegatedPolicyVersion
		_, err := fmt.Sscanf(entry.Name(), "v%d.json", &version)
		require.NoError(t, err, "unexpected file %s", entry.Name())
		require.LessOrEqual(t, version, CurrentDelegatedPolicyVersion, "fixture %s is for a version this reader does not support", entry.Name())
	}
}

func describeRuntimeGrant(grant authz.Grant) string {
	selector, err := json.Marshal(grant.Selector)
	if err != nil {
		return string(grant.Scope) + " <unencodable selector>"
	}
	return string(grant.Scope) + " " + string(selector)
}

// writeStoredPolicyFixtureFile issues, at version, one policy per scope that is
// agent-runtime-safe at that version, one with a concrete MCP selector, and
// one carrying a retired and an unknown scope, and stores their bytes.
func writeStoredPolicyFixtureFile(t *testing.T, version DelegatedPolicyVersion) {
	t.Helper()

	path := storedPolicyFixturePath(version)
	_, err := os.Stat(path)
	require.ErrorIs(t, err, fs.ErrNotExist, "%s already exists; stored fixtures are never regenerated", path)

	registry := RuntimeScopeRegistryVersion(version)
	out := make(map[string]json.RawMessage)
	issue := func(name string, grants ...authz.Grant) {
		policy, err := NewDelegatedPolicy(version, grants)
		require.NoError(t, err, "issue %q", name)
		raw, err := EncodeDelegatedPolicy(version, policy)
		require.NoError(t, err)
		out[name] = raw
	}
	for scope := range runtimeScopeDefinitions {
		if IsRuntimeScopeSafe(registry, scope) {
			issue(string(scope), authz.NewGrant(scope, authz.WildcardResource))
		}
	}
	if IsRuntimeScopeSafe(registry, authz.ScopeMCPWrite) {
		issue("concrete mcp:write on one tool", authz.NewGrantWithSelector(authz.ScopeMCPWrite, authz.Selector{
			authz.SelectorKeyResourceKind: "mcp",
			authz.SelectorKeyResourceID:   "server-1",
			"project_id":                  "project-1",
			"tool":                        "tool-a",
		}))
	}
	out["retired and unknown entries"] = json.RawMessage(`{"requested":[{"scope":"future:read","selector":{"resource_id":"*","resource_kind":"future"}},{"scope":"mcp_approval:read","selector":{"resource_id":"*","resource_kind":"mcp_approval"}},{"scope":"project:read","selector":{"resource_id":"*","resource_kind":"project"}}],"effective":[{"scope":"future:read","selector":{"resource_id":"*","resource_kind":"future"}},{"scope":"mcp_approval:read","selector":{"resource_id":"*","resource_kind":"mcp_approval"}},{"scope":"project:read","selector":{"resource_id":"*","resource_kind":"project"}}]}`)

	raw, err := json.MarshalIndent(out, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(raw, '\n'), 0o600))
}
