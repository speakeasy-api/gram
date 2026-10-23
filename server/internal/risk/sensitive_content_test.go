package risk_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestRiskSensitiveContentAccess(t *testing.T) {
	t.Parallel()
	for _, session := range []string{"ordinary", "validated_support", "workos_impersonation", "demo_support", "demo_workos_impersonation"} {
		t.Run(session, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
			allowed := session == "ordinary" || session == "demo_support" || session == "demo_workos_impersonation"
			if session == "demo_support" || session == "demo_workos_impersonation" {
				orgID = constants.DemoOrganizationID
				require.NoError(t, testrepo.New(ti.conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
					ID: orgID, Name: "Demo fixture", Slug: "demo-sensitive-content", GramAccountType: "enterprise",
					FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
					FreeTrialEndsAt:    pgtype.Timestamptz{Time: time.Now().UTC().Add(24 * time.Hour), Valid: true},
				}))
				project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
					OrganizationID: orgID, Name: "Demo fixture", Slug: "demo-sensitive-content",
				})
				require.NoError(t, err)
				projectID = project.ID
				demoAuth := *authCtx
				demoAuth.ActiveOrganizationID = orgID
				demoAuth.ProjectID = &projectID
				authCtx = &demoAuth
				ctx = contextvalues.SetAuthContext(ctx, authCtx)
			}
			// These are real stored and loaded grants, so a denial cannot be an RBAC failure.
			ctx = withExactAccessGrants(t, ctx, ti.conn,
				authz.NewGrant(authz.ScopeOrgAdmin, orgID),
				authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
			)
			copied := *authCtx
			switch session {
			case "validated_support", "demo_support":
				copied.IsAdmin = true
				copied.SupportOrganizationID = orgID
				ctx = contextvalues.WithValidatedSupportSession(ctx, &copied)
				require.True(t, contextvalues.IsSupportSession(ctx))
			case "workos_impersonation", "demo_workos_impersonation":
				ctx = contextvalues.WithValidatedGramSession(ctx, &copied, true)
				require.True(t, contextvalues.IsLegacyImpersonatedSession(ctx))
			}

			secret := "raw credential"
			repo := riskrepo.New(ti.conn)
			chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
				ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
			})
			require.NoError(t, err)
			msgID, err := repo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
				ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: secret,
				UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
			})
			require.NoError(t, err)
			chatRow := insertUnmaskFinding(t, ti, unmaskFinding{
				orgID: orgID, projectID: projectID.String(), chatID: chatID.String(), chatMessageID: msgID.String(),
				surface: "content", startPos: 0, endPos: int32(len(secret)), matchLen: uint32(len(secret)), matchRedacted: "<redacted>",
			})
			executionID := uuid.NewString()
			payload := `{"token":"raw credential"}`
			mcpRow := insertUnmaskFinding(t, ti, unmaskFinding{
				orgID: orgID, projectID: projectID.String(), chatID: uuid.NewString(),
				mediationSurface: "hosted_mcp", executionID: executionID, phase: "request",
				startPos: 10, endPos: int32(10 + len(secret)), matchLen: uint32(len(secret)), matchRedacted: "<redacted>",
			})
			require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
				OrganizationID: orgID, ProjectID: projectID, CreatedAt: time.Now().UTC(),
				Findings:  []risk.MCPFindingEvidence{{ID: mcpRow, Match: secret}},
				Execution: &risk.MCPExecutionPayload{ExecutionID: executionID, Phase: "request", Payload: payload},
			}))

			for _, surface := range []string{"chat_match", "mcp_match", "full_payload"} {
				t.Run(surface, func(t *testing.T) {
					action := audit.ActionRiskResultUnmask
					if surface == "full_payload" {
						action = audit.ActionRiskResultRevealPayload
					}
					before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, action)
					require.NoError(t, err)
					if surface == "full_payload" {
						res, err := ti.service.RevealRiskResultPayload(ctx, &gen.RevealRiskResultPayloadPayload{ID: mcpRow.String()})
						if allowed {
							require.NoError(t, err)
							require.Equal(t, "available", res.RevealState)
							require.Equal(t, payload, res.Payload)
						} else {
							var forbidden *oops.ShareableError
							require.ErrorAs(t, err, &forbidden)
							require.Equal(t, oops.CodeForbidden, forbidden.Code)
							require.Nil(t, res)
						}
					} else {
						id := chatRow
						if surface == "mcp_match" {
							id = mcpRow
						}
						res, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: id.String()})
						if allowed {
							require.NoError(t, err)
							require.Equal(t, "available", res.RevealState)
							require.Equal(t, secret, res.Match)
						} else {
							var forbidden *oops.ShareableError
							require.ErrorAs(t, err, &forbidden)
							require.Equal(t, oops.CodeForbidden, forbidden.Code)
							require.Nil(t, res)
						}
					}
					after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, action)
					require.NoError(t, err)
					if allowed {
						require.Equal(t, before+1, after)
					} else {
						require.Equal(t, before, after, "denied reveals must not emit success audits")
					}
				})
			}
		})
	}
}

