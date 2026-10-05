package killswitches

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"

	gen "github.com/speakeasy-api/gram/server/gen/platform_killswitches"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type platformAdminReaderStub struct {
	results     []bool
	err         error
	calls       []string
	displayName string
}

func (s *platformAdminReaderStub) GetPlatformAdminIdentity(_ context.Context, userID string) (bool, string, error) {
	s.calls = append(s.calls, userID)
	if s.err != nil {
		return false, "", s.err
	}
	if len(s.results) == 0 {
		return false, "", nil
	}
	result := s.results[0]
	if len(s.results) > 1 {
		s.results = s.results[1:]
	}
	return result, s.displayName, nil
}

type platformGenericStub struct {
	genericServiceStub
	surfaces []string
}

func (s *platformGenericStub) record(ctx context.Context) {
	surface, _ := contextvalues.GetActingSurface(ctx)
	s.surfaces = append(s.surfaces, surface)
}

func (s *platformGenericStub) ListDefinitions(ctx context.Context) ([]Definition, error) {
	s.record(ctx)
	return s.genericServiceStub.ListDefinitions(ctx)
}

func (s *platformGenericStub) ActivatePrescription(ctx context.Context, request ActivatePrescriptionInput) (MutationResult, error) {
	s.record(ctx)
	return s.genericServiceStub.ActivatePrescription(ctx, request)
}

func (s *platformGenericStub) ChangePrescription(ctx context.Context, request ChangePrescriptionRequest) (MutationResult, error) {
	s.record(ctx)
	return s.genericServiceStub.ChangePrescription(ctx, request)
}

func (s *platformGenericStub) DeactivatePrescription(ctx context.Context, request DeactivatePrescriptionRequest) (MutationResult, error) {
	s.record(ctx)
	return s.genericServiceStub.DeactivatePrescription(ctx, request)
}

func (s *platformGenericStub) GetPrescription(ctx context.Context, request GetPrescriptionRequest) (CurrentPrescription, error) {
	s.record(ctx)
	return s.genericServiceStub.GetPrescription(ctx, request)
}

func (s *platformGenericStub) ListPrescriptions(ctx context.Context, request ListPrescriptionsRequest) (ListPrescriptionsResult, error) {
	s.record(ctx)
	return s.genericServiceStub.ListPrescriptions(ctx, request)
}

func TestPlatformKillswitchRequestDecoderBoundsBody(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/rpc/platformKillswitches.activatePrescription", bytes.NewReader(make([]byte, maxPlatformKillswitchRequestBodyBytes+1)))
	_ = platformKillswitchRequestDecoder(request)
	read, err := io.ReadAll(request.Body)
	var maxBytesErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxBytesErr)
	require.Equal(t, int64(maxPlatformKillswitchRequestBodyBytes), maxBytesErr.Limit)
	require.Len(t, read, maxPlatformKillswitchRequestBodyBytes)
}

func TestPlatformServiceDelegatesAllSixOperationsWithExplicitTargetAndSessionActor(t *testing.T) {
	t.Parallel()

	generic := &platformGenericStub{}
	admins := &platformAdminReaderStub{results: []bool{true}, displayName: "Platform Operator"}
	service := &PlatformService{logger: testenv.NewLogger(t), sessions: admins, generic: generic}
	ctx := validatedCustomerContext(t, "", "actor_user", " actor@example.com ")

	for _, err := range invokeAllPlatformMethods(ctx, service, "org_target") {
		require.NoError(t, err)
	}

	require.Equal(t, []string{"list-definitions", "activate", "change", "deactivate", "get", "list"}, generic.calls)
	require.Equal(t, []string{"actor_user", "actor_user", "actor_user", "actor_user", "actor_user", "actor_user"}, admins.calls)
	require.Equal(t, OrganizationID("org_target"), generic.activate.OrganizationID)
	require.Equal(t, OrganizationID("org_target"), generic.change.OrganizationID)
	require.Equal(t, OrganizationID("org_target"), generic.deactivate.OrganizationID)
	require.Equal(t, OrganizationID("org_target"), generic.get.OrganizationID)
	require.Equal(t, OrganizationID("org_target"), generic.list.OrganizationID)
	require.Equal(t, "actor_user", generic.activate.ActorUserID)
	require.Equal(t, "Platform Operator", generic.activate.ActorDisplayName)
	require.Equal(t, "actor_user", generic.change.ActorUserID)
	require.Equal(t, "Platform Operator", generic.change.ActorDisplayName)
	require.Equal(t, "actor_user", generic.deactivate.ActorUserID)
	require.Equal(t, "Platform Operator", generic.deactivate.ActorDisplayName)
	require.Equal(t, []string{
		string(audit.SurfacePlatformBreakGlass), string(audit.SurfacePlatformBreakGlass), string(audit.SurfacePlatformBreakGlass),
		string(audit.SurfacePlatformBreakGlass), string(audit.SurfacePlatformBreakGlass), string(audit.SurfacePlatformBreakGlass),
	}, generic.surfaces)
}

