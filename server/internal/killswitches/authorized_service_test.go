package killswitches

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type genericServiceStub struct {
	calls      []string
	activate   ActivatePrescriptionInput
	change     ChangePrescriptionRequest
	deactivate DeactivatePrescriptionRequest
	get        GetPrescriptionRequest
	list       ListPrescriptionsRequest
	listResult *ListPrescriptionsResult
	err        error
}

func (s *genericServiceStub) ListDefinitions(context.Context) ([]Definition, error) {
	s.calls = append(s.calls, "list-definitions")
	return []Definition{{Key: "definition"}}, s.err
}

func (s *genericServiceStub) ActivatePrescription(_ context.Context, request ActivatePrescriptionInput) (MutationResult, error) {
	s.calls = append(s.calls, "activate")
	s.activate = request
	return MutationResult{PrescriptionID: "00000000-0000-0000-0000-000000000001", Version: 1}, s.err
}

func (s *genericServiceStub) ChangePrescription(_ context.Context, request ChangePrescriptionRequest) (MutationResult, error) {
	s.calls = append(s.calls, "change")
	s.change = request
	return MutationResult{PrescriptionID: request.PrescriptionID, Version: request.ExpectedVersion + 1}, s.err
}

func (s *genericServiceStub) DeactivatePrescription(_ context.Context, request DeactivatePrescriptionRequest) (MutationResult, error) {
	s.calls = append(s.calls, "deactivate")
	s.deactivate = request
	return MutationResult{PrescriptionID: request.PrescriptionID, Version: request.ExpectedVersion + 1}, s.err
}

func (s *genericServiceStub) GetPrescription(_ context.Context, request GetPrescriptionRequest) (CurrentPrescription, error) {
	s.calls = append(s.calls, "get")
	s.get = request
	return CurrentPrescription{ID: request.PrescriptionID, OrganizationID: request.OrganizationID}, s.err
}

func (s *genericServiceStub) ListPrescriptions(_ context.Context, request ListPrescriptionsRequest) (ListPrescriptionsResult, error) {
	s.calls = append(s.calls, "list")
	s.list = request
	if s.listResult != nil {
		return *s.listResult, s.err
	}
	return ListPrescriptionsResult{Prescriptions: []CurrentPrescription{{OrganizationID: request.OrganizationID}}}, s.err
}

type authorizationCheck struct {
	organizationID string
	userID         string
	scope          authz.Scope
}

type authorizerStub struct {
	err    error
	checks []authorizationCheck
}

func (a *authorizerStub) RequireUserOrganizationScope(_ context.Context, organizationID, userID string, scope authz.Scope) error {
	a.checks = append(a.checks, authorizationCheck{organizationID: organizationID, userID: userID, scope: scope})
	return a.err
}

func TestAuthorizedServiceDerivesTenantAndActorForAllSixMethods(t *testing.T) {
	t.Parallel()
	generic := &genericServiceStub{}
	authorizer := &authorizerStub{}
	service, err := NewAuthorizedService(generic, authorizer)
	require.NoError(t, err)
	ctx := validatedCustomerContext(t, "org_trusted", "user_trusted", "admin@example.com")
	prescriptionID := PrescriptionID("00000000-0000-0000-0000-000000000001")

	_, err = service.ListDefinitions(ctx)
	require.NoError(t, err)
	_, err = service.ActivatePrescription(ctx, AuthorizedActivatePrescriptionRequest{OperationID: uuid.New(), Definition: "definition", PrincipalKind: "user", PrincipalInput: "principal", ResourceKind: "tool", Desired: testDesired([]string{"tool:a"})})
	require.NoError(t, err)
	_, err = service.ChangePrescription(ctx, AuthorizedChangePrescriptionRequest{OperationID: uuid.New(), PrescriptionID: prescriptionID, ExpectedVersion: 1, Desired: testDesired([]string{"tool:b"})})
	require.NoError(t, err)
	_, err = service.DeactivatePrescription(ctx, AuthorizedDeactivatePrescriptionRequest{OperationID: uuid.New(), PrescriptionID: prescriptionID, ExpectedVersion: 2})
	require.NoError(t, err)
	_, err = service.GetPrescription(ctx, AuthorizedGetPrescriptionRequest{PrescriptionID: prescriptionID})
	require.NoError(t, err)
	_, err = service.ListPrescriptions(ctx, AuthorizedListPrescriptionsRequest{Limit: 12})
	require.NoError(t, err)

	require.Equal(t, []string{"list-definitions", "activate", "change", "deactivate", "get", "list"}, generic.calls)
	require.Len(t, authorizer.checks, 6)
	for _, check := range authorizer.checks {
		require.Equal(t, authorizationCheck{organizationID: "org_trusted", userID: "user_trusted", scope: authz.ScopeOrgAdmin}, check)
	}
	require.Equal(t, OrganizationID("org_trusted"), generic.activate.OrganizationID)
	require.Equal(t, "user_trusted", generic.activate.ActorUserID)
	require.Equal(t, "admin@example.com", generic.activate.ActorDisplayName)
	require.Equal(t, OrganizationID("org_trusted"), generic.change.OrganizationID)
	require.Equal(t, "user_trusted", generic.deactivate.ActorUserID)
	require.Equal(t, GetPrescriptionRequest{OrganizationID: "org_trusted", PrescriptionID: prescriptionID}, generic.get)
	require.Equal(t, ListPrescriptionsRequest{OrganizationID: "org_trusted", Limit: 12}, generic.list)
}

