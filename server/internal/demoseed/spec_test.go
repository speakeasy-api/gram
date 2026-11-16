package demoseed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// The seed's notion of the demo org and the server's must be the same string,
// or the demo carve-outs in authz/middleware would apply to a different org
// than the one that holds the data.
func TestDefaultSpecMatchesDemoConstant(t *testing.T) {
	t.Parallel()

	require.Equal(t, constants.DemoOrganizationID, DefaultSpec().OrgID)
}

// The seed must build every tenant identifier from its Spec. A demo
// identifier hardcoded in the seed would write the local or test tenant's rows
// into the demo org's scope — and, for the local seed, hand a developer's
// writable org the demo org's id.
func TestSeedSourceHasNoDemoIdentifiers(t *testing.T) {
	t.Parallel()

	for _, path := range seedSourceFiles(t) {
		code := stripComments(readFile(t, path))
		for _, id := range DefaultSpec().Identifiers() {
			require.NotContains(t, code, id,
				"%s hardcodes the demo identifier %q; build it from the seeder's Spec helpers", path, id)
		}
	}
}

func TestSpecValidateRejectsCollisions(t *testing.T) {
	t.Parallel()

	shared := LocalSpec()
	shared.OrgSlug = DefaultSpec().OrgSlug
	require.ErrorContains(t, shared.Validate(), "shared with the default demo tenant")

	nested := LocalSpec()
	nested.NameSeed = "x-" + DefaultSpec().NameSeed
	require.ErrorContains(t, nested.Validate(), "contains the default identifier")

	// Two families holding one value collapse under Rewrite.
	collapsed := LocalSpec()
	collapsed.GroupPrefix = collapsed.UserPrefix
	require.ErrorContains(t, collapsed.Validate(), "two identifier families")

	require.NoError(t, DefaultSpec().Validate())
}

// seedSourceFiles lists the seed's own code: the Go files that write rows and
// the sqlc query files. Excluded: spec.go, which defines the identifiers;
// demoseed.go, the runner, whose advisory lock name every tenant shares on
// purpose; and tests.
func seedSourceFiles(t *testing.T) []string {
	t.Helper()

	goFiles, err := filepath.Glob("*.go")
	require.NoError(t, err)
	sqlFiles, err := filepath.Glob("queries/*.sql")
	require.NoError(t, err)

	var out []string
	for _, f := range goFiles {
		if f != "spec.go" && f != "demoseed.go" && !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	return append(out, sqlFiles...)
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// stripComments drops // and -- line comments, which document the default
// tenant by name.
func stripComments(code string) string {
	var sb strings.Builder
	for line := range strings.Lines(code) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "--") {
			continue
		}
		sb.WriteString(line)
	}
	return sb.String()
}