func TestPlatformServicePreservesListPagination(t *testing.T) {
	t.Parallel()

	afterID := PrescriptionID(uuid.NewString())
	nextAfterID := PrescriptionID(uuid.NewString())
	generic := &platformGenericStub{genericServiceStub: genericServiceStub{listResult: &ListPrescriptionsResult{
		Prescriptions: []CurrentPrescription{{ID: afterID, OrganizationID: "org_target", StartsAt: time.Now()}},
		NextAfterID:   &nextAfterID,
	}}}
	service := &PlatformService{logger: testenv.NewLogger(t), sessions: &platformAdminReaderStub{results: []bool{true}}, generic: generic}
	after := string(afterID)

	result, err := service.ListPrescriptions(
		validatedCustomerContext(t, "", "operator", "operator@example.com"),
		&gen.ListPrescriptionsPayload{OrganizationID: "org_target", AfterID: &after},
	)
	require.NoError(t, err)
	require.Equal(t, afterID, *generic.list.AfterID)
	require.Equal(t, string(nextAfterID), *result.NextAfterID)
}

func TestPlatformServiceReactivationOmitsImmutableIdentity(t *testing.T) {
	t.Parallel()

	generic := &platformGenericStub{}
	service := &PlatformService{logger: testenv.NewLogger(t), sessions: &platformAdminReaderStub{results: []bool{true}}, generic: generic}
	prescriptionID := uuid.NewString()
	expectedVersion := int64(4)
	startsAt := "2026-08-30T12:00:00Z"

	_, err := service.ActivatePrescription(
		validatedCustomerContext(t, "", "operator", "operator@example.com"),
		&gen.ActivatePrescriptionPayload{
			OrganizationID: "org_target", OperationID: uuid.NewString(), PrescriptionID: &prescriptionID, ExpectedVersion: &expectedVersion,
			ResourceScope: "all", StartMode: "at", StartsAt: &startsAt, InternalNote: "internal", ExternalNote: "external",
		},
	)
	require.NoError(t, err)
	require.Equal(t, PrescriptionID(prescriptionID), *generic.activate.PrescriptionID)
	require.Equal(t, expectedVersion, *generic.activate.ExpectedVersion)
	require.Empty(t, generic.activate.Definition)
	require.Empty(t, generic.activate.PrincipalKind)
	require.Empty(t, generic.activate.PrincipalInput)
	require.Empty(t, generic.activate.ResourceKind)
	require.Equal(t, StartModeAt, generic.activate.Desired.StartMode)
	require.NotNil(t, generic.activate.Desired.StartsAt)
}