func TestAuthorizedServiceRejectsAdminRevokedAfterContextPreparation(t *testing.T) {
	t.Parallel()

	conn, organizationID := newLifecycleDatabase(t, "killswitch_authorized_revocation")
	userID := "user_" + uuid.NewString()
	_, err := usersrepo.New(conn).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID: userID, Email: userID + "@example.com", DisplayName: userID, PhotoUrl: pgtype.Text{}, Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: pgtype.Text{String: userID, Valid: true},
	})
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeOrgAdmin, organizationID).MarshalJSON()
	require.NoError(t, err)
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, userID)
	grant, err := accessrepo.New(conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID, PrincipalUrn: principal, Scope: string(authz.ScopeOrgAdmin), Selectors: selectors,
	})
	require.NoError(t, err)

	ctx := validatedCustomerContext(t, organizationID, userID, "admin@example.com")
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	ctx, err = engine.PrepareContext(ctx)
	require.NoError(t, err)
	check := authz.Check{Scope: authz.ScopeOrgAdmin, ResourceID: organizationID}
	require.NoError(t, engine.Require(ctx, check))

	deleted, err := accessrepo.New(conn).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{
		ID: grant.ID, OrganizationID: organizationID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	// The prepared context is stale, so only live authorization can observe revocation.
	require.NoError(t, engine.Require(ctx, check))

	generic := &genericServiceStub{}
	service, err := NewAuthorizedService(generic, engine)
	require.NoError(t, err)
	_, err = service.ListDefinitions(ctx)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, generic.calls)
}

func TestAuthorizedServiceRejectsCredentialAndBypassMatrixAcrossAllMethods(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		context    func(*testing.T) context.Context
		authzErr   error
		wantCode   oops.Code
		wantChecks int
	}{
		{name: "missing auth", context: func(t *testing.T) context.Context { t.Helper(); return t.Context() }, wantCode: oops.CodeUnauthorized},
		{name: "session shaped but unvalidated", context: unvalidatedSessionContext, wantCode: oops.CodeUnauthorized},
		{name: "api key", context: apiKeyContext, wantCode: oops.CodeUnauthorized},
		{name: "organization only", context: organizationOnlyContext, wantCode: oops.CodeUnauthorized},
		{name: "missing active organization", context: func(t *testing.T) context.Context {
			t.Helper()
			return validatedCustomerContext(t, "", "user", "user@example.com")
		}, wantCode: oops.CodeUnauthorized},
		{name: "missing user", context: func(t *testing.T) context.Context {
			t.Helper()
			return validatedCustomerContext(t, "org", "", "user@example.com")
		}, wantCode: oops.CodeUnauthorized},
		{name: "missing session", context: validatedMissingSessionContext, wantCode: oops.CodeUnauthorized},
		{name: "marked api key", context: markedAPIKeyContext, wantCode: oops.CodeForbidden},
		{name: "assistant token", context: markedAssistantContext, wantCode: oops.CodeForbidden},
		{name: "mcp oauth", context: markedOAuthContext, wantCode: oops.CodeForbidden},
		{name: "platform mcp", context: markedPlatformMCPContext, wantCode: oops.CodeForbidden},
		{name: "support session", context: supportSessionContext, wantCode: oops.CodeForbidden},
		{name: "legacy impersonation", context: legacyImpersonationContext, wantCode: oops.CodeForbidden},
		{name: "demo organization", context: demoOrganizationContext, wantCode: oops.CodeForbidden},
		{name: "rbac override", context: rbacOverrideContext, wantCode: oops.CodeForbidden},
		{name: "ordinary member", context: func(t *testing.T) context.Context {
			t.Helper()
			return validatedCustomerContext(t, "org", "member", "member@example.com")
		}, authzErr: oops.C(oops.CodeForbidden), wantCode: oops.CodeForbidden, wantChecks: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			generic := &genericServiceStub{}
			authorizer := &authorizerStub{err: test.authzErr}
			service, err := NewAuthorizedService(generic, authorizer)
			require.NoError(t, err)
			ctx := test.context(t)
			for _, invoke := range authorizedMethodInvocations() {
				requireOopsCode(t, invoke(ctx, service), test.wantCode)
			}
			require.Empty(t, generic.calls)
			require.Len(t, authorizer.checks, test.wantChecks)
		})
	}
}

func TestAuthorizedServicePreservesLifecycleErrors(t *testing.T) {
	t.Parallel()
	for _, target := range []error{ErrOperationConflict, &VersionConflictError{Expected: 1, Actual: 2}, ErrOperationUnavailable} {
		generic := &genericServiceStub{err: target}
		service, err := NewAuthorizedService(generic, &authorizerStub{})
		require.NoError(t, err)
		_, err = service.ActivatePrescription(validatedCustomerContext(t, "org", "user", "user@example.com"), AuthorizedActivatePrescriptionRequest{OperationID: uuid.New()})
		require.Error(t, err)
		require.True(t, errors.Is(err, target) || errors.Is(err, ErrVersionConflict))
	}
}

