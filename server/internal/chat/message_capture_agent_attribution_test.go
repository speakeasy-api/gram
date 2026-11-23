package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// withoutHumanIdentity clones the session auth context into the shape a
// machine credential produces: same tenant and project, no user, no session.
func withoutHumanIdentity(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *authCtx
	clone.UserID = ""
	clone.ExternalUserID = ""
	clone.Email = nil
	clone.APIKeyScopes = nil
	clone.SessionID = nil
	return &clone
}

// agentCredentialCtx makes ctx look like a completion authenticated with the
// agent's principal API key: the agent is the authenticated actor and its owner
// is recorded only as the authorizer.
func agentCredentialCtx(t *testing.T, ctx context.Context, agentID uuid.UUID, ownerUserID string) context.Context {
	t.Helper()
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String())
	credential := contextvalues.PrincipalCredential{AuthorizerUserID: ownerUserID, DelegatedGrants: nil, DelegatedGrantsVersion: 0, DerivesPolicyAtAdmission: false}
	return contextvalues.WithPrincipalAPIKeyAuthorization(ctx, withoutHumanIdentity(t, ctx), actor, credential)
}

// workloadForAgentCtx makes ctx look like an admitted workload acting for the
// agent: the workload stays the actor and the agent is recorded on the context.
func workloadForAgentCtx(t *testing.T, ctx context.Context, agentID uuid.UUID) context.Context {
	t.Helper()
	ctx = contextvalues.WithAuthenticatedActor(ctx, withoutHumanIdentity(t, ctx), urn.NewWorkloadPrincipal(uuid.New(), "attribution-workload"))
	return contextvalues.WithWorkloadAgent(ctx, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()))
}

// memberCtxForUser returns a member session for userID holding no chat grants,
// so only authorship or agent ownership can open a session.
func memberCtxForUser(t *testing.T, ti *chatTestInstance, userID string) context.Context {
	t.Helper()
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *authCtx
	clone.UserID = userID
	return authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &clone))
}

func completionRequest(ti *chatTestInstance, chatID uuid.UUID, userID string) openrouter.CompletionRequest {
	req := makeRequest(chatID, ti.projectID, ti.orgID, openrouter.CreateMessageUser("hello from an agent"))
	req.UserID = userID
	return req
}