func TestPlatformServiceRejectsEveryUnsafeCredentialClassAcrossAllOperations(t *testing.T) {
	t.Parallel()

	cachedAdminRevoked := validatedCustomerContext(t, "org_cached", "user", "user@example.com")
	cachedAuth, _ := contextvalues.GetAuthContext(cachedAdminRevoked)
	cachedAuth.IsAdmin = true

	missingUser := validatedCustomerContext(t, "", "", "user@example.com")
	missingEmail := validatedCustomerContext(t, "", "user", "")
	apiKey := markedAPIKeyContext(t)
	chatSessionID, chatEmail := "chat-session", "forged@example.com"
	chatToken := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org", UserID: "user", ExternalUserID: "external-user", SessionID: &chatSessionID, Email: &chatEmail,
	})

	//nolint:containedctx // Explicit contexts are the authorization matrix inputs.
	tests := []struct {
		name  string
		ctx   context.Context
		admin bool
		want  oops.Code
	}{
		{name: "unattributed", ctx: t.Context(), want: oops.CodeUnauthorized},
		{name: "unvalidated session", ctx: unvalidatedSessionContext(t), want: oops.CodeUnauthorized},
		{name: "organization only", ctx: organizationOnlyContext(t), want: oops.CodeUnauthorized},
		{name: "chat token", ctx: chatToken, want: oops.CodeUnauthorized},
		{name: "raw api key", ctx: apiKeyContext(t), want: oops.CodeUnauthorized},
		{name: "validated missing session", ctx: validatedMissingSessionContext(t), want: oops.CodeUnauthorized},
		{name: "validated missing user", ctx: missingUser, want: oops.CodeUnauthorized},
		{name: "validated missing email", ctx: missingEmail, want: oops.CodeUnauthorized},
		{name: "api key marker", ctx: apiKey, admin: true, want: oops.CodeForbidden},
		{name: "assistant", ctx: markedAssistantContext(t), admin: true, want: oops.CodeForbidden},
		{name: "mcp oauth client", ctx: markedOAuthContext(t), admin: true, want: oops.CodeForbidden},
		{name: "platform mcp surface", ctx: markedPlatformMCPContext(t), admin: true, want: oops.CodeForbidden},
		{name: "validated support", ctx: supportSessionContext(t), admin: true, want: oops.CodeForbidden},
		{name: "legacy impersonation", ctx: legacyImpersonationContext(t), admin: true, want: oops.CodeForbidden},
		{name: "rbac override", ctx: rbacOverrideContext(t), admin: true, want: oops.CodeForbidden},
		{name: "ordinary non-admin", ctx: validatedCustomerContext(t, "", "user", "user@example.com"), want: oops.CodeForbidden},
		{name: "cached admin revoked", ctx: cachedAdminRevoked, want: oops.CodeForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			generic := &platformGenericStub{}
			admins := &platformAdminReaderStub{results: []bool{test.admin}}
			service := &PlatformService{logger: testenv.NewLogger(t), sessions: admins, generic: generic}
			for _, err := range invokeAllPlatformMethods(test.ctx, service, "org_target") {
				requireOopsCode(t, err, test.want)
			}
			require.Empty(t, generic.calls)
		})
	}
}

func TestPlatformServiceRereadsCurrentEntitlementForEveryOperation(t *testing.T) {
	t.Parallel()

	generic := &platformGenericStub{}
	admins := &platformAdminReaderStub{results: []bool{true, false, true, true, true, true}}
	service := &PlatformService{logger: testenv.NewLogger(t), sessions: admins, generic: generic}
	errs := invokeAllPlatformMethods(validatedCustomerContext(t, "", "user", "user@example.com"), service, "org_target")

	require.NoError(t, errs[0])
	requireOopsCode(t, errs[1], oops.CodeForbidden)
	for _, err := range errs[2:] {
		require.NoError(t, err)
	}
	require.Len(t, admins.calls, 6)
	require.Equal(t, []string{"list-definitions", "change", "deactivate", "get", "list"}, generic.calls)
}

