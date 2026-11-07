package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// challengeScopeFixture is a session caller granted mcp:connect on one server
// only, with challenge logging on.
func challengeScopeFixture(t *testing.T) (context.Context, *Engine, *pgxpool.Pool, string) {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	sessionID := "session_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_scope",
		SessionID:            &sessionID,
		AccountType:          "enterprise",
	})
	granted := uuid.NewString()
	ctx = GrantsToContext(ctx, []Grant{
		{PrincipalUrn: "user:user_scope", Scope: ScopeMCPConnect, Selector: NewSelector(ScopeMCPConnect, granted)},
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	return ctx, NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient()), conn, granted
}

// recordedChallenges reads back the resource and outcome of every recorded
// check, in order.
func recordedChallengeOutcomes(t *testing.T, conn *pgxpool.Pool) [][2]string {
	t.Helper()
	var outcomes [][2]string
	for _, row := range listChallengeOutboxRows(t, conn) {
		message := &authzv1.Challenge{}
		require.NoError(t, proto.Unmarshal(row.Message, message))
		outcomes = append(outcomes, [2]string{message.GetResourceId(), message.GetOutcome()})
	}
	return outcomes
}

func TestWithOneChallengePerResource_recordsEachResourceOnce(t *testing.T) {
	t.Parallel()

	ctx, engine, conn, granted := challengeScopeFixture(t)
	ctx = WithOneChallengePerResource(ctx)
	ungranted := uuid.NewString()

	// The server, then a tool on it: the second allow on the same server is
	// not recorded again.
	require.NoError(t, engine.Require(ctx, MCPCheck(ScopeMCPConnect, granted, "")))
	require.NoError(t, engine.Require(ctx, MCPToolCallCheck(granted, MCPToolCallDimensions{Tool: "search", Disposition: "", ProjectID: ""})))
	// A denial is recorded once however often it is checked.
	require.Error(t, engine.Require(ctx, MCPCheck(ScopeMCPConnect, ungranted, "")))
	require.Error(t, engine.Require(ctx, MCPCheck(ScopeMCPConnect, ungranted, "")))
	// A nested call keeps the request's record rather than starting over.
	require.NoError(t, engine.Require(WithOneChallengePerResource(ctx), MCPCheck(ScopeMCPConnect, granted, "")))

	require.Equal(t, [][2]string{{granted, "allow"}, {ungranted, "deny"}}, recordedChallengeOutcomes(t, conn))
}

func TestWithOneChallengePerResource_recordsADenialAfterAnAllow(t *testing.T) {
	t.Parallel()

	ctx, engine, conn, granted := challengeScopeFixture(t)
	ctx = GrantsToContext(ctx, []Grant{{
		PrincipalUrn: "user:user_scope",
		Scope:        ScopeMCPConnect,
		Selector:     Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: granted, SelectorKeyDisposition: DispositionReadOnly},
	}})
	ctx = WithOneChallengePerResource(ctx)

	// Connecting is allowed, but this destructive tool is not: the refusal
	// is a real one and must still show.
	require.NoError(t, engine.Require(ctx, MCPCheck(ScopeMCPConnect, granted, "")))
	require.Error(t, engine.Require(ctx, MCPToolCallCheck(granted, MCPToolCallDimensions{Tool: "delete", Disposition: DispositionDestructive, ProjectID: ""})))

	require.Equal(t, [][2]string{{granted, "allow"}, {granted, "deny"}}, recordedChallengeOutcomes(t, conn))
}

func TestWithoutChallenges_recordsNothing(t *testing.T) {
	t.Parallel()

	ctx, engine, conn, granted := challengeScopeFixture(t)
	silent := WithoutChallenges(WithOneChallengePerResource(ctx))

	require.NoError(t, engine.Require(silent, MCPCheck(ScopeMCPConnect, granted, "")))
	require.Error(t, engine.Require(silent, MCPCheck(ScopeMCPConnect, uuid.NewString(), "")))
	matched, err := engine.FindMatched(silent, []Check{MCPCheck(ScopeMCPConnect, granted, "")})
	require.NoError(t, err)
	require.Equal(t, []bool{true}, matched)

	require.Empty(t, listChallengeOutboxRows(t, conn))
}

func TestWithOneChallengePerResource_batchesDoNotTakeAResourcesSlot(t *testing.T) {
	t.Parallel()

	ctx, engine, conn, _ := challengeScopeFixture(t)
	ctx = WithOneChallengePerResource(ctx)
	ungranted := uuid.NewString()

	// A batch filtering the server's tools records, but leaves the server's
	// own denial to be recorded when a call names it. Once that is recorded,
	// a later batch about the server records nothing.
	_, err := engine.FindMatched(ctx, []Check{MCPToolCallCheck(ungranted, MCPToolCallDimensions{Tool: "search", Disposition: "", ProjectID: ""})})
	require.NoError(t, err)
	require.Error(t, engine.Require(ctx, MCPCheck(ScopeMCPConnect, ungranted, "")))
	_, err = engine.FindMatched(ctx, []Check{MCPToolCallCheck(ungranted, MCPToolCallDimensions{Tool: "search", Disposition: "", ProjectID: ""})})
	require.NoError(t, err)

	// A batch row names no single resource.
	require.Equal(t, [][2]string{{"", "deny"}, {ungranted, "deny"}}, recordedChallengeOutcomes(t, conn))
}
