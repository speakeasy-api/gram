package access

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// captureLoopsClient records every SendTransactional call. Thread-safe.
type captureLoopsClient struct {
	mu   sync.Mutex
	sent []loops.SendTransactionalInput
}

func (c *captureLoopsClient) SendTransactional(_ context.Context, input loops.SendTransactionalInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, input)
	return nil
}

func (c *captureLoopsClient) Sent() []loops.SendTransactionalInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]loops.SendTransactionalInput, len(c.sent))
	copy(out, c.sent)
	return out
}

type retryOnceLoopsClient struct {
	mu       sync.Mutex
	attempts int
	sent     []loops.SendTransactionalInput
}

func (c *retryOnceLoopsClient) SendTransactional(_ context.Context, input loops.SendTransactionalInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.attempts++
	if c.attempts == 1 {
		return errors.New("temporary send failure")
	}

	c.sent = append(c.sent, input)
	return nil
}

func (c *retryOnceLoopsClient) Attempts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

func (c *retryOnceLoopsClient) Sent() []loops.SendTransactionalInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]loops.SendTransactionalInput, len(c.sent))
	copy(out, c.sent)
	return out
}

func seedGrantWithEffect(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, principal urn.Principal, scope authz.Scope, resource string, effect authz.PolicyEffect) {
	t.Helper()

	selectors, err := authz.NewSelector(scope, resource).MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Effect:         conv.ToPGText(string(effect)),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func TestNotifyAdminsOfNewRequestBestEffort_SendsToOrgAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	// Seed two users: one admin, one non-admin.
	seedConnectedUser(t, ctx, ti.conn, orgID, "admin_user_1", "admin@example.com", "Admin User", "workos_admin_1", "membership_admin_1")
	seedConnectedUser(t, ctx, ti.conn, orgID, "regular_user_1", "regular@example.com", "Regular User", "workos_regular_1", "membership_regular_1")

	// Grant org:admin only to admin_user_1.
	seedGrant(t, ctx, ti.conn, orgID,
		urn.NewPrincipal(urn.PrincipalTypeUser, "admin_user_1"),
		authz.ScopeOrgAdmin,
		orgID,
	)

	project := createShadowMCPProject(t, ctx, ti, orgID)

	captured := &captureLoopsClient{}
	ti.service.emailSvc = email.NewService(testenv.NewLogger(t), captured)
	ti.service.siteURL = *must.Value(url.Parse("https://app.gram.sh"))

	req := accesscontrol.AccessApprovalRequest{
		OrganizationID: orgID,
		RequesterEmail: "requester@example.com",
		DisplayName:    "GitHub MCP Server",
		ProjectID:      project.ID.String(),
	}

	ti.service.notifyAdminsOfNewRequestBestEffort(ctx, req)

	sent := captured.Sent()
	require.Len(t, sent, 1, "exactly one email should be sent: to the org admin")
	assert.Equal(t, "admin@example.com", sent[0].Email)
	assert.Equal(t, "requester@example.com", sent[0].DataVariables["requester_email"])
	assert.Equal(t, "GitHub MCP Server", sent[0].DataVariables["display_name"])
	assert.Equal(t, fmt.Sprintf("https://app.gram.sh/%s/projects/%s/approval-requests", mockidp.MockOrgSlug, project.Slug), sent[0].DataVariables["approval_url"])
}

