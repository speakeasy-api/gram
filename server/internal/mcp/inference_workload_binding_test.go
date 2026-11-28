package mcp_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestWorkloadInference_LiveSessionBinding(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"missing ceiling", "malformed ceiling", "broadened ceiling", "unsupported ceiling version", "subject mismatch", "resource project mismatch", "missing resource", "expired authorization lifetime"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			enableWorkloadInference(t, f)
			token := requireWorkloadInferenceToken(t, f, issuer)
			_, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
			require.NoError(t, err)
			require.True(t, handled)
			sessions := f.workloadSessions(t)
			require.Len(t, sessions, 1)

			var column string
			var value any
			switch mutation {
			case "missing ceiling":
				column, value = "delegated_grants", nil
			case "malformed ceiling":
				column, value = "delegated_grants", []byte(`{"invalid":true}`)
			case "broadened ceiling":
				policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion6, []authz.Grant{authz.NewGrant(authz.ScopeInferenceExecute, authz.WildcardResource)})
				require.NoError(t, err)
				encoded, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion6, policy)
				require.NoError(t, err)
				column, value = "delegated_grants", encoded
			case "unsupported ceiling version":
				column, value = "delegated_grants_version", int32(999)
			case "subject mismatch":
				column, value = "subject_urn", urn.NewWorkloadSubject(f.issuerID, "different-subject").String()
			case "resource project mismatch":
				column, value = "resource", mcp.InferenceResource+":"+uuid.NewString()
			case "missing resource":
				column, value = "resource", nil
			case "expired authorization lifetime":
				column, value = "refresh_expires_at", sessions[0].CreatedAt.Time.AddDate(-1, 0, 0)
			}
			// Only a fixed test-case column is interpolated; bind every row identifier and value.
			result, err := f.ti.conn.Exec( //nolint:glint // notestingrawsql: corrupt immutable session bindings to verify the live fail-closed boundary
				t.Context(), "UPDATE user_sessions SET "+column+" = $1 WHERE id = $2 AND organization_id = $3 AND user_session_issuer_id = $4", value, sessions[0].ID, f.fx.orgID, f.fx.target.UserSessionIssuerID)
			require.NoError(t, err)
			require.Equal(t, int64(1), result.RowsAffected())
			_, handled, err = f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
			require.True(t, handled, "recognized inference credentials must not fall back")
			require.Error(t, err, "an issued token must not override its live session binding")
		})
	}
}

func TestWorkloadInference_LiveOwnerAndSharedIssuer(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"owner membership removed", "owner reassignment required", "shared issuer disabled", "shared issuer deleted"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			enableWorkloadInference(t, f)
			token := requireWorkloadInferenceToken(t, f, issuer)
			_, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
			require.NoError(t, err)
			require.True(t, handled)
			switch mutation {
			case "owner membership removed":
				err := orgrepo.New(f.ti.conn).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: f.fx.orgID, UserID: conv.ToPGText(f.fx.userID)})
				require.NoError(t, err)
			case "owner reassignment required":
				_, err := agentsrepo.New(f.ti.conn).LatchAgentsForOwnerLossByMembership(t.Context(), agentsrepo.LatchAgentsForOwnerLossByMembershipParams{OrganizationID: f.fx.orgID, OwnerUserID: f.fx.userID, OwnerReassignmentReason: pgtype.Text{String: "membership_loss", Valid: true}})
				require.NoError(t, err)
			case "shared issuer disabled":
				setIssuerMode(t, f.fixtureContext(t), f.ti, f.fx.target.UserSessionIssuerID, "endpoint", "")
			case "shared issuer deleted":
				// Issuers can only be deleted after their live endpoint owners are removed.
				servers := mcpserversrepo.New(f.ti.conn)
				ids, err := servers.ListMCPServerIDsByUserSessionIssuerID(t.Context(), mcpserversrepo.ListMCPServerIDsByUserSessionIssuerIDParams{ProjectID: f.fx.target.ProjectID, UserSessionIssuerID: uuid.NullUUID{UUID: f.fx.target.UserSessionIssuerID, Valid: true}})
				require.NoError(t, err)
				for _, id := range ids {
					_, err := servers.DeleteMCPServer(t.Context(), mcpserversrepo.DeleteMCPServerParams{ID: id, ProjectID: f.fx.target.ProjectID})
					require.NoError(t, err)
				}
				_, err = toolsetsrepo.New(f.ti.conn).DeleteToolset(t.Context(), toolsetsrepo.DeleteToolsetParams{Slug: f.fx.toolset.Slug, ProjectID: f.fx.target.ProjectID})
				require.NoError(t, err)
				_, handled, err = f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
				require.NoError(t, err, "removing MCP endpoints must not revoke inference")
				require.True(t, handled)
				_, err = usersessionsrepo.New(f.ti.conn).DeleteUserSessionIssuer(t.Context(), usersessionsrepo.DeleteUserSessionIssuerParams{ID: f.fx.target.UserSessionIssuerID, ProjectID: f.fx.target.ProjectID})
				require.NoError(t, err)
			}
			_, handled, err = f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
			require.True(t, handled)
			require.Error(t, err, "an issued token must observe live owner and issuer eligibility")
		})
	}
}

func TestWorkloadInference_CrossResourceReplay(t *testing.T) {
	t.Parallel()
	for _, inferenceFirst := range []bool{true, false} {
		name := "MCP then inference"
		if inferenceFirst {
			name = "inference then MCP"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			enableWorkloadInference(t, f)
			assertion := f.assertion(t, issuer)
			resources := []string{f.resource, mcp.InferenceResource}
			if inferenceFirst {
				resources = []string{mcp.InferenceResource, f.resource}
			}
			for i, resource := range resources {
				w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), url.Values{
					"grant_type": {workloadGrantJWTBearer}, "assertion": {assertion}, "resource": {resource}, "project_id": {f.fx.target.ProjectID.String()},
				})
				if i == 0 {
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				} else {
					requireWorkloadGrantRefused(t, w)
				}
			}
			require.Len(t, f.workloadSessions(t), 1, "changing resource must not mint a second session from a replayed assertion")
		})
	}
}
