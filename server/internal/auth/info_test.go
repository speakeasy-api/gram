package auth_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestService_Info(t *testing.T) {
	t.Parallel()

	t.Run("successful info request for regular user", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create user in database
		err := instance.createTestUser(ctx, userInfo)
		require.NoError(t, err)

		// Create organization in database
		err = instance.createTestOrganization(ctx, userInfo.Organizations[0])
		require.NoError(t, err)

		// Create and store a session first
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
		}
		err = instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, session.SessionID, result.SessionToken)
		require.Equal(t, session.SessionID, result.SessionCookie)
		require.Equal(t, session.ActiveOrganizationID, result.ActiveOrganizationID)
		require.Equal(t, userInfo.UserID, result.UserID)
		require.Equal(t, userInfo.Email, result.UserEmail)
		require.Equal(t, userInfo.Admin, result.IsAdmin)
		require.Equal(t, "test", result.GramAccountType)
		require.Len(t, result.Organizations, 1)

		org := result.Organizations[0]
		require.Equal(t, userInfo.Organizations[0].ID, org.ID)
		require.Equal(t, userInfo.Organizations[0].Name, org.Name)
		require.Equal(t, userInfo.Organizations[0].Slug, org.Slug)
		require.Len(t, org.Projects, 1) // Default project should be created
		require.Equal(t, "Default", org.Projects[0].Name)
	})

	t.Run("info request for admin user filters organizations", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		// Add additional organizations to test filtering
		userInfo.Organizations = append(userInfo.Organizations, MockOrganizationEntry{
			ID:                 "other-org-456",
			Name:               "Other Organization",
			Slug:               "other-org",
			WorkosID:           nil,
			UserWorkspaceSlugs: []string{"other-workspace"},
		})
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session first
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID, // First org is active
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.True(t, result.IsAdmin)
		// Admin should only see the active organization
		require.Len(t, result.Organizations, 1)
		require.Equal(t, userInfo.Organizations[0].ID, result.Organizations[0].ID)
	})

	t.Run("unauthenticated info request", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// No auth context set
		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.Error(t, err)
		require.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("info request without session ID", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Set up auth context without session ID
		authCtx := &contextvalues.AuthContext{
			SessionID:            nil, // No session ID
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
			AccountType:          "test",
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.Error(t, err)
		require.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("info request creates default project and environment", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create user in database
		err := instance.createTestUser(ctx, userInfo)
		require.NoError(t, err)

		// Create organization in database
		err = instance.createTestOrganization(ctx, userInfo.Organizations[0])
		require.NoError(t, err)

		// Create and store a session first
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
		}
		err = instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Len(t, result.Organizations, 1)
		org := result.Organizations[0]
		require.Len(t, org.Projects, 1)

		project := org.Projects[0]
		require.Equal(t, "Default", project.Name)
		require.Equal(t, "default", string(project.Slug))
	})
}

// TestService_Info_RegularUserOrgRelationshipUpserted verifies that a standard (non-admin)
// user who belongs to a non-speakeasy org gets an organization_user_relationships row
// created when Info is called. This is the common path for all real customers.
func TestService_Info_RegularUserOrgRelationshipUpserted(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)

	err := instance.createTestUser(ctx, userInfo)
	require.NoError(t, err)

	err = instance.createTestOrganization(ctx, userInfo.Organizations[0])
	require.NoError(t, err)

	queries := orgRepo.New(instance.conn)

	// Confirm no relationship exists before the Info call.
	exists, err := queries.HasOrganizationUserRelationship(ctx, orgRepo.HasOrganizationUserRelationshipParams{
		OrganizationID: userInfo.Organizations[0].ID,
		UserID:         userInfo.UserID,
	})
	require.NoError(t, err)
	require.False(t, exists, "expected no org-user relationship before Info call")

	session := sessions.Session{
		SessionID:            "regular-user-session-id",
		UserID:               userInfo.UserID,
		ActiveOrganizationID: userInfo.Organizations[0].ID,
	}
	err = instance.sessionManager.StoreSession(ctx, session)
	require.NoError(t, err)

	authCtx := &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		AccountType:          "test",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	}
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	result, err := instance.service.Info(ctx, &gen.InfoPayload{SessionToken: nil})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsAdmin)

	// The upsert must have created the relationship.
	exists, err = queries.HasOrganizationUserRelationship(ctx, orgRepo.HasOrganizationUserRelationshipParams{
		OrganizationID: userInfo.Organizations[0].ID,
		UserID:         userInfo.UserID,
	})
	require.NoError(t, err)
	require.True(t, exists, "expected org-user relationship to be upserted by Info call")
}