// A completion made with an agent's credential records the agent on the session
// and no human identity, so the session takes the agent-only shape that lets the
// agent's owner open it without chat:read. Nobody else can.
func TestStartOrResumeChat_AgentCredentialStampsAgentWithoutHumanIdentity(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	strategy := newCaptureStrategy(t, ti.conn)

	agentID := seedAgent(t, ctx, ti, authCtx.UserID, "Completion agent")
	chatID := uuid.New()
	// request.UserID stays empty because the agent credential's auth context
	// carries no human identity; it must land as NULL, not "".
	_, err := strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentID, authCtx.UserID), completionRequest(ti, chatID, ""))
	require.NoError(t, err)

	row := chatRow(t, ti, chatID)
	require.True(t, row.AgentID.Valid, "the acting agent is recorded on the session")
	require.Equal(t, agentID, row.AgentID.UUID)
	require.False(t, row.UserID.Valid, "an agent credential never attributes the session to a human")
	require.False(t, row.ExternalUserID.Valid)

	ownerCtx := authztest.WithExactGrants(t, ctx)
	res, err := ti.service.LoadChat(ownerCtx, loadPayload(chatID.String()))
	require.NoError(t, err, "the agent's owner opens the agent's session without chat:read")
	require.Equal(t, chatID.String(), res.ID)

	otherCtx := memberCtxForUser(t, ti, "other-member-"+uuid.NewString())
	_, err = ti.service.LoadChat(otherCtx, loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A workload admitted for an agent is that agent's activity too.
func TestStartOrResumeChat_WorkloadForAgentStampsAgent(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	strategy := newCaptureStrategy(t, ti.conn)

	agentID := seedAgent(t, ctx, ti, authCtx.UserID, "Workload agent")
	chatID := uuid.New()
	_, err := strategy.StartOrResumeChat(workloadForAgentCtx(t, ctx, agentID), completionRequest(ti, chatID, ""))
	require.NoError(t, err)

	row := chatRow(t, ti, chatID)
	require.True(t, row.AgentID.Valid)
	require.Equal(t, agentID, row.AgentID.UUID)
	require.False(t, row.UserID.Valid)
	require.False(t, row.ExternalUserID.Valid)
}

// Defensive invariant: no authenticator produces an agent actor together with a
// human user today, but if one ever does, the human identity wins. The agent is
// attribution only, and owning the agent does not open the human's session.
func TestStartOrResumeChat_AgentActorKeepsHumanIdentityWhenPresent(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	strategy := newCaptureStrategy(t, ti.conn)

	agentOwner := "agent-owner-" + uuid.NewString()
	agentID := seedAgent(t, ctx, ti, agentOwner, "Agent with a human turn")
	chatID := uuid.New()
	_, err := strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentID, agentOwner), completionRequest(ti, chatID, authCtx.UserID))
	require.NoError(t, err)

	row := chatRow(t, ti, chatID)
	require.True(t, row.AgentID.Valid)
	require.Equal(t, agentID, row.AgentID.UUID)
	require.True(t, row.UserID.Valid)
	require.Equal(t, authCtx.UserID, row.UserID.String)

	_, err = ti.service.LoadChat(authztest.WithExactGrants(t, ctx), loadPayload(chatID.String()))
	require.NoError(t, err, "the human who performed the turn opens it")
	_, err = ti.service.LoadChat(memberCtxForUser(t, ti, agentOwner), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A human completion is untouched: user_id as before, no agent recorded.
func TestStartOrResumeChat_HumanCompletionRecordsNoAgent(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	strategy := newCaptureStrategy(t, ti.conn)

	chatID := uuid.New()
	_, err := strategy.StartOrResumeChat(ctx, completionRequest(ti, chatID, authCtx.UserID))
	require.NoError(t, err)

	row := chatRow(t, ti, chatID)
	require.False(t, row.AgentID.Valid)
	require.True(t, row.UserID.Valid)
	require.Equal(t, authCtx.UserID, row.UserID.String)
}

// A second agent cannot append to another agent's session: the turn is rejected
// as if the chat did not exist, the row is untouched, and only the first
// agent's owner can open it. The same agent resumes its own session freely.
func TestStartOrResumeChat_SecondAgentTurnIsRejected(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	strategy := newCaptureStrategy(t, ti.conn)

	ownerA := "agent-owner-a-" + uuid.NewString()
	ownerB := "agent-owner-b-" + uuid.NewString()
	agentA := seedAgent(t, ctx, ti, ownerA, "Agent A")
	agentB := seedAgent(t, ctx, ti, ownerB, "Agent B")
	chatID := uuid.New()

	_, err := strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentA, ownerA), completionRequest(ti, chatID, ""))
	require.NoError(t, err)
	_, err = strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentB, ownerB), completionRequest(ti, chatID, ""))
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentA, ownerA), completionRequest(ti, chatID, ""))
	require.NoError(t, err, "the first agent resumes its own session")

	row := chatRow(t, ti, chatID)
	require.Equal(t, agentA, row.AgentID.UUID)
	require.False(t, row.UserID.Valid, "the rejected turn does not demote the session")

	_, err = ti.service.LoadChat(memberCtxForUser(t, ti, ownerA), loadPayload(chatID.String()))
	require.NoError(t, err)
	_, err = ti.service.LoadChat(memberCtxForUser(t, ti, ownerB), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A session with no identity at all (NULL user, NULL external user, no agent)
// is the shape assistant-driven chats take. An agent turn on it does not name
// the agent, so the agent's owner never gains the earlier turns.
func TestStartOrResumeChat_AgentTurnDoesNotAdoptExistingSession(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	strategy := newCaptureStrategy(t, ti.conn)

	owner := "agent-owner-" + uuid.NewString()
	agentID := seedAgent(t, ctx, ti, owner, "Agent joining an assistant session")
	chatID := uuid.New()
	_, err := repo.New(ti.conn).UpsertChat(ctx, repo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		UserID:         pgtype.Text{String: "", Valid: false},
		ExternalUserID: pgtype.Text{String: "", Valid: false},
		AgentID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Title:          conv.ToPGText("assistant-shaped session"),
	})
	require.NoError(t, err)

	_, err = strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentID, owner), completionRequest(ti, chatID, ""))
	require.NoError(t, err)

	row := chatRow(t, ti, chatID)
	require.False(t, row.AgentID.Valid, "an existing session is never named after a later agent")
	require.False(t, row.UserID.Valid, "the identity columns are left as they were")

	_, err = ti.service.LoadChat(memberCtxForUser(t, ti, owner), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A session created before NULL attribution existed holds user_id = "". A later
// agent turn leaves both the empty string and the missing agent alone, so the
// session is not retroactively opened to the agent's owner.
func TestStartOrResumeChat_PreExistingEmptyIdentityIsNotNamedOrOpened(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	strategy := newCaptureStrategy(t, ti.conn)

	agentID := seedAgent(t, ctx, ti, authCtx.UserID, "Agent resuming an old session")
	// The capture path wrote "" for an empty user id before this change.
	chatID := uuid.New()
	_, err := repo.New(ti.conn).UpsertChat(ctx, repo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		UserID:         conv.ToPGText(""),
		ExternalUserID: conv.ToPGText(""),
		AgentID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Title:          conv.ToPGText("captured before attribution"),
	})
	require.NoError(t, err)

	_, err = strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentID, authCtx.UserID), completionRequest(ti, chatID, ""))
	require.NoError(t, err)

	after := chatRow(t, ti, chatID)
	require.False(t, after.AgentID.Valid, "the resumed session is not named after its agent")
	require.True(t, after.UserID.Valid, "an existing empty identity is not rewritten")
	require.Empty(t, after.UserID.String)

	_, err = ti.service.LoadChat(authztest.WithExactGrants(t, ctx), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A human turn landing on an agent-only session makes its attribution mixed.
// The session is demoted to the no-author shape: the agent's owner no longer
// opens it by ownership, and the human who wrote into it does not inherit it.
func TestStartOrResumeChat_HumanTurnOnAgentSessionClosesItToBoth(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	strategy := newCaptureStrategy(t, ti.conn)

	agentOwner := "agent-owner-" + uuid.NewString()
	agentID := seedAgent(t, ctx, ti, agentOwner, "Agent whose session a human joins")
	chatID := uuid.New()
	_, err := strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentID, agentOwner), completionRequest(ti, chatID, ""))
	require.NoError(t, err)
	_, err = ti.service.LoadChat(memberCtxForUser(t, ti, agentOwner), loadPayload(chatID.String()))
	require.NoError(t, err, "before the human turn the owner opens the agent-only session")

	_, err = strategy.StartOrResumeChat(ctx, completionRequest(ti, chatID, authCtx.UserID))
	require.NoError(t, err)

	row := chatRow(t, ti, chatID)
	require.Equal(t, agentID, row.AgentID.UUID, "the agent stays recorded")
	require.True(t, row.UserID.Valid)
	require.Empty(t, row.UserID.String, "mixed attribution leaves no author")

	_, err = ti.service.LoadChat(memberCtxForUser(t, ti, agentOwner), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.LoadChat(authztest.WithExactGrants(t, ctx), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A turn with no identity at all (no user, no external user, no agent actor)
// is still a non-agent turn, so it closes the agent-only session the same way a
// named human turn does.
func TestStartOrResumeChat_IdentitylessTurnOnAgentSessionClosesIt(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	strategy := newCaptureStrategy(t, ti.conn)

	agentOwner := "agent-owner-" + uuid.NewString()
	agentID := seedAgent(t, ctx, ti, agentOwner, "Agent whose session an anonymous turn joins")
	chatID := uuid.New()
	_, err := strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentID, agentOwner), completionRequest(ti, chatID, ""))
	require.NoError(t, err)

	anonymous := contextvalues.SetAuthContext(ctx, withoutHumanIdentity(t, ctx))
	_, err = strategy.StartOrResumeChat(anonymous, completionRequest(ti, chatID, ""))
	require.NoError(t, err)

	row := chatRow(t, ti, chatID)
	require.Equal(t, agentID, row.AgentID.UUID, "the agent stays recorded")
	require.True(t, row.UserID.Valid)
	require.Empty(t, row.UserID.String, "mixed attribution leaves no author")

	_, err = ti.service.LoadChat(memberCtxForUser(t, ti, agentOwner), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// An agent from another organization never lands on the chat: the stamp
// resolves the agent against the caller's tenant at write time.
func TestStartOrResumeChat_ForeignTenantAgentIsNotRecorded(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	strategy := newCaptureStrategy(t, ti.conn)

	otherOrg := "other-org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: otherOrg, Name: "Other org", Slug: otherOrg})
	require.NoError(t, err)
	foreignAgent := seedAgentInOrg(t, ctx, ti, otherOrg, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "foreign-owner", "Foreign agent")

	chatID := uuid.New()
	_, err = strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, foreignAgent, authCtx.UserID), completionRequest(ti, chatID, ""))
	require.NoError(t, err)

	row := chatRow(t, ti, chatID)
	require.False(t, row.AgentID.Valid, "a foreign tenant's agent is never recorded")
	require.False(t, row.UserID.Valid)
}

