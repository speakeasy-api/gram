package associationbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

func (f *fixture) enforce() {
	f.t.Helper()
	_, err := productfeaturesrepo.New(f.pool).EnableFeature(f.t.Context(), productfeaturesrepo.EnableFeatureParams{
		OrganizationID: f.orgID,
		FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
	})
	require.NoError(f.t, err)
}

// An organization that enforces typed auth clients offers only what was
// configured for a server, so the backfill reports what it would infer from
// the issuer and writes none of it, as reconcile does.
func TestRunWritesNothingInferredUnderEnforcement(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.enforce()

	soleIssuer := f.userIssuer()
	sole := f.server(soleIssuer, "https://sole.example.com/mcp")
	f.client(soleIssuer, "https://as-sole.example.com")

	gatewayIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	memberClient, _ := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, memberClient, "https://member.example.com/mcp")
	f.gateway(gatewayIssuer, member)

	// A server whose evidence names two clients would be a gap, but under
	// enforcement its offers decide, so it is not one.
	sharedIssuer := f.userIssuer()
	ambiguous := f.server(sharedIssuer, "https://ambiguous.example.com/mcp")
	f.client(sharedIssuer, "https://as-one.example.com")
	f.client(sharedIssuer, "https://as-two.example.com")
	f.protectedResource("https://ambiguous.example.com/mcp", "https://as-one.example.com", "https://as-two.example.com")
	f.server(sharedIssuer, "https://sibling.example.com/mcp")

	applied := f.run(true)
	soleResult := resultFor(t, applied, sole)
	require.Equal(t, OutcomeSoleConsumer, soleResult.Outcome, "the decision is still reported")
	require.True(t, soleResult.Enforced)
	require.Zero(t, soleResult.Written)
	require.True(t, gatewayResultFor(t, applied, member).Enforced)
	require.Equal(t, OutcomeAmbiguous, resultFor(t, applied, ambiguous).Outcome)
	require.Zero(t, applied.Written)
	require.Zero(t, applied.Gaps)
	require.Equal(t, applied.Decided, applied.Enforced)
	require.Empty(t, f.associations(sole))
	require.Empty(t, f.associations(member))
}

// Enforcement is read per organization, so an organization without it is
// still backfilled in the same run.
func TestRunBackfillsAnOrganizationWithoutEnforcementBesideOneWithIt(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	enforced := newFixtureIn(t, f.pool)
	enforced.enforce()

	issuer := f.userIssuer()
	server := f.server(issuer, "https://open.example.com/mcp")
	client := f.client(issuer, "https://as-open.example.com")
	enforcedIssuer := enforced.userIssuer()
	enforcedServer := enforced.server(enforcedIssuer, "https://enforced.example.com/mcp")
	enforced.client(enforcedIssuer, "https://as-enforced.example.com")

	report, err := Run(t.Context(), f.pool, Options{OrganizationID: "", Apply: true, PageSize: 1})
	require.NoError(t, err)
	require.False(t, resultFor(t, report, server).Enforced)
	require.True(t, resultFor(t, report, enforcedServer).Enforced)
	require.Equal(t, []uuid.UUID{client}, f.associations(server))
	require.Empty(t, enforced.associations(enforcedServer))
}