// TestService_Info_AdminOrgRelationshipUpserted verifies that an admin user who has no
// pre-existing record in organization_user_relationships gets one created when Info is
// called for their speakeasy-team org.
func TestService_Info_AdminOrgRelationshipUpserted(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID:          "admin-user-speakeasy",
		Email:           "admin@speakeasyapi.dev",
		Admin:           true,
		UserWhitelisted: true,
		Organizations: []MockOrganizationEntry{
			{
				ID:                 "speakeasy-team-org-id",
				Name:               "Speakeasy Team",
				Slug:               "speakeasy-team",
				WorkosID:           nil,
				UserWorkspaceSlugs: []string{"speakeasy-workspace"},
			},
		},
	}
	ctx, instance := newTestAuthService(t, userInfo)

	err := instance.createTestUser(ctx, userInfo)
	require.NoError(t, err)

	err = instance.createTestOrganization(ctx, userInfo.Organizations[0])
	require.NoError(t, err)

	queries := orgRepo.New(instance.conn)

	// Confirm no relationship exists before the Info call.
	exists, err := queries.HasOrganizationUserRelationship(ctx, orgRepo.HasOrganizationUserRelationshipParams{
		OrganizationID: userInfo.Organizations[0].ID,
		UserID:         userInfo.UserID,
	})
	require.NoError(t, err)
	require.False(t, exists, "expected no org-user relationship before Info call")

	session := sessions.Session{
		SessionID:            "admin-session-id",
		UserID:               userInfo.UserID,
		ActiveOrganizationID: userInfo.Organizations[0].ID,
	}
	err = instance.sessionManager.StoreSession(ctx, session)
	require.NoError(t, err)

	authCtx := &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		AccountType:          "test",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	}
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	result, err := instance.service.Info(ctx, &gen.InfoPayload{SessionToken: nil})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsAdmin)

	// The upsert must have created the relationship.
	exists, err = queries.HasOrganizationUserRelationship(ctx, orgRepo.HasOrganizationUserRelationshipParams{
		OrganizationID: userInfo.Organizations[0].ID,
		UserID:         userInfo.UserID,
	})
	require.NoError(t, err)
	require.True(t, exists, "expected org-user relationship to be upserted by Info call")
}