// A deleted agent resolves to no agent at write time: a fresh chat records
// nothing, and its identity-less turn on another agent's session is rejected,
// so the first agent's owner never reads it.
func TestStartOrResumeChat_DeletedAgentIsNotRecordedAndCannotJoin(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	strategy := newCaptureStrategy(t, ti.conn)

	ownerA := "agent-owner-a-" + uuid.NewString()
	ownerD := "agent-owner-d-" + uuid.NewString()
	agentA := seedAgent(t, ctx, ti, ownerA, "Live agent")
	deleted := seedAgent(t, ctx, ti, ownerD, "Deleted agent")
	require.NoError(t, testrepo.New(ti.conn).SoftDeleteAgentFixture(ctx, deleted))

	fresh := uuid.New()
	_, err := strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, deleted, ownerD), completionRequest(ti, fresh, ""))
	require.NoError(t, err)
	require.False(t, chatRow(t, ti, fresh).AgentID.Valid, "a deleted agent is never recorded")

	shared := uuid.New()
	_, err = strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, agentA, ownerA), completionRequest(ti, shared, ""))
	require.NoError(t, err)
	_, err = strategy.StartOrResumeChat(agentCredentialCtx(t, ctx, deleted, ownerD), completionRequest(ti, shared, ""))
	requireOopsCode(t, err, oops.CodeNotFound)

	row := chatRow(t, ti, shared)
	require.Equal(t, agentA, row.AgentID.UUID)
	require.False(t, row.UserID.Valid, "the rejected turn does not demote the session")
}
