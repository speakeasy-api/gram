package mcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
)

// ErrWorkloadIssuerUntrusted and ErrWorkloadNotAdmitted expose the workload
// admission sentinels, so a test can tell which stage refused an assertion.
var (
	ErrWorkloadIssuerUntrusted = errWorkloadIssuerUntrusted
	ErrWorkloadNotAdmitted     = errWorkloadNotAdmitted
)

// AdmitWorkloadAssertion runs the workload grant's verification stages
// against real rows with the given verifier and an unlimited issuer lookup
// budget.
func AdmitWorkloadAssertion(
	ctx context.Context,
	db *pgxpool.Pool,
	verifier *workload.Verifier,
	endpoint *ResolvedMcpEndpoint,
	audiences []string,
	raw string,
) error {
	grant := &workloadGrant{
		issuers:    newWorkloadIssuerAdmission(workloadIssuerStoreLookup(db), allowAllWorkloadLookups),
		identities: workloadIdentityStoreLookup(db),
		verifier:   verifier,
		deviceKeys: workloadDeviceKeyStoreLookup(db),
	}
	_, err := admitWorkloadAssertion(ctx, grant, endpoint.workloadTenancy(), audiences, raw)
	return err
}

// VerifyRemoteGrant exposes the grant hook to tests.
func (s *Service) VerifyRemoteGrant(ctx context.Context, grant remotesessions.RemoteGrant) time.Time {
	return s.verifyRemoteGrant(ctx, grant)
}

// VerifyRemoteGrantOn runs the probe step against an endpoint the test resolved itself.
// It preserves ctx, including its deadline; unlike VerifyRemoteGrant it does not
// start detached work with a fresh callback budget.
func (s *Service) VerifyRemoteGrantOn(ctx context.Context, endpoint *ResolvedMcpEndpoint, grant remotesessions.RemoteGrant) {
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+grant.ParentChallengeID)
	if err != nil {
		return
	}
	s.probeRemoteGrant(ctx, s.logger, endpoint, challengeState, grant)
}

// RemoteChallengeManager is the manager the service registered its grant hook on.
func (s *Service) RemoteChallengeManager() *remotesessions.ChallengeManager {
	return s.remoteChallengeMgr
}

// FailAIToolBlockedIDsRead makes the "does this organization block anything?"
// read behind the gateway block check fail with err.
func (s *Service) FailAIToolBlockedIDsRead(err error) {
	s.aiToolBlockReads.blockedTargetIDs = func(context.Context, *agentrepo.Queries, string) ([]string, error) {
		return nil, err
	}
}

// FailAIToolCatalogRead makes the scan-target catalog read behind the gateway
// block check fail with err, leaving the blocked-ids read intact.
func (s *Service) FailAIToolCatalogRead(err error) {
	s.aiToolBlockReads.catalog = func(context.Context, *agentrepo.Queries, string) (*aitargets.OrganizationList, error) {
		return nil, err
	}
}

// SetRemoteSessionRecheckPacing swaps the sweep's per-host limiter rate and claim batch for a test.
func (s *Service) SetRemoteSessionRecheckPacing(rate ratelimit.Rate, batch int32) {
	r := s.remoteSessionRecheck
	r.batch = batch
	if r.limiterStore != nil {
		r.limiter = ratelimit.New(r.limiterStore, "remote_session_recheck_host", rate)
	}
}

// SetRiskScanEvaluator replaces observation only in the test binary.
func (s *Service) SetRiskScanEvaluator(evaluator *mcpriskscan.Evaluator) {
	s.scanEvaluator = evaluator
}

// SetPosthog replaces the product analytics client only in the test binary.
func (s *Service) SetPosthog(p *posthog.Posthog) {
	s.posthog = p
}

// SetPrepareMandatory replaces the shadow's caller admission, so a test can
// count or delay admissions.
func (s *Service) SetPrepareMandatory(prepare func(context.Context) (authz.MandatoryAuthorization, error)) {
	s.prepareMandatory = prepare
}

// ConnectionRecoveryLink names what a test connection link binds.
type ConnectionRecoveryLink struct {
	OrganizationID string
	UserID         string
	ProjectID      uuid.UUID
	McpServerID    uuid.UUID
	AuthClientID   uuid.UUID
	RegistrationID uuid.UUID
}

// IssueConnectionRecovery stores a connection link and returns its path.
func (s *Service) IssueConnectionRecovery(ctx context.Context, link ConnectionRecoveryLink) (string, error) {
	token, err := generateOpaqueToken()
	if err != nil {
		return "", err
	}
	err = s.connectionRecoveryCache.Store(ctx, connectionRecovery{
		ID: token, OrganizationID: link.OrganizationID, UserID: link.UserID, ProjectID: link.ProjectID,
		UserSessionIssuerID: uuid.New(), McpServerID: link.McpServerID, McpSlug: "server", ServerName: "Server",
		AuthClientID: link.AuthClientID, RegistrationID: link.RegistrationID, CreatedAt: time.Now(),
	})
	if err != nil {
		return "", fmt.Errorf("store connection link: %w", err)
	}
	return connectionRecoveryPath + token, nil
}

// SetBrowserSession replaces how a browser's session token authenticates.
func (s *Service) SetBrowserSession(authenticate func(context.Context, string) (context.Context, error)) {
	s.browserSession = authenticate
}

type fixedPermitter bool

func (p fixedPermitter) Permitted(context.Context, authClientPermitRequest) (bool, error) {
	return bool(p), nil
}

// SetAuthClientPermitted answers every auth client permission check with allowed.
func (s *Service) SetAuthClientPermitted(allowed bool) {
	s.authClientPermitter = fixedPermitter(allowed)
}

// RemoteLoginState reads a started remote login by its state parameter.
func (s *Service) RemoteLoginState(ctx context.Context, state string) (remotesessions.RemoteLoginState, error) {
	remote, err := s.remoteLoginCache.Get(ctx, "remoteLogin:"+state)
	if err != nil {
		return remote, fmt.Errorf("load remote login: %w", err)
	}
	return remote, nil
}

// VerifyConnectionRecoveryCompletion runs the completion check of a
// connection link's login.
func (s *Service) VerifyConnectionRecoveryCompletion(r *http.Request, remote remotesessions.RemoteLoginState) (bool, error) {
	return s.verifyConnectionRecoveryCompletion(r, remote)
}

// ConnectionRecoveryRoute and ConnectionRecoveryDonePath expose the link's routes.
const (
	ConnectionRecoveryRoute    = connectionRecoveryRoute
	ConnectionRecoveryDonePath = connectionRecoveryDonePath
)

// ConnectionSubject exposes whose stored connection a typed auth client
// serves for the caller in ctx.
func ConnectionSubject(ctx context.Context, subject urn.SessionSubject) urn.SessionSubject {
	return connectionSubject(ctx, subject)
}

// ConnectionRecoveryErrorFor runs the gate's connection answer for a direct
// endpoint whose typed auth client resolved to clientID.
func (s *Service) ConnectionRecoveryErrorFor(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, clientID, registrationID uuid.UUID, clientType string) (error, bool) {
	res := gapResolution("")
	res.outcome = authClientResolved
	res.clientID = clientID
	res.clientType = clientType
	res.serverName = "Linear"
	authentication := &issuerGateAuthentication{
		endpoint: endpoint, protectedResourceURL: "", mcpURL: "", surface: "", subject: subject, refreshableUserSession: false,
		chosenClient: uuid.NullUUID{UUID: registrationID, Valid: true}, chosenNoAuth: false, authClient: &res,
	}
	return s.connectionRecoveryError(ctx, authentication)
}
