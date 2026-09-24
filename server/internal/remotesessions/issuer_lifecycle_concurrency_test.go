package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	orgusersgen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	usergen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/stretchr/testify/require"
)

func TestIssuerLifecycle_RotationAdoptsEMABoundReplacement(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", prepared.State)

	// Adoption and refusals occur before any upstream or credential work.
	rotator := remotesessions.NewClientRotator(testenv.NewLogger(t), ti.conn, nil, nil, nil, ti.redisCache, nil, nil, nil, nil)
	var params remotesessions.RotateClientRegistrationParams
	params.ClientID = in.ClientID
	params.ExpectedClientID = "superseded-registration"
	params.Trigger = remotesessions.RotationTriggerSecretExpired
	current, err := rotator.Rotate(ctx, params)
	require.NoError(t, err)
	require.Equal(t, prepared.ExternalClientID, current.ClientID)

	params.Trigger = remotesessions.RotationTriggerManual
	_, err = rotator.Rotate(ctx, params)
	requireOopsCode(t, err, oops.CodeConflict)
	var payload orgclientsgen.RotateClientPayload
	payload.ID = in.ClientID.String()
	_, err = ti.service.RotateClient(ctx, &payload)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Contains(t, err.Error(), "unlink")
}

func TestIssuerLifecycle_FailedDiscoveryAfterConcurrentEditConflicts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	var issuerID string
	var once sync.Once
	editErr := make(chan error, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() {
			var update gen.UpdateRemoteSessionIssuerPayload
			update.ID = issuerID
			update.Name = conv.PtrEmpty("Concurrent edit")
			_, err := ti.service.UpdateRemoteSessionIssuer(ctx, &update)
			editErr <- err
		})
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("refresh-failure-race", upstream.URL))
	require.NoError(t, err)
	issuerID = created.ID
	var payload gen.RefreshRemoteSessionIssuerMetadataPayload
	payload.ID = created.ID
	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &payload)
	require.NoError(t, <-editErr)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestIssuerLifecycle_ProjectUserIssuerMutationHidesSiblingBindings(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", prepared.State)
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracer, []string{})
	require.NoError(t, err)
	service := usersessions.NewService(logger, tracer, testenv.NewMeterProvider(t), ti.conn, ti.sessionManager, nil,
		authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		audit.NewLogger(), policy, nil, testenv.NewEncryptionClient(t), nil, "", nil)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	sibling := createProject(t, ctx, ti.conn, "issuer-review-sibling")
	siblingAuth := *auth
	siblingAuth.ProjectID = &sibling
	siblingCtx := contextvalues.SetAuthContext(ctx, &siblingAuth)
	var update usergen.UpdateUserSessionIssuerPayload
	update.ID = in.UserSessionIssuerID.String()
	_, err = service.UpdateUserSessionIssuer(siblingCtx, &update)
	requireOopsCode(t, err, oops.CodeNotFound)
	var deletion usergen.DeleteUserSessionIssuerPayload
	deletion.ID = in.UserSessionIssuerID.String()
	requireOopsCode(t, service.DeleteUserSessionIssuer(siblingCtx, &deletion), oops.CodeNotFound)
	_, err = service.UpdateUserSessionIssuer(ctx, &update)
	requireOopsCode(t, err, oops.CodeConflict)
	requireOopsCode(t, service.DeleteUserSessionIssuer(ctx, &deletion), oops.CodeConflict)

	// Organization mutation endpoints must not probe project-owned bindings.
	var orgUpdate orgusersgen.UpdateIssuerPayload
	orgUpdate.ID = in.UserSessionIssuerID.String()
	_, err = service.UpdateIssuer(ctx, &orgUpdate)
	requireOopsCode(t, err, oops.CodeNotFound)
	var orgDeletion orgusersgen.DeleteIssuerPayload
	orgDeletion.ID = in.UserSessionIssuerID.String()
	requireOopsCode(t, service.DeleteIssuer(ctx, &orgDeletion), oops.CodeNotFound)
}
