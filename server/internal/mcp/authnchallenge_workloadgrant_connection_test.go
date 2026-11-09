package mcp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

func TestWorkloadAssertionGrant_RecordsSignIn(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultSignedIn)
	require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
	require.Equal(t, oauthwire.GrantTypeJWTBearer, row.GrantType)
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), row.SubjectURN)
	require.Equal(t, mcpconnectionattempts.AppWorkload.String(), row.AppKey)
	require.NotEmpty(t, row.UserSessionID)
	require.Len(t, rec.Rows(), 1)
}

func TestWorkloadAssertionGrant_RecordsUnadmittedSubject(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)

	const stranger = "repo:someone-else/app:ref:refs/heads/main"
	claims := oauthtest.WorkloadClaims(f.issuer, stranger, f.advertisedIssuer)
	requireWorkloadGrantRefused(t, f.exchange(t, oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims), f.resource))

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSubjectNotAdmitted)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, stranger).String(), row.SubjectURN)
	require.Equal(t, oauthwire.GrantTypeJWTBearer, row.GrantType)
}

// Before the signature verifies, nothing proves a real workload, so a refusal writes no row.
func TestWorkloadAssertionGrant_UnverifiedRefusalsWriteNoRow(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)

	claims := oauthtest.WorkloadClaims(f.issuer, f.subject, f.advertisedIssuer)
	claims.Audience = jwt.Audience{f.advertisedIssuer, "https://elsewhere.example.com"}
	requireWorkloadGrantRefused(t, f.exchange(t, oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims), f.resource))

	requireWorkloadGrantRefused(t, f.exchange(t, forgeSignature(t, f.assertion(t, f.advertisedIssuer)), f.resource))
	requireWorkloadGrantRefused(t, f.exchange(t, "not-a-jwt", f.resource))

	require.Equal(t, http.StatusBadRequest, f.exchange(t, "", f.resource).Code)
	require.Equal(t, http.StatusBadRequest, f.exchange(t, f.assertion(t, f.advertisedIssuer)).Code)
	rec.RequireNothing()
}

// forgeSignature changes one byte of a compact JWS signature.
func forgeSignature(t *testing.T, compact string) string {
	t.Helper()

	parts := strings.Split(compact, ".")
	require.Len(t, parts, 3)
	signature := []byte(parts[2])
	const forged = 8
	if signature[forged] == 'A' {
		signature[forged] = 'B'
	} else {
		signature[forged] = 'A'
	}
	parts[2] = string(signature)
	return strings.Join(parts, ".")
}

// An organization that takes no upstream choices yet refuses one the app names, after the workload is proven.
func TestWorkloadAssertionGrant_RecordsUpstreamChoice(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)

	form := url.Values{}
	form.Set("grant_type", oauthwire.GrantTypeJWTBearer)
	form.Set("assertion", f.assertion(t, f.advertisedIssuer))
	form.Set("resource", f.resource)
	form.Set("upstream_auth_client", uuid.NewString()+":none")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+f.fx.toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", f.fx.toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, routeCtx))
	var oauthErr *oops.OAuthError
	require.ErrorAs(t, f.ti.service.HandleToken(httptest.NewRecorder(), req), &oauthErr)
	require.Equal(t, http.StatusBadRequest, oauthErr.Status)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeUpstreamCredentialChoice)
	require.Equal(t, "choice_not_accepted", row.SubReason)
	require.Equal(t, string(mcpconnectionattempts.WhoApp), row.WhoFixes)
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), row.SubjectURN)
	require.Empty(t, f.workloadSessions(t))
}

func TestWorkloadAssertionGrant_RecordsSuspendedAgent(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)
	_, err := agentsrepo.New(f.ti.conn).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
	require.NoError(t, err)

	requireWorkloadGrantRefused(t, f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource))

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeNoServerAccess)
	require.Equal(t, "agent_admission_denied", row.SubReason)
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), row.SubjectURN)
}

func TestWorkloadAssertionGrant_LogsOffRecordsNothing(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	rec := mcpconnectionattemptstest.NewLogsOff(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)

	require.Equal(t, http.StatusOK, f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource).Code)
	claims := oauthtest.WorkloadClaims(f.issuer, "repo:someone-else/app:ref:refs/heads/main", f.advertisedIssuer)
	requireWorkloadGrantRefused(t, f.exchange(t, oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims), f.resource))
	rec.RequireNothing()
}

// Refusals after the signature verifies are the workload's own: each writes exactly one row naming it.
func TestWorkloadAssertionGrant_RecordsRefusalsAfterSignature(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)

	expired := oauthtest.WorkloadClaims(f.issuer, f.subject, f.advertisedIssuer)
	past := time.Now().Add(-30 * time.Minute)
	expired.Expiry, expired.NotBefore, expired.IssuedAt = jwt.NewNumericDate(past.Add(5*time.Minute)), jwt.NewNumericDate(past), jwt.NewNumericDate(past)
	requireWorkloadGrantRefused(t, f.exchange(t, oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", expired), f.resource))

	requireWorkloadGrantRefused(t, f.exchange(t, f.assertion(t, f.advertisedIssuer+"/revoke"), f.resource))

	replayed := f.assertion(t, f.advertisedIssuer)
	require.Equal(t, http.StatusOK, f.exchange(t, replayed, f.resource).Code)
	requireWorkloadGrantRefused(t, f.exchange(t, replayed, f.resource))

	subject := urn.NewWorkloadSubject(f.issuerID, f.subject).String()
	for _, reason := range []string{"assertion_expired", "assertion_audience_mismatch", "assertion_replayed"} {
		var matched []mcpconnectionattempts.Row
		for _, row := range rec.Rows() {
			if row.SubReason == reason {
				matched = append(matched, row)
			}
		}
		require.Len(t, matched, 1, reason)
		require.Equal(t, mcpconnectionattempts.CodeAssertionInvalid.String(), matched[0].Reason, reason)
		require.Equal(t, subject, matched[0].SubjectURN, reason)
	}
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultSignedIn)
	require.Len(t, rec.Rows(), 4)
}

// failingReplayGuard is a replay store that cannot be reached.
type failingReplayGuard struct{}

func (failingReplayGuard) MaxHold() time.Duration { return 24 * time.Hour }

func (failingReplayGuard) Reserve(context.Context, replay.Key, time.Time) (bool, error) {
	return false, errors.New("replay store unreachable")
}

// A replay store outage on a validly signed assertion is our failure, and is recorded.
func TestWorkloadAssertionGrant_RecordsReplayStoreOutage(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)
	require.NoError(t, f.ti.service.SetWorkloadReplayGuard(failingReplayGuard{}))

	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeAssertionCheckUnavailable)
	require.Equal(t, "assertion_replay_store_unavailable", row.SubReason)
	require.Equal(t, string(mcpconnectionattempts.WhoSpeakeasy), row.WhoFixes)
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), row.SubjectURN)
	require.Len(t, rec.Rows(), 1)

	requireWorkloadGrantRefused(t, f.exchange(t, forgeSignature(t, f.assertion(t, f.advertisedIssuer)), f.resource))
	require.Len(t, rec.Rows(), 1, "a forged signature writes no row")
}
