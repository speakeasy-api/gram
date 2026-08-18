// Package remotesessionprovider implements reviewed Platform MCP provider
// readiness over shared remote-session authorization. It intentionally accepts
// all provider routing only through constructor-injected descriptors.
package remotesessionprovider

import (
	"context"
	"errors"
	"fmt"
	"io"

	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	probeTimeout      = 15 * time.Second
	maxResponseBytes  = 1 << 20
	readinessLifetime = time.Minute
)

var (
	errResponseTooLarge = errors.New("platform mcp provider response too large")
	errRedirectRejected = errors.New("platform mcp provider redirect rejected")
)

// Descriptor is the server-owned reviewed configuration for one provider.
// It is not populated from MCP tool input or persisted Platform registration
// metadata.
type Descriptor struct {
	ProviderKey                string
	RemoteSessionIssuerID      uuid.UUID
	StreamableHTTPURL          string
	ProviderSetupCompletionURL string
	Resource                   string
	// TestOnlyAllowedCIDRBlocks is a narrowly-scoped test-fixture escape hatch
	// for a TLS server on loopback. Production composition must leave it empty.
	TestOnlyAllowedCIDRBlocks []string
	// TestOnlyReadinessLifetime extends local fixture evidence only so a human
	// can complete the local OAuth and first-use walkthrough. Production
	// composition must leave it zero, preserving the default lifetime.
	TestOnlyReadinessLifetime time.Duration
}

// Adapter resolves one reviewed provider authorization and probes only its
// configured Streamable HTTP endpoint.
// ProviderClientConfigurator ensures the reviewed remote-session client exists
// before a setup handoff is consumed. Implementations own their persistence and
// provider registration behavior; the adapter stays responsible only for the
// shared authorization and readiness paths.
type ProviderClientConfigurator interface {
	ConfigureProviderClient(ctx context.Context, request platformmcp.ProviderSetupRequest, descriptor Descriptor) error
}

type Adapter struct {
	policy       *guardian.Policy
	sessions     *remotesessions.ChallengeManager
	descriptor   Descriptor
	configurator ProviderClientConfigurator
}

// New accepts an optional configurator so existing reviewed adapters retain
// validate-only preflight behavior. Local fixture composition supplies the one
// approved configurator; normal startup supplies none.
func New(policy *guardian.Policy, sessions *remotesessions.ChallengeManager, descriptor Descriptor, configurator ...ProviderClientConfigurator) *Adapter {
	var configured ProviderClientConfigurator
	if len(configurator) > 0 {
		configured = configurator[0]
	}
	return &Adapter{
		policy:       policy,
		sessions:     sessions,
		descriptor:   descriptor,
		configurator: configured,
	}
}

func (a *Adapter) ProviderKey() string {
	if a == nil || !validDescriptor(a.descriptor) {
		return ""
	}
	return a.descriptor.ProviderKey
}

// PreflightSetup confirms that a reviewed shared client binding exists before
// a Platform handoff is consumed. It never starts OAuth or makes provider egress.
func (a *Adapter) PreflightSetup(ctx context.Context, request platformmcp.ProviderSetupRequest) error {
	if err := validateSetupRequest(request); err != nil {
		return err
	}
	if request.HandoffID != uuid.Nil {
		return platformmcp.ErrSetupHandoffInvalid
	}
	descriptor := a.descriptor
	if !validDescriptor(descriptor) || a.sessions == nil {
		return platformmcp.ErrProviderAdapterUnavailable
	}
	if a.configurator != nil {
		if err := a.configurator.ConfigureProviderClient(ctx, request, descriptor); err != nil {
			return fmt.Errorf("configure reviewed provider client: %w", err)
		}
	}
	_, err := a.reviewedClient(ctx, request, descriptor)
	return err
}

func (a *Adapter) reviewedClient(ctx context.Context, request platformmcp.ProviderSetupRequest, descriptor Descriptor) (remotesessions.Client, error) {
	clients, err := a.sessions.ListClients(ctx, request.ProjectID, request.OrganizationID, request.UserSessionIssuerID)
	if err != nil {
		return remotesessions.Client{}, fmt.Errorf("list reviewed remote-session clients: %w", err)
	}
	var match remotesessions.Client
	for _, client := range clients {
		if client.ID == uuid.Nil || client.RemoteSessionIssuerID != descriptor.RemoteSessionIssuerID {
			continue
		}
		if match.ID != uuid.Nil {
			return remotesessions.Client{}, fmt.Errorf("%w: multiple reviewed remote-session clients", platformmcp.ErrProviderAdapterUnavailable)
		}
		match = client
	}
	if match.ID == uuid.Nil {
		return remotesessions.Client{}, platformmcp.ErrProviderAdapterUnavailable
	}
	return match, nil
}

