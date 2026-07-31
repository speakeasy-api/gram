package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// apiKeyCtx builds a context authenticated as a first-party project API key:
// no dashboard session and no external-user identity, scoped to ti.projectID.
func apiKeyCtx(t *testing.T, ti *chatTestInstance) context.Context {
	t.Helper()
	authCtx := &contextvalues.AuthContext{
		APIKeyID:             uuid.NewString(),
		APIKeyName:           "test-key",
		APIKeyScopes:         []string{"producer"},
		ProjectID:            &ti.projectID,
		ActiveOrganizationID: ti.orgID,
	}
	return contextvalues.SetAuthContext(t.Context(), authCtx)
}

// TestLoadChat_APIKey_ReadsAnyProjectChat proves a project API key can load a
// chat in its project even when that chat is owned by an external user. API
// keys are first-party project credentials (like the dashboard session), not
// owner-matched end users, so the external-user ownership gate does not apply.
func TestLoadChat_APIKey_ReadsAnyProjectChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)

	// A chat owned by a specific external user, which an API key does not match.
	seedCtx := initSessionCtx(t, ti)
	chatID := seedChat(t, seedCtx, ti, "owner-user", "external-user-123", "api key readable")
	seedNMessages(t, seedCtx, ti, chatID, 3)

	got, err := ti.service.LoadChat(apiKeyCtx(t, ti), loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
}