func TestRiskListingSessionTitles(t *testing.T) {
	t.Parallel()
	for _, clickhouse := range []bool{false, true} {
		t.Run(fmt.Sprintf("clickhouse=%t", clickhouse), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			authCtx, _ := contextvalues.GetAuthContext(ctx)
			projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
			ctx = withExactAccessGrants(t, ctx, ti.conn,
				authz.NewGrant(authz.ScopeOrgAdmin, orgID),
				authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
			)
			policyID := seedUnmaskPolicy(t, ti, projectID, orgID, true)
			chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "")
			seedRiskResult(t, ti, projectID, orgID, policyID, 1, msgID, true)
			insertUnmaskFinding(t, ti, unmaskFinding{
				orgID: orgID, projectID: projectID.String(), chatID: chatID.String(), chatMessageID: msgID.String(),
				riskPolicyID: policyID.String(), matchLen: 12, matchRedacted: "<redacted>",
			})
			if clickhouse {
				ti.flags.SetFlag(feature.FlagRiskFindingsClickHouseCutover, orgID, true)
			}
			for _, session := range []string{"ordinary", "support", "workos"} {
				t.Run(session, func(t *testing.T) {
					sessionCtx := ctx
					expected := "test chat"
					copied := *authCtx
					switch session {
					case "support":
						copied.IsAdmin = true
						copied.SupportOrganizationID = orgID
						sessionCtx = contextvalues.WithValidatedSupportSession(ctx, &copied)
						expected = "Session " + chatID.String()[:8]
					case "workos":
						sessionCtx = contextvalues.WithValidatedGramSession(ctx, &copied, true)
						expected = "Session " + chatID.String()[:8]
					}
					for _, filterChat := range []bool{false, true} {
						var chatFilter *string
						if filterChat {
							id := chatID.String()
							chatFilter = &id
						}
						results, err := ti.service.ListRiskResults(sessionCtx, &gen.ListRiskResultsPayload{ChatID: chatFilter})
						require.NoError(t, err)
						require.Len(t, results.Results, 1)
						if session != "ordinary" {
							require.Nil(t, results.Results[0].Match, "chat:read must not bypass impersonation protection")
							require.Empty(t, results.Results[0].Spans)
						}
						require.NotNil(t, results.Results[0].ChatTitle)
						require.Equal(t, expected, *results.Results[0].ChatTitle)
						agentResults, err := ti.service.ListRiskResultsForAgent(sessionCtx, &gen.ListRiskResultsForAgentPayload{ChatID: chatFilter})
						require.NoError(t, err)
						require.Len(t, agentResults.Results, 1)
						require.NotNil(t, agentResults.Results[0].ChatTitle)
						require.Equal(t, expected, *agentResults.Results[0].ChatTitle)
					}
					chats, err := ti.service.ListRiskResultsByChat(sessionCtx, &gen.ListRiskResultsByChatPayload{})
					require.NoError(t, err)
					require.Len(t, chats.Chats, 1)
					require.Equal(t, chatID.String(), chats.Chats[0].ChatID)
					require.NotNil(t, chats.Chats[0].ChatTitle)
					require.Equal(t, expected, *chats.Chats[0].ChatTitle)
				})
			}
		})
	}
}
