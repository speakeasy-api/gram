//go:build demoseed_safety

package demoseed

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// TestSeedUpstreamAuthReadyAfterReseed seeds each tenant twice and checks that
// the second run, whose project delete cascades away every typed auth row the
// first run recorded, leaves nothing for the admin upstream OAuth migration's
// dry runs to do, and leaves exactly the intended auth permissions.
func TestSeedUpstreamAuthReadyAfterReseed(t *testing.T) {
	t.Parallel()

	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.Marker, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)

			seedLocalPostgres(ctx, t, db, spec)
			seedLocalPostgres(ctx, t, db, spec)

			require.NoError(t, upstreamAuthReadiness(ctx, testenv.NewLogger(t), db, spec.OrgID))

			clients, err := authclientbackfill.Run(ctx, db, authclientbackfill.Options{OrganizationID: spec.OrgID, Apply: false, PageSize: 0})
			require.NoError(t, err)
			require.NotEmpty(t, clients.Results, "the seed records no typed auth clients; readiness is vacuous")
			for _, result := range clients.Results {
				require.Equal(t, authclientbackfill.OutcomeExisting, result.Outcome, "%s auth client for %s", result.Kind, result.SubjectID)
			}

			grants, err := testrepo.New(db).ListDemoSeedAuthGrantsFixture(ctx, spec.OrgID)
			require.NoError(t, err)
			s := &seeder{spec: spec}
			got := make([]string, 0, len(grants))
			for _, g := range grants {
				got = append(got, authGrantLabel(s, g))
			}
			require.ElementsMatch(t, expectedSeedAuthGrants(), got)
		})
	}
}

// expectedSeedAuthGrants lists every mcp:auth and mcp:blocked_auth grant a
// seeded organization holds. Negative fixtures hold none: the suspended
// Support triage and revoked Retired documentation bot agents, the Inference
// summary worker, and the Contractors role, which keeps only its block on the
// shared token. Each agent attachment grant is bound to the agent's owner and
// the attachment's upstream client.
func expectedSeedAuthGrants() []string {
	sharedClients := []string{"Static credential", "Your own GitHub token", "Engineering automation token", "Acme Wiki", "Acme Tracker"}
	var out []string
	shared := func(principal, kind string, clients ...string) {
		for _, client := range clients {
			out = append(out, fmt.Sprintf("%s | mcp:auth | %s | type= | owner= | upstream= | %s", principal, client, kind))
		}
	}
	personal := func(principal string) {
		out = append(out, principal+" | mcp:auth | * | type=remote_session | owner=$represented_user | upstream= | personal_default")
	}
	attachment := func(principal, owner string) {
		out = append(out, fmt.Sprintf("%s | mcp:auth | * | type=remote_session | owner=%s | upstream=attachment-client | attachment", principal, owner))
	}

	for _, role := range []string{"analyst", "collaborator", "environment-manager", "read-only-tools", "temporary-escalation"} {
		shared("role "+role, "shared_client", sharedClients...)
	}
	shared("role engineer", "shared_client", "Static credential", "Your own GitHub token", "Acme Wiki", "Acme Tracker")
	shared("role engineer", "", "Engineering automation token")
	shared("role support-desk", "shared_client", "Static credential")
	out = append(out, "role contractors | mcp:blocked_auth | Engineering automation token | type= | owner= | upstream= | ")
	personal("role automation-agent")
	personal("role read-only-tools")

	personal("user:all")
	shared("user mateo", "shared_client", sharedClients...)
	shared("user priya", "shared_client", "Your own GitHub token", "Engineering automation token")

	personal("agent Identity-bound assistant agent")
	shared("agent Identity-bound assistant agent", "shared_client", sharedClients...)
	for _, agent := range []struct{ name, owner string }{
		{"Release assistant", demoUsers[0].handle},
		{"Release notes assistant", demoUsers[1].handle},
	} {
		personal("agent " + agent.name)
		attachment("agent "+agent.name, agent.owner)
		shared("agent "+agent.name, "shared_client", "Engineering automation token")
	}
	return out
}

// authGrantLabel names a grant by what stays stable across tenants and seed
// runs: agent names, role slugs, user handles and auth client names.
func authGrantLabel(s *seeder, g testrepo.ListDemoSeedAuthGrantsFixtureRow) string {
	principal := g.PrincipalUrn.String()
	switch {
	case g.AgentName != "":
		principal = "agent " + g.AgentName
	case g.RoleSlug != "":
		principal = "role " + g.RoleSlug
	case strings.HasPrefix(principal, "user:"+s.spec.UserPrefix):
		principal = "user " + strings.TrimPrefix(principal, "user:"+s.spec.UserPrefix)
	}
	upstream := g.UpstreamClient
	if upstream == s.det("attachment-client").String() {
		upstream = "attachment-client"
	}
	return fmt.Sprintf("%s | %s | %s | type=%s | owner=%s | upstream=%s | %s",
		principal, g.Scope, g.AuthClient, g.ClientType, strings.TrimPrefix(g.ConnectionOwner, s.spec.UserPrefix), upstream, g.ProvisionKind)
}
