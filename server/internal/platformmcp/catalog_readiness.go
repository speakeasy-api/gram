//nolint:exhaustruct,wrapcheck // Readiness probes intentionally omit documented optional fields and preserve typed errors.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	catalogProbeTimeout     = 15 * time.Second
	catalogProbeMaxResponse = 1 << 20
	catalogReadinessTTL     = time.Minute
)

var errCatalogProbeResponseTooLarge = errors.New("platform mcp catalogue readiness response too large")

// CatalogReadinessProber verifies a browser-catalogue registration through the
// same persisted Remote MCP source and remote-session authorization used at
// normal runtime. Its inputs come exclusively from a lifecycle-owned
// registration, never from an MCP caller.
type CatalogReadinessProber interface {
	ProbeCatalogReadiness(ctx context.Context, principal Principal, projectID, registrationID, remoteMCPServerID, userSessionIssuerID, connectionID, generation uuid.UUID) (ProviderReadinessProbeResult, error)
}

// RemoteMCPReadinessProber is the generic browser-catalogue readiness path. It
// intentionally has no provider descriptor: source URL, configured headers and
// attached remote identity provider are all read from the persisted Remote MCP
// resources created during registration.
type RemoteMCPReadinessProber struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	enc      *encryption.Client
	policy   *guardian.Policy
	sessions *remotesessions.ChallengeManager
}

func NewRemoteMCPReadinessProber(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, policy *guardian.Policy, sessions *remotesessions.ChallengeManager) *RemoteMCPReadinessProber {
	return &RemoteMCPReadinessProber{logger: logger, db: db, enc: enc, policy: policy, sessions: sessions}
}

func (p *RemoteMCPReadinessProber) ProbeCatalogReadiness(ctx context.Context, principal Principal, projectID, registrationID, remoteMCPServerID, userSessionIssuerID, connectionID, generation uuid.UUID) (ProviderReadinessProbeResult, error) {
	if p == nil || p.db == nil || p.enc == nil || p.policy == nil || p.sessions == nil || principal.UserID == "" || principal.OrganizationID == "" || projectID == uuid.Nil || registrationID == uuid.Nil || remoteMCPServerID == uuid.Nil || userSessionIssuerID == uuid.Nil || connectionID == uuid.Nil || generation == uuid.Nil {
		return ProviderReadinessProbeResult{}, ErrReadinessInvalid
	}
	if _, err := projectsrepo.New(p.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: principal.OrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProviderReadinessProbeResult{}, ErrReadinessInvalid
		}
		return ProviderReadinessProbeResult{}, fmt.Errorf("validate registered Remote MCP project: %w", err)
	}
	remote, err := remotemcprepo.New(p.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: remoteMCPServerID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderReadinessProbeResult{}, ErrReadinessInvalid
	}
	if err != nil {
		return ProviderReadinessProbeResult{}, fmt.Errorf("load registered Remote MCP source: %w", err)
	}
	if remote.TransportType != "streamable-http" || remote.Url == "" {
		return ProviderReadinessProbeResult{}, ErrReadinessInvalid
	}
	headers, err := p.loadHeaders(ctx, remote.ID)
	if err != nil {
		return ProviderReadinessProbeResult{}, fmt.Errorf("load registered Remote MCP headers: %w", err)
	}
	for _, header := range headers {
		if header.IsRequired && (!header.Value.Valid || header.Value.String == "") {
			return p.result(principal, registrationID, ReadinessNeedsConfiguration, "required_header_missing", remotesessions.ResolvedAuthorization{}, "no_client"), nil
		}
		if header.ValueFromRequestHeader.Valid && header.ValueFromRequestHeader.String != "" {
			return p.result(principal, registrationID, ReadinessNeedsConfiguration, "request_header_not_supported", remotesessions.ResolvedAuthorization{}, "no_client"), nil
		}
	}

	clients, err := p.sessions.ListClients(ctx, projectID, principal.OrganizationID, userSessionIssuerID)
	if err != nil {
		return ProviderReadinessProbeResult{}, fmt.Errorf("list configured upstream identity providers: %w", err)
	}
	if len(clients) == 0 {
		state, evidence := p.probe(ctx, remote.Url, headers, "")
		if state == ReadinessUnauthorized {
			return p.result(principal, registrationID, ReadinessNeedsConfiguration, "upstream_identity_provider_not_configured", remotesessions.ResolvedAuthorization{}, "no_client"), nil
		}
		return p.result(principal, registrationID, state, evidence, remotesessions.ResolvedAuthorization{}, "anonymous"), nil
	}
	if len(clients) != 1 {
		return p.result(principal, registrationID, ReadinessNeedsConfiguration, "multiple_upstream_identity_providers", remotesessions.ResolvedAuthorization{}, "no_client"), nil
	}
	authorization, err := p.sessions.ResolveAuthorization(ctx, projectID, principal.OrganizationID, userSessionIssuerID, clients[0].RemoteSessionIssuerID, urn.NewUserSubject(principal.UserID), remote.Url)
	if errors.Is(err, remotesessions.ErrNoRemoteSessionClientBinding) {
		return p.result(principal, registrationID, ReadinessNeedsConfiguration, "upstream_identity_provider_not_configured", remotesessions.ResolvedAuthorization{RemoteSessionIssuerID: clients[0].RemoteSessionIssuerID}, "no_client"), nil
	}
	if errors.Is(err, remotesessions.ErrNoValidToken) {
		return p.result(principal, registrationID, ReadinessNeedsGramAuthorization, "upstream_authorization_required", remotesessions.ResolvedAuthorization{RemoteSessionIssuerID: clients[0].RemoteSessionIssuerID}, "no_session"), nil
	}
	if err != nil {
		return ProviderReadinessProbeResult{}, fmt.Errorf("resolve registered Remote MCP authorization: %w", err)
	}

	state, evidence := p.probe(ctx, remote.Url, headers, authorization.AccessToken)
	return p.result(principal, registrationID, state, evidence, authorization, ""), nil
}