// BeginSetup mints the provider authorization URL after a trusted lifecycle
// caller consumes the handoff. The returned URL is transient; the remote-login
// callback writes tokens through the shared remote-session flow.
func (a *Adapter) BeginSetup(ctx context.Context, request platformmcp.ProviderSetupRequest) (platformmcp.ProviderSetupResult, error) {
	if err := validateSetupRequest(request); err != nil || request.HandoffID == uuid.Nil {
		return platformmcp.ProviderSetupResult{}, platformmcp.ErrSetupHandoffInvalid
	}
	descriptor := a.descriptor
	if !validDescriptor(descriptor) || a.sessions == nil {
		return platformmcp.ProviderSetupResult{}, platformmcp.ErrProviderAdapterUnavailable
	}
	client, err := a.reviewedClient(ctx, request, descriptor)
	if err != nil {
		return platformmcp.ProviderSetupResult{}, err
	}
	subject := urn.NewUserSubject(request.UserID)
	authorizationURL, err := a.sessions.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  request.HandoffID.String(),
		ProjectID:           request.ProjectID,
		OrganizationID:      request.OrganizationID,
		UserSessionIssuerID: request.UserSessionIssuerID,
		Subject:             &subject,
		McpSlug:             request.MCPSlug,
		RouteBase:           "mcp",
		FinalRedirectURI:    descriptor.ProviderSetupCompletionURL,
		Resource:            descriptor.Resource,
		AutoRefresh:         nil,
	}, client)
	if err != nil {
		return platformmcp.ProviderSetupResult{}, fmt.Errorf("build reviewed provider authorization URL: %w", err)
	}
	return platformmcp.ProviderSetupResult{AuthorizationURL: authorizationURL}, nil
}

func (a *Adapter) ProbeReadiness(ctx context.Context, request platformmcp.ProviderReadinessProbeRequest) (platformmcp.ProviderReadinessProbeResult, error) {
	if err := validateReadinessRequest(request); err != nil {
		return platformmcp.ProviderReadinessProbeResult{}, err
	}
	descriptor := a.descriptor
	if !validDescriptor(descriptor) || a.sessions == nil || a.policy == nil {
		return platformmcp.ProviderReadinessProbeResult{}, platformmcp.ErrProviderAdapterUnavailable
	}

	authorization, err := a.sessions.ResolveAuthorization(
		ctx,
		request.ProjectID,
		request.OrganizationID,
		request.UserSessionIssuerID,
		descriptor.RemoteSessionIssuerID,
		urn.NewUserSubject(request.UserID),
		descriptor.Resource,
	)
	if errors.Is(err, remotesessions.ErrNoRemoteSessionClientBinding) {
		return a.readinessResult(platformmcp.ReadinessNeedsConfiguration, "no_reviewed_client", request, remotesessions.ResolvedAuthorization{
			AccessToken:            "",
			RemoteSessionID:        uuid.Nil,
			RemoteSessionUpdatedAt: time.Time{},
			RemoteSessionClientID:  uuid.Nil,
			RemoteSessionIssuerID:  descriptor.RemoteSessionIssuerID,
		}, "no_client"), nil
	}
	if errors.Is(err, remotesessions.ErrNoValidToken) {
		return a.readinessResult(platformmcp.ReadinessNeedsGramAuthorization, "no_valid_authorization", request, remotesessions.ResolvedAuthorization{
			AccessToken:            "",
			RemoteSessionID:        uuid.Nil,
			RemoteSessionUpdatedAt: time.Time{},
			RemoteSessionClientID:  uuid.Nil,
			RemoteSessionIssuerID:  descriptor.RemoteSessionIssuerID,
		}, "no_session"), nil
	}
	if err != nil {
		return platformmcp.ProviderReadinessProbeResult{}, fmt.Errorf("resolve reviewed provider authorization: %w", err)
	}

	state, evidence := a.probe(ctx, descriptor, authorization.AccessToken)
	if state != platformmcp.ReadinessReady {
		return a.readinessResult(state, evidence, request, authorization, ""), nil
	}
	return a.readinessResult(platformmcp.ReadinessReady, "tools_list_ok", request, authorization, ""), nil
}

func (a *Adapter) probe(ctx context.Context, descriptor Descriptor, token string) (platformmcp.ReadinessState, string) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	httpClient := a.policy.Client(guardian.WithAllowedCIDRBlocks(descriptor.TestOnlyAllowedCIDRBlocks...))
	httpClient.Timeout = probeTimeout
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errRedirectRejected }
	authRT := &authorizationRoundTripper{
		base:                  httpClient.Transport,
		authorization:         "Bearer " + token,
		authorizationRejected: atomic.Bool{},
		responseTooLarge:      atomic.Bool{},
	}
	httpClient.Transport = authRT

	client := mcp.NewClient(&mcp.Implementation{
		Name:       "speakeasy-aicp-platform-mcp-readiness",
		Title:      "",
		Version:    "1.0.0",
		WebsiteURL: "",
		Icons:      nil,
	}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             descriptor.StreamableHTTPURL,
		HTTPClient:           httpClient,
		MaxRetries:           0,
		OAuthHandler:         nil,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return normalizedProbeFailure(err, authRT)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.ListTools(ctx, nil); err != nil {
		return normalizedProbeFailure(err, authRT)
	}
	return platformmcp.ReadinessReady, "tools_list_ok"
}