func TestPlatformServiceFreshEntitlementFailureAndUnconfiguredLifecycleAreUnavailable(t *testing.T) {
	t.Parallel()

	ctx := validatedCustomerContext(t, "", "user", "user@example.com")
	readerFailure := &PlatformService{logger: testenv.NewLogger(t), sessions: &platformAdminReaderStub{err: errors.New("database unavailable")}, generic: &platformGenericStub{}}
	_, err := readerFailure.ListDefinitions(ctx, &gen.ListDefinitionsPayload{})
	requireOopsCode(t, err, oops.CodeUnavailable)

	admins := &platformAdminReaderStub{results: []bool{true}}
	unconfigured := &PlatformService{logger: testenv.NewLogger(t), sessions: admins}
	_, err = unconfigured.ListDefinitions(ctx, &gen.ListDefinitionsPayload{})
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.Equal(t, []string{"user"}, admins.calls)
}

func TestPlatformLifecycleErrorMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		code oops.Code
	}{
		{err: ErrInvalidArgument, code: oops.CodeBadRequest},
		{err: ErrInvalidReference, code: oops.CodeBadRequest},
		{err: ErrInvalidTransition, code: oops.CodeBadRequest},
		{err: ErrPrescriptionNotFound, code: oops.CodeNotFound},
		{err: ErrOperationUnavailable, code: oops.CodeUnavailable},
		{err: errors.New("unexpected"), code: oops.CodeUnexpected},
	}
	for _, test := range tests {
		requireOopsCode(t, mapPlatformLifecycleError(test.err), test.code)
	}
	for err, name := range map[error]string{
		ErrOperationConflict:                          "operation_conflict",
		&VersionConflictError{Expected: 1, Actual: 2}: "version_conflict",
	} {
		var serviceErr *goa.ServiceError
		require.ErrorAs(t, mapPlatformLifecycleError(err), &serviceErr)
		require.Equal(t, name, serviceErr.Name)
	}
}

// Invoking every operation through a GenericService-only adapter proves this
// recovery path cannot call or depend on a killswitch evaluator.
func TestPlatformServiceAllOperationsAreEvaluatorIndependent(t *testing.T) {
	t.Parallel()

	service := &PlatformService{logger: testenv.NewLogger(t), sessions: &platformAdminReaderStub{results: []bool{true}}, generic: &platformGenericStub{}}
	for _, err := range invokeAllPlatformMethods(validatedCustomerContext(t, "", "user", "user@example.com"), service, "org_target") {
		require.NoError(t, err)
	}
}

func invokeAllPlatformMethods(ctx context.Context, service *PlatformService, organizationID string) []error {
	prescriptionID := "00000000-0000-0000-0000-000000000001"
	operationID := uuid.NewString()
	_, listDefinitionsErr := service.ListDefinitions(ctx, &gen.ListDefinitionsPayload{})
	_, activateErr := service.ActivatePrescription(ctx, &gen.ActivatePrescriptionPayload{
		OrganizationID: organizationID, OperationID: operationID, Definition: new("definition"), PrincipalKind: new("user"),
		PrincipalInput: new("principal"), ResourceKind: new("tool"), ResourceScope: "all", StartMode: "now", InternalNote: "internal", ExternalNote: "external",
	})
	_, changeErr := service.ChangePrescription(ctx, &gen.ChangePrescriptionPayload{
		OrganizationID: organizationID, OperationID: uuid.NewString(), PrescriptionID: prescriptionID, ExpectedVersion: 1,
		ResourceScope: "all", StartMode: "now", InternalNote: "internal", ExternalNote: "external",
	})
	_, deactivateErr := service.DeactivatePrescription(ctx, &gen.DeactivatePrescriptionPayload{OrganizationID: organizationID, OperationID: uuid.NewString(), PrescriptionID: prescriptionID, ExpectedVersion: 1})
	_, getErr := service.GetPrescription(ctx, &gen.GetPrescriptionPayload{OrganizationID: organizationID, PrescriptionID: prescriptionID})
	_, listErr := service.ListPrescriptions(ctx, &gen.ListPrescriptionsPayload{OrganizationID: organizationID})
	return []error{listDefinitionsErr, activateErr, changeErr, deactivateErr, getErr, listErr}
}