func (p *RemoteMCPReadinessProber) loadHeaders(ctx context.Context, remoteMCPServerID uuid.UUID) ([]remotemcprepo.RemoteMcpServerHeader, error) {
	headers, err := remotemcprepo.New(p.db).ListHeadersByServerID(ctx, remoteMCPServerID)
	if err != nil {
		return nil, err
	}
	for index := range headers {
		if !headers[index].IsSecret || !headers[index].Value.Valid || headers[index].Value.String == "" {
			continue
		}
		value, err := p.enc.Decrypt(headers[index].Value.String)
		if err != nil {
			return nil, fmt.Errorf("decrypt registered Remote MCP header: %w", err)
		}
		headers[index].Value.String = value
	}
	return headers, nil
}

func (p *RemoteMCPReadinessProber) result(principal Principal, registrationID uuid.UUID, state ReadinessState, evidence string, authorization remotesessions.ResolvedAuthorization, absence string) ProviderReadinessProbeResult {
	now := time.Now().UTC()
	return ProviderReadinessProbeResult{AuthorizationIdentity: ProviderAuthorizationIdentity{OrganizationID: principal.OrganizationID, Subject: urn.NewUserSubject(principal.UserID), RegistrationID: registrationID, RemoteSessionID: authorization.RemoteSessionID, RemoteSessionUpdatedAt: authorization.RemoteSessionUpdatedAt, RemoteSessionClientID: authorization.RemoteSessionClientID, RemoteSessionIssuerID: authorization.RemoteSessionIssuerID, Absence: absence}, State: state, EvidenceCode: evidence, CheckedAt: now, ExpiresAt: now.Add(catalogReadinessTTL)}
}

func (p *RemoteMCPReadinessProber) probe(ctx context.Context, remoteURL string, headers []remotemcprepo.RemoteMcpServerHeader, token string) (ReadinessState, string) {
	ctx, cancel := context.WithTimeout(ctx, catalogProbeTimeout)
	defer cancel()
	httpClient := p.policy.Client()
	httpClient.Timeout = catalogProbeTimeout
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	baseTransport := httpClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	roundTripper := &catalogAuthorizationRoundTripper{
		base:         baseTransport,
		token:        token,
		headers:      headers,
		unauthorized: atomic.Bool{},
		tooLarge:     atomic.Bool{},
	}
	httpClient.Transport = roundTripper

	client := mcp.NewClient(&mcp.Implementation{Name: "speakeasy-aicp-platform-mcp-readiness", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: remoteURL, HTTPClient: httpClient, MaxRetries: 0, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return catalogProbeFailure(err, roundTripper)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.ListTools(ctx, nil); err != nil {
		return catalogProbeFailure(err, roundTripper)
	}
	return ReadinessReady, "tools_list_ok"
}

func catalogProbeFailure(err error, roundTripper *catalogAuthorizationRoundTripper) (ReadinessState, string) {
	if roundTripper.unauthorized.Load() {
		return ReadinessUnauthorized, "upstream_authorization_rejected"
	}
	if roundTripper.tooLarge.Load() || errors.Is(err, errCatalogProbeResponseTooLarge) {
		return ReadinessUnsupported, "response_too_large"
	}
	return ReadinessUnreachable, "probe_failed"
}

type catalogAuthorizationRoundTripper struct {
	base         http.RoundTripper
	token        string
	headers      []remotemcprepo.RemoteMcpServerHeader
	unauthorized atomic.Bool
	tooLarge     atomic.Bool
}

func (rt *catalogAuthorizationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	if rt.token != "" {
		request.Header.Set("Authorization", "Bearer "+rt.token)
	}
	for _, header := range rt.headers {
		if header.Value.Valid && header.Value.String != "" {
			request.Header.Set(header.Name, header.Value.String)
		}
	}
	response, err := rt.base.RoundTrip(request)
	if err != nil {
		return nil, fmt.Errorf("send registered Remote MCP readiness request: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		rt.unauthorized.Store(true)
	}
	if response.ContentLength > catalogProbeMaxResponse {
		rt.tooLarge.Store(true)
		_ = response.Body.Close()
		return nil, errCatalogProbeResponseTooLarge
	}
	response.Body = &catalogBoundedReadCloser{ReadCloser: response.Body, remaining: catalogProbeMaxResponse, exceeded: &rt.tooLarge}
	return response, nil
}

type catalogBoundedReadCloser struct {
	io.ReadCloser
	remaining int64
	exceeded  *atomic.Bool
}

func (r *catalogBoundedReadCloser) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		var extra [1]byte
		n, err := r.ReadCloser.Read(extra[:])
		if n > 0 {
			r.exceeded.Store(true)
			return 0, errCatalogProbeResponseTooLarge
		}
		return n, err
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	n, err := r.ReadCloser.Read(buffer)
	r.remaining -= int64(n)
	return n, err
}