// TestLoadChat_Session_CannotReadOtherOrgChat keeps the org boundary for
// dashboard users: user A (a session in org A) cannot load a chat that lives in
// org B's project. newTestChatService provisions a distinct org + project per
// instance, so orgA and orgB are genuinely separate organizations.
func TestLoadChat_Session_CannotReadOtherOrgChat(t *testing.T) {
	t.Parallel()
	orgA := newTestChatService(t)
	orgB := newTestChatService(t)

	// A chat that lives in org B.
	chatID := seedChat(t, initSessionCtx(t, orgB), orgB, "owner-user", "", "org B chat")

	// User A's dashboard session (org A) must not read it.
	_, err := orgB.service.LoadChat(initSessionCtx(t, orgA), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// TestLoadChat_APIKey_CannotReadOtherOrgChat keeps the org boundary for API
// keys: a producer key scoped to org A cannot load a chat that lives in org B's
// project, even though a direct API key is otherwise exempt from owner matching.
func TestLoadChat_APIKey_CannotReadOtherOrgChat(t *testing.T) {
	t.Parallel()
	orgA := newTestChatService(t)
	orgB := newTestChatService(t)

	// A chat that lives in org B.
	chatID := seedChat(t, initSessionCtx(t, orgB), orgB, "owner-user", "", "org B chat")

	// API key A (scoped to org A's project) must not read it.
	_, err := orgB.service.LoadChat(apiKeyCtx(t, orgA), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// TestLoadChat_ExternalUserMismatchStillBlocked guards the ownership check for
// genuine external-user callers: they may only read their own sessions, so the
// API-key exemption must not have widened that path.
func TestLoadChat_ExternalUserMismatchStillBlocked(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)

	seedCtx := initSessionCtx(t, ti)
	chatID := seedChat(t, seedCtx, ti, "", "external-user-123", "someone elses chat")
	seedNMessages(t, seedCtx, ti, chatID, 2)

	_, err := ti.service.LoadChat(externalUserCtx(t, ti, "different-external-user"), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// chatSessionTokenCtx mirrors what chatsessions.Manager.Authorize installs for a
// chat-session token minted via an API key: it restores the minting key's
// APIKeyID but carries NO API-key scopes, and is pinned to an external user.
func chatSessionTokenCtx(t *testing.T, ti *chatTestInstance, externalUserID string) context.Context {
	t.Helper()
	authCtx := &contextvalues.AuthContext{
		APIKeyID:             uuid.NewString(), // restored from the JWT claims
		APIKeyScopes:         nil,              // chat-session tokens never carry scopes
		ExternalUserID:       externalUserID,
		ProjectID:            &ti.projectID,
		ActiveOrganizationID: ti.orgID,
	}
	return contextvalues.SetAuthContext(t.Context(), authCtx)
}

// TestLoadChat_ChatSessionTokenStillOwnerMatched is the regression guard for the
// exemption: a chat-session token minted via an API key carries that key's
// APIKeyID, but it is an end-user credential and must NOT gain project-wide read.
// It may only load its own external user's chats.
func TestLoadChat_ChatSessionTokenStillOwnerMatched(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)

	seedCtx := initSessionCtx(t, ti)
	chatID := seedChat(t, seedCtx, ti, "", "external-user-A", "user A chat")
	seedNMessages(t, seedCtx, ti, chatID, 2)

	// A chat-session token for a different external user must be blocked, even
	// though it carries an APIKeyID.
	_, err := ti.service.LoadChat(chatSessionTokenCtx(t, ti, "external-user-B"), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// The same token can still read its own external user's chat.
	got, err := ti.service.LoadChat(chatSessionTokenCtx(t, ti, "external-user-A"), loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
}

// createProjectInSameOrg adds a second project to ti's organization so a single
// org-wide key can be pointed at more than one project.
func createProjectInSameOrg(t *testing.T, ti *chatTestInstance) uuid.UUID {
	t.Helper()
	p, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Second Project",
		Slug:           "chat-alt-" + uuid.NewString()[:8],
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	return p.ID
}

// seedChatInProject seeds a chat plus one message in an arbitrary project of
// ti's org (seedChat/seedNMessages hardcode ti.projectID).
func seedChatInProject(t *testing.T, ti *chatTestInstance, projectID uuid.UUID, title string) uuid.UUID {
	t.Helper()
	r := chatrepo.New(ti.conn)
	chatID, err := r.UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID:             uuid.New(),
		ProjectID:      projectID,
		OrganizationID: ti.orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{},
		Title:          pgtype.Text{String: title, Valid: title != ""},
	})
	require.NoError(t, err)
	_, err = r.SeedChatMessage(t.Context(), chatrepo.SeedChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		CreatedAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	return chatID
}

// apiKeyCtxForProject builds a direct producer-API-key context resolved to a
// specific project, mirroring what auth.checkProjectAccess sets once the
// Gram-Project header selects the project for that request.
func apiKeyCtxForProject(t *testing.T, ti *chatTestInstance, projectID uuid.UUID) context.Context {
	t.Helper()
	authCtx := &contextvalues.AuthContext{
		APIKeyID:             uuid.NewString(),
		APIKeyName:           "test-key",
		APIKeyScopes:         []string{"producer"},
		ProjectID:            &projectID,
		ActiveOrganizationID: ti.orgID,
	}
	return contextvalues.SetAuthContext(t.Context(), authCtx)
}

// TestLoadChat_OrgWideAPIKey_ReadsChatsInAnyProject covers the org-wide key case
// (api_keys.project_id NULL): such a key can read chats in every project of its
// org. auth.checkProjectAccess lets an org-wide key resolve its context to any
// project in the org (see auth.TestAuthorizeOrganizationWideKeyAllowsProjectSlug),
// and chat.load then serves whichever project was selected. Here the same key
// reads a chat in project A and a chat in project B of one org.
func TestLoadChat_OrgWideAPIKey_ReadsChatsInAnyProject(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t) // project A == ti.projectID
	projectB := createProjectInSameOrg(t, ti)

	chatA := seedChatInProject(t, ti, ti.projectID, "project A chat")
	chatB := seedChatInProject(t, ti, projectB, "project B chat")

	// Resolved to project A it reads A's chat...
	_, err := ti.service.LoadChat(apiKeyCtxForProject(t, ti, ti.projectID), loadPayload(chatA.String()))
	require.NoError(t, err)
	// ...and resolved to project B it reads B's chat. One org-wide key, both projects.
	_, err = ti.service.LoadChat(apiKeyCtxForProject(t, ti, projectB), loadPayload(chatB.String()))
	require.NoError(t, err)
}

// TestLoadChat_ProjectBoundAPIKey_ReadsOnlyItsProject covers the project-bound
// key case (api_keys.project_id set): the key can only ever resolve to that one
// project (auth.checkProjectAccess forbids sibling projects, see
// auth.TestAuthorizeProjectBoundKeyRejectsSiblingProjectSlugWithoutRepointing),
// so it reads its own project's chats but not another project's.
func TestLoadChat_ProjectBoundAPIKey_ReadsOnlyItsProject(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t) // bound project A == ti.projectID
	projectB := createProjectInSameOrg(t, ti)

	chatA := seedChatInProject(t, ti, ti.projectID, "bound project chat")
	chatB := seedChatInProject(t, ti, projectB, "other project chat")

	// A key bound to project A is always resolved to A.
	boundCtx := apiKeyCtxForProject(t, ti, ti.projectID)

	// It reads its own project's chat...
	_, err := ti.service.LoadChat(boundCtx, loadPayload(chatA.String()))
	require.NoError(t, err)
	// ...but not a chat in the other project — its context can never point there.
	_, err = ti.service.LoadChat(boundCtx, loadPayload(chatB.String()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}
