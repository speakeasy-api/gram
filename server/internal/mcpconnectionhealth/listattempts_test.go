package mcpconnectionhealth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (ti *testInstance) listPayload() *gen.ListAttemptsPayload {
	return &gen.ListAttemptsPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()}
}

func TestListAttempts_ReturnsTheProvidersAttemptsWithNames(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	subject := urn.NewUserSubject(ti.userID)
	id := ti.insert(t, ti.org, ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderOAuthInvalidScope,
		mcpconnectionattempts.Subject(&subject), mcpconnectionattempts.Provider("https://linear.app"))

	res, err := ti.service.ListAttempts(ctx, ti.listPayload())
	require.NoError(t, err)
	require.True(t, res.LoggingEnabled)
	require.Equal(t, gen.ConnectionViewerScope("all"), res.ViewerScope)
	require.Len(t, res.Attempts, 1)
	row := res.Attempts[0]
	require.Equal(t, id.String(), row.ID)
	require.Equal(t, gen.ConnectionOutcome("failed"), row.Outcome)
	require.Equal(t, gen.ConnectionStepGroup("service_sign_in"), row.Step)
	require.Equal(t, gen.ConnectionFailureSource("service"), *row.Source)
	require.Equal(t, "linear.app", *row.Service)
	require.Equal(t, "The service refused the access Speakeasy asked for.", *row.Message)
	require.Equal(t, "The service returned OAuth error invalid_scope, unauthorized_client, or invalid_target.", *row.TechnicalMessage)
	require.Equal(t, "Linear", *row.Server.Name)
	require.Equal(t, "gateway", row.Server.Kind)
	require.Equal(t, subject.String(), row.Person.SubjectUrn)
	require.NotNil(t, row.Person.Name)
	require.Nil(t, row.Person.Email)
	require.Equal(t, "Unlisted app", row.App.Name)
}

func TestListAttempts_NeverShowsAnotherOrganizationsAttempts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.insert(t, "org-"+uuid.NewString(), ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken,
		mcpconnectionattempts.UserSessionIssuer(ti.issuer))
	ti.insert(t, ti.org, uuid.New(), ti.now.Add(-time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken,
		mcpconnectionattempts.UserSessionIssuer(ti.issuer))

	res, err := ti.service.ListAttempts(ctx, ti.listPayload())
	require.NoError(t, err)
	require.Empty(t, res.Attempts)

	counts, err := ti.service.GetAttemptCounts(ctx, &gen.GetAttemptCountsPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()})
	require.NoError(t, err)
	require.Zero(t, *counts.TotalAttempts)
}

func TestListAttempts_SaysLoggingIsOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.insert(t, ti.org, ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)
	ti.logsOn.Store(false)

	res, err := ti.service.ListAttempts(ctx, ti.listPayload())
	require.NoError(t, err)
	require.False(t, res.LoggingEnabled)
	require.Nil(t, res.Attempts)

	counts, err := ti.service.GetAttemptCounts(ctx, &gen.GetAttemptCountsPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()})
	require.NoError(t, err)
	require.False(t, counts.LoggingEnabled)
	require.Nil(t, counts.TotalAttempts)
}

func TestListAttempts_NotFoundWhenTheFlagIsOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.flags.SetFlag(feature.FlagMCPConnectionHealth, ti.org, false)

	_, err := ti.service.ListAttempts(ctx, ti.listPayload())
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetSessionActivity(ctx, &gen.GetSessionActivityPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListAttempts_AMemberWithoutLogReadSeesOnlyTheirOwn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead)
	me := urn.NewUserSubject(ti.userID)
	other := urn.NewUserSubject("user-" + uuid.NewString())
	mine := ti.insert(t, ti.org, ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.Subject(&me))
	theirs := ti.insert(t, ti.org, ti.project, ti.now.Add(-2*time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.Subject(&other))

	res, err := ti.service.ListAttempts(ctx, ti.listPayload())
	require.NoError(t, err)
	require.Equal(t, gen.ConnectionViewerScope("own"), res.ViewerScope)
	require.Len(t, res.Attempts, 1)
	require.Equal(t, mine.String(), res.Attempts[0].ID)

	detail, err := ti.service.GetAttempt(ctx, &gen.GetAttemptPayload{Reference: theirs.String()})
	require.NoError(t, err)
	require.False(t, *detail.Found)
}

func TestListAttempts_RejectsAnotherProjectsProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	payload := ti.listPayload()
	payload.UserSessionIssuerID = uuid.NewString()

	_, err := ti.service.ListAttempts(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListAttempts_RejectsARangeOutsideRetention(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	payload := ti.listPayload()
	payload.From = ti.now.Add(-31 * 24 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.ListAttempts(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetAttemptCounts_CountsEachFilterValue(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.insert(t, ti.org, ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)
	ti.insert(t, ti.org, ti.project, ti.now.Add(-2*time.Hour), mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued)

	res, err := ti.service.GetAttemptCounts(ctx, &gen.GetAttemptCountsPayload{
		UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo(),
		Filters: &gen.ConnectionAttemptFilters{Outcomes: []gen.ConnectionOutcome{"failed"}},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), *res.TotalAttempts)
	require.Len(t, res.Facets.Outcomes, 2)
	require.Len(t, res.Facets.Servers, 1)
	require.Equal(t, "Linear", *res.Facets.Servers[0].Label)
	require.Equal(t, "invalid_bearer_token", res.Facets.Reasons[0].Value)
	require.NotEmpty(t, *res.Facets.Reasons[0].Label)
}

func TestListAttempts_AServerLockAndAnotherServerFilterMatchNothing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.insert(t, ti.org, ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderUnavailable)

	payload := ti.listPayload()
	payload.ServerID = new(ti.server.String())
	res, err := ti.service.ListAttempts(ctx, payload)
	require.NoError(t, err)
	require.Len(t, res.Attempts, 1)

	payload.Filters = &gen.ConnectionAttemptFilters{ServerIds: []string{uuid.NewString()}}
	res, err = ti.service.ListAttempts(ctx, payload)
	require.NoError(t, err)
	require.Empty(t, res.Attempts)
}