func (a *Adapter) readinessResult(state platformmcp.ReadinessState, evidence string, request platformmcp.ProviderReadinessProbeRequest, authorization remotesessions.ResolvedAuthorization, absence string) platformmcp.ProviderReadinessProbeResult {
	now := time.Now().UTC()
	lifetime := readinessLifetime
	if a != nil && a.descriptor.TestOnlyReadinessLifetime > 0 {
		lifetime = a.descriptor.TestOnlyReadinessLifetime
	}
	return platformmcp.ProviderReadinessProbeResult{
		AuthorizationIdentity: platformmcp.ProviderAuthorizationIdentity{
			OrganizationID:         request.OrganizationID,
			Subject:                urn.NewUserSubject(request.UserID),
			RegistrationID:         request.RegistrationID,
			RemoteSessionID:        authorization.RemoteSessionID,
			RemoteSessionUpdatedAt: authorization.RemoteSessionUpdatedAt,
			RemoteSessionClientID:  authorization.RemoteSessionClientID,
			RemoteSessionIssuerID:  authorization.RemoteSessionIssuerID,
			Absence:                absence,
		},
		State:        state,
		EvidenceCode: evidence,
		CheckedAt:    now,
		ExpiresAt:    now.Add(lifetime),
	}
}

func normalizedProbeFailure(err error, authRT *authorizationRoundTripper) (platformmcp.ReadinessState, string) {
	if authRT.authorizationRejected.Load() {
		return platformmcp.ReadinessUnauthorized, "provider_authorization_rejected"
	}
	if authRT.responseTooLarge.Load() || errors.Is(err, errResponseTooLarge) {
		return platformmcp.ReadinessUnsupported, "response_too_large"
	}
	if errors.Is(err, errRedirectRejected) {
		return platformmcp.ReadinessUnsupported, "redirect_rejected"
	}
	return platformmcp.ReadinessUnreachable, "probe_failed"
}

func validateSetupRequest(request platformmcp.ProviderSetupRequest) error {
	if request.UserID == "" || request.OrganizationID == "" || request.ProjectID == uuid.Nil || request.RegistrationID == uuid.Nil || request.UserSessionIssuerID == uuid.Nil || request.MCPSlug == "" || !validConnectionPair(request.ConnectionID, request.Generation) {
		return platformmcp.ErrSetupHandoffInvalid
	}
	return nil
}

// validConnectionPair accepts a complete connection pair or none at all. A
// connection-less caller is identified by its user, which every request already
// carries; a half-populated pair is an incomplete identity, matching the
// all-or-nothing CHECK the connection columns carry.
func validConnectionPair(connectionID, generation uuid.UUID) bool {
	return (connectionID == uuid.Nil) == (generation == uuid.Nil)
}

func validateReadinessRequest(request platformmcp.ProviderReadinessProbeRequest) error {
	if request.UserID == "" || request.OrganizationID == "" || request.ProjectID == uuid.Nil || request.RegistrationID == uuid.Nil || request.UserSessionIssuerID == uuid.Nil || request.ConnectionID == uuid.Nil || request.Generation == uuid.Nil {
		return platformmcp.ErrReadinessInvalid
	}
	return nil
}

func validDescriptor(descriptor Descriptor) bool {
	endpoint, endpointErr := url.Parse(descriptor.StreamableHTTPURL)
	completion, completionErr := url.Parse(descriptor.ProviderSetupCompletionURL)
	return endpointErr == nil && completionErr == nil && descriptor.ProviderKey != "" && descriptor.RemoteSessionIssuerID != uuid.Nil && endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.User == nil && completion.Scheme == "https" && completion.Host != "" && completion.User == nil
}

type authorizationRoundTripper struct {
	base                  http.RoundTripper
	authorization         string
	authorizationRejected atomic.Bool
	responseTooLarge      atomic.Bool
}

func (rt *authorizationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", rt.authorization)
	response, err := rt.base.RoundTrip(request)
	if err != nil {
		return nil, fmt.Errorf("send reviewed provider request: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		rt.authorizationRejected.Store(true)
	}
	if response.ContentLength > maxResponseBytes {
		rt.responseTooLarge.Store(true)
		_ = response.Body.Close()
		return nil, errResponseTooLarge
	}
	response.Body = &boundedReadCloser{ReadCloser: response.Body, remaining: maxResponseBytes, exceeded: &rt.responseTooLarge}
	return response, nil
}

type boundedReadCloser struct {
	io.ReadCloser
	remaining int64
	exceeded  *atomic.Bool
}

func (r *boundedReadCloser) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		var extra [1]byte
		n, err := r.ReadCloser.Read(extra[:])
		if n > 0 {
			if r.exceeded != nil {
				r.exceeded.Store(true)
			}
			return 0, errResponseTooLarge
		}
		if errors.Is(err, io.EOF) {
			return 0, io.EOF
		}
		if err != nil {
			return 0, fmt.Errorf("read reviewed provider response overflow byte: %w", err)
		}
		return 0, nil
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	n, err := r.ReadCloser.Read(buffer)
	r.remaining -= int64(n)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read reviewed provider response: %w", err)
	}
	return n, nil
}
