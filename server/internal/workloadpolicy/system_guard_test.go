package workloadpolicy_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Only the fixture changes kind; the public API must never offer that transition.
func guardedIssuerFixture(t *testing.T, kind string) (context.Context, *testInstance, string, string, uuid.UUID) {
	t.Helper()
	ctx, ti := newTestService(t)
	ctx = asAPIKey(t, ctx)
	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		Name: "Guarded issuer", Issuer: anthropicIssuer, JwksURI: anthropicJWKS,
		AllowWildcardAdmission: new(false), ProjectScoped: true,
	})
	require.NoError(t, err)
	issuerID := policy.Issuers[0].ID
	agentID := newAgent(t, ctx, ti, "original-agent")
	replacement := newAgent(t, ctx, ti, "replacement-agent")
	policy, err = ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		Issuer: anthropicIssuer, Subject: channelOne, MatchKind: "exact",
		Name: new("Original subject"), Tags: []string{"original"}, AgentID: agentID.String(), ProjectScoped: true,
	})
	require.NoError(t, err)
	admissionID := onlyAdmission(t, policy).ID
	_, err = ti.conn.Exec(ctx, `UPDATE workload_issuers SET issuer_kind = $2, jwks_uri = CASE WHEN $2 = 'system' THEN '' ELSE jwks_uri END WHERE id = $1`, issuerID, kind) //nolint:glint // notestingrawsql: deliberately creates kinds that the management API cannot write; no fixture query exists
	require.NoError(t, err)
	return ctx, ti, issuerID, admissionID, replacement
}

func guardedPolicySnapshot(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	var snapshot string
	//nolint:glint // notestingrawsql: whole-row snapshot includes tombstones and timestamps intentionally excluded by public read queries
	err := ti.conn.QueryRow(ctx, `SELECT jsonb_build_object(
 'issuers', (SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM workload_issuers i WHERE organization_id = $1),
 'admissions', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM workload_identity_admissions a WHERE organization_id = $1),
 'assignments', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM workload_agent_assignments a WHERE organization_id = $1)
 )::text`, ti.orgID).Scan(&snapshot)
	require.NoError(t, err)
	return snapshot
}

func TestSystemGuard_GenericMutationsLeavePolicyUnchanged(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"system", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			for _, operation := range []string{"update_issuer", "withdraw_issuer", "admit_subject", "repoint_assignment", "update_metadata", "withdraw_subject"} {
				t.Run(operation, func(t *testing.T) {
					t.Parallel()
					ctx, ti, issuerID, admissionID, replacement := guardedIssuerFixture(t, kind)
					before := guardedPolicySnapshot(t, ctx, ti)
					var err error
					switch operation {
					case "update_issuer":
						payload := updatePayload(issuerID)
						payload.Name = new("Changed issuer")
						payload.JwksURI = new("https://example.test/replacement-keys")
						payload.Tags = []string{"changed"}
						_, err = ti.service.UpdateIssuer(ctx, payload)
					case "withdraw_issuer":
						_, err = ti.service.WithdrawIssuer(ctx, &gen.WithdrawIssuerPayload{ID: issuerID})
					case "admit_subject":
						_, err = ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
							Issuer: anthropicIssuer, Subject: fleetStem + "new-agent", MatchKind: "exact", AgentID: replacement.String(), ProjectScoped: true,
						})
					case "repoint_assignment":
						payload := updateSubjectPayload(admissionID)
						payload.AgentID = new(replacement.String())
						_, err = ti.service.UpdateSubject(ctx, payload)
					case "update_metadata":
						payload := updateSubjectPayload(admissionID)
						payload.Name = new("Changed subject")
						payload.Tags = []string{"changed"}
						_, err = ti.service.UpdateSubject(ctx, payload)
					case "withdraw_subject":
						_, err = ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{ID: admissionID})
					}
					requireOopsCode(t, err, oops.CodeNotFound)
					require.JSONEq(t, before, guardedPolicySnapshot(t, ctx, ti))
				})
			}
		})
	}
}

func TestSystemGuard_SystemWildcardIsRejectedWithoutWrites(t *testing.T) {
	t.Parallel()
	ctx, ti, _, _, agentID := guardedIssuerFixture(t, "system")
	before := guardedPolicySnapshot(t, ctx, ti)
	_, err := ti.service.AdmitSubject(ctx, &gen.AdmitSubjectPayload{
		Issuer: anthropicIssuer, Subject: fleetRule, MatchKind: "wildcard", AgentID: agentID.String(), ProjectScoped: true,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.JSONEq(t, before, guardedPolicySnapshot(t, ctx, ti))
}

func TestSystemGuard_RemoteWildcardRemainsSupported(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registerAnthropic(t, ctx, ti, true)
	agentID := newAgent(t, ctx, ti, "remote-agent")
	policy, err := admit(t, ctx, ti, fleetRule, "wildcard", agentID)
	require.NoError(t, err)
	require.Equal(t, fleetRule, onlyAdmission(t, policy).Subject)
}