// TestService_Info_ProjectFilter verifies that the ProjectFilterFunc is applied
// to the projects returned in the Info response, matching the behaviour of
// projects.List which uses access.Filter.
func TestService_Info_ProjectFilter(t *testing.T) {
	t.Parallel()

	// setupInfoCtx is a small helper that creates the user, org, session and
	// auth context needed to call Info. It returns the ready-to-use context.
	setupInfoCtx := func(t *testing.T, instance *testInstance, userInfo *MockUserInfo) context.Context {
		t.Helper()
		ctx := t.Context()

		err := instance.createTestUser(ctx, userInfo)
		require.NoError(t, err)
		err = instance.createTestOrganization(ctx, userInfo.Organizations[0])
		require.NoError(t, err)

		session := sessions.Session{
			SessionID:            "filter-test-session",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
		}
		err = instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			Email:                &userInfo.Email,
		})
	}

	t.Run("filter removes disallowed projects", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()

		// Filter that only allows the first project ID it sees.
		var seen []string
		filter := func(_ context.Context, ids []string) ([]string, error) {
			seen = ids
			if len(ids) > 0 {
				return ids[:1], nil
			}
			return nil, nil
		}

		_, instance := newTestAuthServiceWithFilter(t, userInfo, filter)
		ctx := setupInfoCtx(t, instance, userInfo)

		orgID := userInfo.Organizations[0].ID
		p1, err := instance.createTestProject(ctx, orgID, "ProjectA", "project-a")
		require.NoError(t, err)
		p2, err := instance.createTestProject(ctx, orgID, "ProjectB", "project-b")
		require.NoError(t, err)

		result, err := instance.service.Info(ctx, &gen.InfoPayload{})
		require.NoError(t, err)
		require.Len(t, result.Organizations, 1)

		// The filter was called with both project IDs.
		assert.Len(t, seen, 2)

		// Only the first project (by UUID order) should survive.
		// We cannot assume p1 sorts before p2 since UUIDs are random.
		require.Len(t, result.Organizations[0].Projects, 1)
		assert.Equal(t, seen[0], result.Organizations[0].Projects[0].ID)

		// The surviving project must be one of the two we created.
		validIDs := []string{p1.ID.String(), p2.ID.String()}
		assert.Contains(t, validIDs, result.Organizations[0].Projects[0].ID)
	})

	t.Run("filter allows all projects", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.UserID = "filter-all-user"
		userInfo.Email = "filterall@example.com"
		userInfo.Organizations[0].ID = "filter-all-org"

		// Pass-through filter.
		filter := func(_ context.Context, ids []string) ([]string, error) {
			return ids, nil
		}

		_, instance := newTestAuthServiceWithFilter(t, userInfo, filter)
		ctx := setupInfoCtx(t, instance, userInfo)

		orgID := userInfo.Organizations[0].ID
		_, err := instance.createTestProject(ctx, orgID, "ProjX", "proj-x")
		require.NoError(t, err)
		_, err = instance.createTestProject(ctx, orgID, "ProjY", "proj-y")
		require.NoError(t, err)

		result, err := instance.service.Info(ctx, &gen.InfoPayload{})
		require.NoError(t, err)

		assert.Len(t, result.Organizations[0].Projects, 2)
	})

	t.Run("filter removes all projects", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.UserID = "filter-none-user"
		userInfo.Email = "filternone@example.com"
		userInfo.Organizations[0].ID = "filter-none-org"

		// Deny-all filter.
		filter := func(_ context.Context, _ []string) ([]string, error) {
			return nil, nil
		}

		_, instance := newTestAuthServiceWithFilter(t, userInfo, filter)
		ctx := setupInfoCtx(t, instance, userInfo)

		orgID := userInfo.Organizations[0].ID
		_, err := instance.createTestProject(ctx, orgID, "Hidden", "hidden")
		require.NoError(t, err)

		result, err := instance.service.Info(ctx, &gen.InfoPayload{})
		require.NoError(t, err)

		assert.Empty(t, result.Organizations[0].Projects)
	})

	t.Run("filter error propagates", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.UserID = "filter-err-user"
		userInfo.Email = "filtererr@example.com"
		userInfo.Organizations[0].ID = "filter-err-org"

		filterErr := fmt.Errorf("access denied")
		filter := func(_ context.Context, _ []string) ([]string, error) {
			return nil, filterErr
		}

		_, instance := newTestAuthServiceWithFilter(t, userInfo, filter)
		ctx := setupInfoCtx(t, instance, userInfo)

		orgID := userInfo.Organizations[0].ID
		_, err := instance.createTestProject(ctx, orgID, "Proj", "proj")
		require.NoError(t, err)

		result, err := instance.service.Info(ctx, &gen.InfoPayload{})
		require.ErrorIs(t, err, filterErr)
		require.Nil(t, result)
	})

	t.Run("nil filter returns all projects", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.UserID = "nil-filter-user"
		userInfo.Email = "nilfilter@example.com"
		userInfo.Organizations[0].ID = "nil-filter-org"

		// Use the standard constructor (nil filter).
		_, instance := newTestAuthService(t, userInfo)
		ctx := setupInfoCtx(t, instance, userInfo)

		orgID := userInfo.Organizations[0].ID
		_, err := instance.createTestProject(ctx, orgID, "Visible", "visible")
		require.NoError(t, err)

		result, err := instance.service.Info(ctx, &gen.InfoPayload{})
		require.NoError(t, err)

		assert.Len(t, result.Organizations[0].Projects, 1)
		assert.Equal(t, "Visible", result.Organizations[0].Projects[0].Name)
	})
}

// TestService_Info_AdminVisitingCustomerOrgDoesNotUpsertRelationship verifies that
// when an admin uses the org override to view a customer org, no relationship row is
// written for that customer org. Only the admin's own orgs should be upserted.
func TestService_Info_AdminVisitingCustomerOrgDoesNotUpsertRelationship(t *testing.T) {
	t.Parallel()

	// adminMockUserInfo gives the admin a single own org: admin-org-123.
	userInfo := adminMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)

	err := instance.createTestUser(ctx, userInfo)
	require.NoError(t, err)

	// Create the admin's own org so the projects loop in Info can resolve it.
	err = instance.createTestOrganization(ctx, userInfo.Organizations[0])
	require.NoError(t, err)

	// Simulate the admin override: the session's active org is a customer org that
	// does not appear in the admin's userInfo.Organizations list.
	const customerOrgID = "customer-org-override-456"
	session := sessions.Session{
		SessionID:            "admin-customer-session-id",
		UserID:               userInfo.UserID,
		ActiveOrganizationID: customerOrgID,
	}
	err = instance.sessionManager.StoreSession(ctx, session)
	require.NoError(t, err)

	authCtx := &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		AccountType:          "test",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	}
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	result, err := instance.service.Info(ctx, &gen.InfoPayload{SessionToken: nil})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsAdmin)

	// No relationship row must be created for the customer org.
	queries := orgRepo.New(instance.conn)
	exists, err := queries.HasOrganizationUserRelationship(ctx, orgRepo.HasOrganizationUserRelationshipParams{
		OrganizationID: customerOrgID,
		UserID:         userInfo.UserID,
	})
	require.NoError(t, err)
	require.False(t, exists, "admin must not be upserted into a customer org's relationship table")
}