func TestNotifyAdminsOfNewRequestBestEffort_SendsToRoleAssignedOrgAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	adminRoleID := seedGlobalRole(t, ctx, ti.conn, mockSystemRole("role_admin", "Admin", authz.SystemRoleAdmin))
	seedConnectedUser(t, ctx, ti.conn, orgID, "admin_user_1", "admin@example.com", "Admin User", "workos_admin_1", "membership_admin_1")
	seedConnectedUser(t, ctx, ti.conn, orgID, "regular_user_1", "regular@example.com", "Regular User", "workos_regular_1", "membership_regular_1")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "admin_user_1", mockMember("", "membership_admin_1", "workos_admin_1", authz.SystemRoleAdmin))
	seedGrant(t, ctx, ti.conn, orgID,
		urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+adminRoleID),
		authz.ScopeOrgAdmin,
		orgID,
	)

	captured := &captureLoopsClient{}
	ti.service.emailSvc = email.NewService(testenv.NewLogger(t), captured)

	ti.service.notifyAdminsOfNewRequestBestEffort(ctx, accesscontrol.AccessApprovalRequest{
		OrganizationID: orgID,
		RequesterEmail: "requester@example.com",
		DisplayName:    "GitHub MCP Server",
	})

	sent := captured.Sent()
	require.Len(t, sent, 1, "exactly one email should be sent: to the role-assigned org admin")
	assert.Equal(t, "admin@example.com", sent[0].Email)
}

func TestNotifyAdminsOfNewRequestBestEffort_DoesNotSendWhenOrgAdminGrantIsDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	adminRoleID := seedGlobalRole(t, ctx, ti.conn, mockSystemRole("role_admin", "Admin", authz.SystemRoleAdmin))
	seedConnectedUser(t, ctx, ti.conn, orgID, "admin_user_1", "admin@example.com", "Admin User", "workos_admin_1", "membership_admin_1")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "admin_user_1", mockMember("", "membership_admin_1", "workos_admin_1", authz.SystemRoleAdmin))
	seedGrant(t, ctx, ti.conn, orgID,
		urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+adminRoleID),
		authz.ScopeOrgAdmin,
		orgID,
	)
	seedGrantWithEffect(t, ctx, ti, orgID,
		urn.NewPrincipal(urn.PrincipalTypeUser, "admin_user_1"),
		authz.ScopeOrgAdmin,
		orgID,
		authz.PolicyEffectDeny,
	)

	captured := &captureLoopsClient{}
	ti.service.emailSvc = email.NewService(testenv.NewLogger(t), captured)

	ti.service.notifyAdminsOfNewRequestBestEffort(ctx, accesscontrol.AccessApprovalRequest{
		OrganizationID: orgID,
		RequesterEmail: "requester@example.com",
		DisplayName:    "GitHub MCP Server",
	})

	require.Empty(t, captured.Sent(), "deny grant should remove the user from org admin notification recipients")
}

func TestNotifyAdminsOfNewRequestBestEffort_RetriesTransientEmailFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedConnectedUser(t, ctx, ti.conn, orgID, "admin_user_1", "admin@example.com", "Admin User", "workos_admin_1", "membership_admin_1")
	seedGrant(t, ctx, ti.conn, orgID,
		urn.NewPrincipal(urn.PrincipalTypeUser, "admin_user_1"),
		authz.ScopeOrgAdmin,
		orgID,
	)

	captured := &retryOnceLoopsClient{}
	ti.service.emailSvc = email.NewService(testenv.NewLogger(t), captured)

	ti.service.notifyAdminsOfNewRequestBestEffort(ctx, accesscontrol.AccessApprovalRequest{
		OrganizationID: orgID,
		RequesterEmail: "requester@example.com",
		DisplayName:    "GitHub MCP Server",
	})

	require.Equal(t, 2, captured.Attempts())
	sent := captured.Sent()
	require.Len(t, sent, 1)
	assert.Equal(t, "admin@example.com", sent[0].Email)
}

func TestNotifyAdminsOfNewRequestBestEffort_NoOp_WhenNoAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	captured := &captureLoopsClient{}
	ti.service.emailSvc = email.NewService(testenv.NewLogger(t), captured)
	ti.service.siteURL = *must.Value(url.Parse("https://app.gram.sh"))

	// No admin grants seeded — no emails should be sent.
	ti.service.notifyAdminsOfNewRequestBestEffort(ctx, accesscontrol.AccessApprovalRequest{
		OrganizationID: orgID,
		RequesterEmail: "someone@example.com",
		DisplayName:    "Some Resource",
	})

	require.Empty(t, captured.Sent())
}
