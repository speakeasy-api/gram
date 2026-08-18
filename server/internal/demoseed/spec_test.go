package demoseed

import (
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

// Production runs the scripts as written; a default rewrite that changed even
// one byte would mean the daily prod run is executing something other than
// what is reviewed in postgres.sql / clickhouse.sql.
func TestDefaultSpecRewriteIsIdentity(t *testing.T) {
	t.Parallel()

	require.Equal(t, postgresSQL, DefaultSpec().Rewrite(postgresSQL))
	require.Equal(t, clickhouseSQL, DefaultSpec().Rewrite(clickhouseSQL))
}

// A retargeted script must not keep ANY default identifier: one left behind
// would write that tenant's rows into the demo org's scope — and, for the
// local seed, would hand a developer's writable org the demo org's id.
func TestLocalSpecRewritesEveryDefaultIdentifier(t *testing.T) {
	t.Parallel()

	spec := LocalSpec()
	require.NoError(t, spec.Validate())

	for _, script := range map[string]string{
		"postgres.sql":   postgresSQL,
		"clickhouse.sql": clickhouseSQL,
	} {
		rewritten := spec.Rewrite(script)
		for _, id := range DefaultSpec().Identifiers() {
			require.NotContains(t, stripComments(rewritten), id,
				"the local seed script still contains the demo identifier %q", id)
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

// stripComments drops -- line comments, which document the default tenant by
// name and would otherwise trip the identifier check.
func stripComments(script string) string {
	var sb strings.Builder
	for line := range strings.Lines(script) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			sb.WriteString(line)
		}
	}
	return sb.String()
}