func authorizedMethodInvocations() []func(context.Context, *AuthorizedService) error {
	prescriptionID := PrescriptionID("00000000-0000-0000-0000-000000000001")
	return []func(context.Context, *AuthorizedService) error{
		func(ctx context.Context, service *AuthorizedService) error {
			_, err := service.ListDefinitions(ctx)
			return err
		},
		func(ctx context.Context, service *AuthorizedService) error {
			_, err := service.ActivatePrescription(ctx, AuthorizedActivatePrescriptionRequest{OperationID: uuid.New()})
			return err
		},
		func(ctx context.Context, service *AuthorizedService) error {
			_, err := service.ChangePrescription(ctx, AuthorizedChangePrescriptionRequest{OperationID: uuid.New(), PrescriptionID: prescriptionID, ExpectedVersion: 1})
			return err
		},
		func(ctx context.Context, service *AuthorizedService) error {
			_, err := service.DeactivatePrescription(ctx, AuthorizedDeactivatePrescriptionRequest{OperationID: uuid.New(), PrescriptionID: prescriptionID, ExpectedVersion: 1})
			return err
		},
		func(ctx context.Context, service *AuthorizedService) error {
			_, err := service.GetPrescription(ctx, AuthorizedGetPrescriptionRequest{PrescriptionID: prescriptionID})
			return err
		},
		func(ctx context.Context, service *AuthorizedService) error {
			_, err := service.ListPrescriptions(ctx, AuthorizedListPrescriptionsRequest{})
			return err
		},
		func(ctx context.Context, service *AuthorizedService) error {
			_, err := service.ListCustomerPrescriptions(ctx, AuthorizedListCustomerPrescriptionsRequest{})
			return err
		},
	}
}

func TestAuthorizedRequestDTOsContainNoTrustedOrBypassFields(t *testing.T) {
	t.Parallel()
	for _, value := range []any{AuthorizedActivatePrescriptionRequest{}, AuthorizedChangePrescriptionRequest{}, AuthorizedDeactivatePrescriptionRequest{}, AuthorizedGetPrescriptionRequest{}, AuthorizedListPrescriptionsRequest{}, AuthorizedListCustomerPrescriptionsRequest{}} {
		typeOf := reflect.TypeOf(value)
		for field := range typeOf.Fields() {
			name := strings.ToLower(field.Name)
			for _, forbidden := range []string{"organization", "actor", "override", "bypass"} {
				require.NotContains(t, name, forbidden)
			}
		}
	}
}

func validatedCustomerContext(t *testing.T, organizationID, userID, email string) context.Context {
	t.Helper()
	sessionID := "session"
	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: organizationID, UserID: userID, SessionID: &sessionID, Email: &email}
	return contextvalues.WithValidatedGramSession(t.Context(), authCtx, false)
}

func unvalidatedSessionContext(t *testing.T) context.Context {
	t.Helper()
	sessionID, email := "session", "user@example.com"
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID, Email: &email})
}

func apiKeyContext(t *testing.T) context.Context {
	t.Helper()
	email := "user@example.com"
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", APIKeyID: "key", Email: &email})
}

func organizationOnlyContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org"})
}

func validatedMissingSessionContext(t *testing.T) context.Context {
	t.Helper()
	email := "user@example.com"
	return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", Email: &email}, false)
}

func markedAPIKeyContext(t *testing.T) context.Context {
	t.Helper()
	ctx := validatedCustomerContext(t, "org", "user", "user@example.com")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	authCtx.APIKeyID = "key"
	return ctx
}

func markedAssistantContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetAssistantPrincipal(validatedCustomerContext(t, "org", "user", "user@example.com"), contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
}

func markedOAuthContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetOAuthClientID(validatedCustomerContext(t, "org", "user", "user@example.com"), "client")
}

func markedPlatformMCPContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetActingSurface(validatedCustomerContext(t, "org", "user", "user@example.com"), contextvalues.ActingSurfacePlatformMCP)
}

func supportSessionContext(t *testing.T) context.Context {
	t.Helper()
	ctx := validatedCustomerContext(t, "org", "support", "support@example.com")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	authCtx.IsAdmin = true
	authCtx.SupportOrganizationID = "org"
	return contextvalues.WithValidatedSupportSession(ctx, authCtx)
}

func legacyImpersonationContext(t *testing.T) context.Context {
	t.Helper()
	sessionID, email := "session", "user@example.com"
	return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID, Email: &email}, true)
}

func demoOrganizationContext(t *testing.T) context.Context {
	t.Helper()
	return validatedCustomerContext(t, constants.DemoOrganizationID, "user", "user@example.com")
}

func rbacOverrideContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetRBACScopeOverride(validatedCustomerContext(t, "org", "user", "user@example.com"), string(authz.ScopeOrgAdmin))
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}
