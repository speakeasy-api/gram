package agentmanagement

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestDemoListProjectsSyntheticInventoryWithoutManagementCapabilities(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	for _, org := range []string{constants.DemoOrganizationID, "org-customer"} {
		seedOrganization(t, db, org)
		seedOrganizationUser(t, db, org, "synthetic-owner")
		createAgent(t, db, org, "synthetic-owner", "Release assistant")
	}
	agent := createAgent(t, db, constants.DemoOrganizationID, "synthetic-owner", "Billing reconciliation")
	used := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	require.NoError(t, repo.New(db).SeedAgentCredentialKeyActivityFixture(t.Context(), activityKeyFixture(constants.DemoOrganizationID, "synthetic-owner", agent.ID, used)))
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, constants.DemoOrganizationID, "visitor-without-membership")
	rows, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, &gen.AgentPermissions{Read: true, Write: false, Authorize: false, Transfer: false}, row.Permissions)
		if row.ID == agent.ID.String() {
			require.Equal(t, new(used.Format(time.RFC3339Nano)), row.LastCredentialUsedAt)
		}
	}
	_, err = service.List(validatedHumanContext(t, "org-customer", "visitor-without-membership"), &gen.ListPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	id := agent.ID.String()
	checks := map[string]func() error{
		"get":      func() error { _, err := service.Get(ctx, &gen.GetPayload{ID: id}); return err },
		"sessions": func() error { _, err := service.ListSessions(ctx, &gen.ListSessionsPayload{AgentID: id}); return err },
		"policy read": func() error {
			_, err := service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: id})
			return err
		},
		"create":  func() error { _, err := service.Create(ctx, &gen.CreatePayload{Name: "Forbidden"}); return err },
		"rename":  func() error { _, err := service.Rename(ctx, &gen.RenamePayload{ID: id, Name: "Forbidden"}); return err },
		"suspend": func() error { _, err := service.Suspend(ctx, &gen.SuspendPayload{AgentID: id}); return err },
		"resume":  func() error { _, err := service.Resume(ctx, &gen.ResumePayload{AgentID: id}); return err },
		"revoke":  func() error { _, err := service.Revoke(ctx, &gen.RevokePayload{AgentID: id}); return err },
		"transfer": func() error {
			_, err := service.Transfer(ctx, &gen.TransferPayload{AgentID: id, OwnerUserID: "synthetic-owner"})
			return err
		},
		"delete": func() error { return service.Delete(ctx, &gen.DeletePayload{AgentID: id}) },
		// Agent API-key listing uses this same locked credential authorization seam.
		"key access": func() error {
			_, _, err := service.authorizer.RequireAgentForUpdate(ctx, db, agent.ID, OwnedAgentAuthorize)
			return err
		},
		"policy write": func() error {
			_, _, err := service.authorizer.RequireAgentForUpdate(ctx, db, agent.ID, OwnedAgentSetup)
			return err
		},
	}
	for name, check := range checks {
		err := check()
		require.Error(t, err, name)
		requireOopsCode(t, err, oops.CodeForbidden)
	}
}

func TestDemoListRejectsAlternateCallers(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	valid := validatedHumanContext(t, constants.DemoOrganizationID, "visitor")
	keyAuth := *mustAuthContext(t, valid)
	keyAuth.APIKeyID = "key-id"
	supportAuth := *mustAuthContext(t, valid)
	supportAuth.IsAdmin = true
	supportAuth.SupportOrganizationID = constants.DemoOrganizationID
	support := contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(t.Context(), &supportAuth), &supportAuth, false)
	support = contextvalues.WithValidatedSupportSession(support, mustAuthContext(t, support))
	for name, ctx := range map[string]context.Context{
		"key":           contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(t.Context(), &keyAuth), &keyAuth, false),
		"support":       support,
		"impersonation": contextvalues.WithValidatedGramSession(t.Context(), mustAuthContext(t, valid), true),
		"assistant":     contextvalues.SetAssistantPrincipal(valid, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}),
		"oauth":         contextvalues.SetOAuthClientID(valid, "client-id"),
		"override":      contextvalues.SetRBACScopeOverride(valid, "root"),
	} {
		_, err := service.List(ctx, &gen.ListPayload{})
		require.Error(t, err, name)
		requireOopsCode(t, err, oops.CodeForbidden)
	}
}
