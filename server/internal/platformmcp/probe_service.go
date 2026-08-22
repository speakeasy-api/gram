package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// remoteProbeTimeout bounds the whole verification probe — initialize
// handshake, tools/list, and OAuth discovery — so a slow or unresponsive
// user-supplied host cannot hang the tool call. One shot, no retries.
const remoteProbeTimeout = 10 * time.Second

// maxProbeEvidenceToolNames bounds how many tool names one probe carries into
// its evidence. The evidence is shown to a user for confirmation and its
// digest is signed into the receipt, so a hostile server must not be able to
// inflate it without bound; the exact declared count is always reported and a
// clipped listing is recorded as an explicit gap.
const maxProbeEvidenceToolNames = 50

// maxProbeEvidenceFieldBytes bounds each server-declared free-text field
// carried into probe evidence.
const maxProbeEvidenceFieldBytes = 256

// maxProbeResponseBytes caps each HTTP response body the probe reads. The
// probe talks to an arbitrary user-supplied host, so an enormous tools/list
// or initialize response must fail as a bounded size refusal before it is
// materialized, never exhaust probe-process memory.
const maxProbeResponseBytes = 4 << 20

// probeTruncationMarker flags a clipped evidence field so a bounded value can
// never pass as the server's own words.
const probeTruncationMarker = "…[truncated]"

var (
	// ErrProbeEgressDenied reports a probe target the guardian egress policy
	// refuses (private ranges, blocked hosts). It maps to the egress_denied
	// tool result code and deliberately carries no guardian or resolver
	// detail.
	ErrProbeEgressDenied = errors.New("platform mcp probe target denied by egress policy")

	// ErrProbeUnreachable reports a probe target that could not be reached:
	// connect failure, TLS failure, or timeout. It maps to the unreachable
	// tool result code.
	ErrProbeUnreachable = errors.New("platform mcp probe target unreachable")

	// ErrProbeNotMCPServer reports a probe target that answered but did not
	// complete the MCP initialize handshake. It maps to the not_an_mcp_server
	// tool result code.
	ErrProbeNotMCPServer = errors.New("platform mcp probe target is not an mcp server")
)

// ProbeAuthPosture is the authentication posture a verification probe observed
// on a remote MCP server.
type ProbeAuthPosture string

const (
	// ProbeAuthPostureOAuthDiscovered means the server publishes OAuth
	// metadata at the RFC 9728/8414 well-known endpoints.
	ProbeAuthPostureOAuthDiscovered ProbeAuthPosture = "oauth_discovered"

	// ProbeAuthPostureAuthRequired means the server rejected unauthenticated
	// access with a typed auth rejection but publishes no discoverable OAuth
	// metadata.
	ProbeAuthPostureAuthRequired ProbeAuthPosture = "auth_required"

	// ProbeAuthPostureOpen means the server completed the handshake without
	// credentials and publishes no OAuth metadata.
	ProbeAuthPostureOpen ProbeAuthPosture = "open"
)

// ProbeEvidence is what a verification probe observed about a remote MCP
// server. It is disclosed to the user for explicit confirmation before
// registration, and its digest is signed into the probe receipt so what the
// user confirmed is exactly what a registration can redeem.
type ProbeEvidence struct {
	// NormalizedURL is the probed URL after normalizeRemoteURL — the identity
	// the receipt binds and a registration would persist.
	NormalizedURL string `json:"normalized_url"`

	// ServerName is the implementation name the server declared during the
	// initialize handshake, bounded; empty when the handshake did not complete.
	ServerName string `json:"server_name,omitempty"`

	// ServerVersion is the implementation version the server declared during
	// the initialize handshake, bounded; empty when the handshake did not
	// complete.
	ServerVersion string `json:"server_version,omitempty"`

	// ToolCount is the exact number of tools the server declared to an
	// unauthenticated tools/list, and zero when the listing was declined or
	// failed — Gaps says which.
	ToolCount int `json:"tool_count"`

	// ToolNames lists declared tool names, bounded to
	// maxProbeEvidenceToolNames; a clipped listing is recorded in Gaps.
	ToolNames []string `json:"tool_names,omitempty"`

	// AuthPosture is the observed authentication posture.
	AuthPosture ProbeAuthPosture `json:"auth_posture"`

	// Gaps states what the probe could not observe, so absence of evidence is
	// never presented as evidence of absence.
	Gaps []string `json:"gaps,omitempty"`
}

// RemoteProbeResult is a successful verification: evidence to disclose to the
// user plus the signed receipt the registration tool accepts in place of a raw
// URL.
type RemoteProbeResult struct {
	// Evidence is what the probe observed, for user confirmation.
	Evidence ProbeEvidence

	// Receipt is the signed probe receipt bound to the probing caller, the
	// normalized URL, and the evidence digest.
	Receipt string

	// ReceiptExpiresAt is when the receipt stops being redeemable; the remedy
	// after that is to re-probe.
	ReceiptExpiresAt time.Time
}

// RemoteProbeService verifies that a user-supplied remote URL hosts a real MCP
// server before any registration may reference it. Verification means a
// completed initialize handshake or a typed auth rejection; everything else is
// a typed refusal that issues no receipt.
type RemoteProbeService struct {
	logger *slog.Logger
	policy *guardian.Policy
	codec  *probeReceiptCodec
	budget OperationBudget

	// now and timeout are fixed at construction; tests in this package
	// substitute them for determinism.
	now     func() time.Time
	timeout time.Duration
}

// NewRemoteProbeService wires a probe service. receiptKeyMaterial is the same
// key material the catalog cursor codec uses; the receipt codec domain
// separates it. The budget must be the dedicated probe budget: the probe
// performs egress to an arbitrary user-supplied host with Gram as the egress
// point, so it is metered independently of — and more tightly than — read
// operations.
func NewRemoteProbeService(logger *slog.Logger, policy *guardian.Policy, receiptKeyMaterial string, budget OperationBudget) (*RemoteProbeService, error) {
	if policy == nil {
		return nil, errors.New("platform mcp probe service requires a guardian policy")
	}
	codec, err := newProbeReceiptCodec(receiptKeyMaterial)
	if err != nil {
		return nil, fmt.Errorf("create platform mcp probe receipt codec: %w", err)
	}
	return &RemoteProbeService{
		logger:  logger,
		policy:  policy,
		codec:   codec,
		budget:  budget,
		now:     time.Now,
		timeout: remoteProbeTimeout,
	}, nil
}

// Probe validates and verifies one user-supplied remote MCP URL. On
// verification it returns evidence plus a signed receipt; every refusal is one
// of the typed errors ErrRemoteURLInvalid, ErrProbeEgressDenied,
// ErrProbeUnreachable, ErrProbeNotMCPServer, ErrOperationRateLimited, or
// ErrOperationBudgetUnavailable, and issues no receipt.
func (s *RemoteProbeService) Probe(ctx context.Context, principal Principal, remoteURL string) (RemoteProbeResult, error) {
	// A typed-nil service passes the RemoteMCPProber interface nil check at
	// composition, so the boundary guards itself like every sibling entry point
	// instead of panicking on the first call.
	if s == nil || s.policy == nil || s.codec == nil {
		return RemoteProbeResult{}, ErrOperationBudgetUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return RemoteProbeResult{}, err
	}
	// A caller that could not be issued a receipt must not spend egress: the
	// probe's only product is the receipt.
	if principal.OrganizationID == "" || principalReceiptBinding(principal) == "" {
		return RemoteProbeResult{}, ErrProbeReceiptInvalid
	}
	normalized, err := normalizeRemoteURL(remoteURL)
	if err != nil {
		return RemoteProbeResult{}, err
	}
	// Consult the egress policy before any connection so a refused target is
	// always reported as egress_denied rather than surfacing as whatever
	// transport error the blocked dial would have produced. The guardian
	// dialer still re-checks every resolved address at connect time.
	if _, err := s.policy.ValidateHTTPSURL(ctx, normalized); err != nil {
		if errors.Is(err, guardian.ErrBlockedIP) {
			return RemoteProbeResult{}, ErrProbeEgressDenied
		}
		return RemoteProbeResult{}, ErrProbeUnreachable
	}

	probeCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	type probeAttempt struct {
		findings probeFindings
		err      error
	}
	// The MCP SDK can spend well past a tripped context deadline on its own
	// synchronous cleanup, so the attempt runs in a goroutine and the response
	// to the caller is raced against the deadline; the deferred cancel aborts
	// whatever the attempt is still doing once a result is chosen.
	resultCh := make(chan probeAttempt, 1)
	go func() {
		findings, probeErr := s.executeProbe(probeCtx, normalized)
		resultCh <- probeAttempt{findings: findings, err: probeErr}
	}()

	var findings probeFindings
	select {
	case attempt := <-resultCh:
		if attempt.err != nil {
			return RemoteProbeResult{}, attempt.err
		}
		findings = attempt.findings
	case <-probeCtx.Done():
		// select picks pseudo-randomly when both cases are ready, so an
		// attempt finishing right at the deadline gets one more non-blocking
		// chance before the timeout is reported.
		select {
		case attempt := <-resultCh:
			if attempt.err != nil {
				return RemoteProbeResult{}, attempt.err
			}
			findings = attempt.findings
		default:
			return RemoteProbeResult{}, ErrProbeUnreachable
		}
	}

	evidence := buildProbeEvidence(normalized, findings)
	digest, err := probeEvidenceDigest(evidence)
	if err != nil {
		return RemoteProbeResult{}, err
	}
	now := s.now()
	receipt, err := s.codec.Encode(principal, normalized, digest, now)
	if err != nil {
		return RemoteProbeResult{}, fmt.Errorf("issue platform mcp probe receipt: %w", err)
	}
	return RemoteProbeResult{
		Evidence:         evidence,
		Receipt:          receipt,
		ReceiptExpiresAt: now.Add(probeReceiptTTL),
	}, nil
}

// probeFindings is the raw observation of one probe attempt, before posture
// classification and evidence bounding.
type probeFindings struct {
	serverName         string
	serverVersion      string
	toolCount          int
	toolNames          []string
	handshakeCompleted bool
	authRejected       bool
	oauthDiscovered    bool
	gaps               []string
}

// Evidence gap statements. Static strings so a refusal or partial observation
// can never echo attacker-controlled or infrastructure detail.
const (
	probeGapInitializeDeclined = "server declined the unauthenticated initialize handshake, so server identity and tools were not observed"
	probeGapToolsDeclined      = "server declined unauthenticated tools/list"
	probeGapToolListFailed     = "server did not return a usable tools/list response"
	probeGapToolListTooLarge   = "server's tools/list response exceeded the probe's bounded size, so tools were not observed"
	probeGapNoOAuthMetadata    = "server publishes no OAuth metadata at the RFC 9728/8414 well-known endpoints"
	probeGapOAuthIncomplete    = "OAuth metadata discovery did not run to completion"
)

// executeProbe performs the bounded network verification: initialize
// handshake, unauthenticated tools/list, and credential-free OAuth discovery.
// A typed auth rejection is a verification success; only transport-level and
// protocol-level failures return an error.
func (s *RemoteProbeService) executeProbe(ctx context.Context, normalizedURL string) (probeFindings, error) {
	var findings probeFindings
	wwwAuthenticate := ""

	// Retries are disabled twice over (HTTP transport and MCP reconnects):
	// this is a one-shot bounded probe, and retries would let an unreachable
	// server take minutes to report as such instead of ~10s.
	client, err := externalmcp.NewClient(ctx, s.logger, s.policy, normalizedURL, externalmcptypes.TransportTypeStreamableHTTP, &externalmcp.ClientOptions{
		Authorization:    "",
		Headers:          nil,
		DisableRetries:   true,
		MaxResponseBytes: maxProbeResponseBytes,
	})
	if err != nil {
		var authErr *externalmcp.AuthRejectedError
		if !errors.As(err, &authErr) {
			return probeFindings{}, classifyProbeTransportError(err)
		}
		// Only a 401/403 carrying a WWW-Authenticate challenge proves an
		// MCP-shaped, auth-walled server; any ordinary protected endpoint can
		// answer a bare 401/403, so without the challenge nothing was
		// verified and no receipt may issue.
		if authErr.WWWAuthenticate == "" {
			return probeFindings{}, ErrProbeNotMCPServer
		}
		findings.authRejected = true
		wwwAuthenticate = authErr.WWWAuthenticate
		findings.gaps = append(findings.gaps, probeGapInitializeDeclined)
	} else {
		defer o11y.NoLogDefer(client.Close)
		findings.handshakeCompleted = true
		identity := client.ServerIdentity()
		findings.serverName = clipProbeEvidenceField(identity.Name)
		findings.serverVersion = clipProbeEvidenceField(identity.Version)

		var listAuthErr *externalmcp.AuthRejectedError
		tools, listErr := client.ListTools(ctx)
		switch {
		case listErr == nil:
			findings.toolCount = len(tools)
			for _, tool := range tools {
				if len(findings.toolNames) == maxProbeEvidenceToolNames {
					findings.gaps = append(findings.gaps, fmt.Sprintf("tool names clipped to the first %d of %d declared tools", maxProbeEvidenceToolNames, len(tools)))
					break
				}
				findings.toolNames = append(findings.toolNames, clipProbeEvidenceField(tool.Name))
			}
		case errors.As(listErr, &listAuthErr) && listAuthErr.WWWAuthenticate != "":
			findings.authRejected = true
			wwwAuthenticate = listAuthErr.WWWAuthenticate
			findings.gaps = append(findings.gaps, probeGapToolsDeclined)
		case errors.Is(listErr, externalmcp.ErrResponseTooLarge):
			findings.gaps = append(findings.gaps, probeGapToolListTooLarge)
		default:
			// The handshake already verified the server; an unusable listing —
			// including a bare 401/403 with no WWW-Authenticate challenge,
			// which is not auth evidence — is a bounded gap, not a refusal.
			findings.gaps = append(findings.gaps, probeGapToolListFailed)
		}
	}

	// Credential-free OAuth discovery via the RFC 9728/8414 well-knowns runs
	// for verified servers regardless of how they were verified: an open
	// server may still publish metadata, and an auth-walled one usually must.
	discovery, err := externalmcp.DiscoverOAuthMetadata(ctx, s.logger, s.policy, wwwAuthenticate, normalizedURL)
	switch {
	case err != nil:
		findings.gaps = append(findings.gaps, probeGapOAuthIncomplete)
	case discovery == nil || (discovery.Version == externalmcp.OAuthVersionNone && discovery.RegistrationEndpoint == "" && len(discovery.ScopesSupported) == 0):
		// Version "none" with nothing else is discovery finding nothing.
		// Whether that is a published absence or a failed probe depends on
		// ProbeIncomplete — a dead well-known endpoint must not read as "the
		// server publishes no OAuth metadata".
		if discovery != nil && discovery.ProbeIncomplete {
			findings.gaps = append(findings.gaps, probeGapOAuthIncomplete)
		} else {
			findings.gaps = append(findings.gaps, probeGapNoOAuthMetadata)
		}
	default:
		findings.oauthDiscovered = true
	}

	return findings, nil
}

// classifyProbeTransportError maps a failed connection attempt onto the typed
// refusals. Guardian refusals are checked first because they surface wrapped
// inside transport errors; the sentinels are static so no guardian, resolver,
// or transport detail is ever echoed to the caller.
func classifyProbeTransportError(err error) error {
	if errors.Is(err, guardian.ErrBlockedIP) || errors.Is(err, guardian.ErrBadHost) {
		return ErrProbeEgressDenied
	}
	// An initialize response past the bounded read: the server answered, but
	// nothing verifiable was observed, so the probe refuses rather than
	// treating an unbounded talker as an MCP server.
	if errors.Is(err, externalmcp.ErrResponseTooLarge) {
		return ErrProbeNotMCPServer
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrProbeUnreachable
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return ErrProbeUnreachable
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return ErrProbeUnreachable
	}
	// The target answered HTTP but the MCP handshake failed: connected, yet
	// not an MCP server.
	return ErrProbeNotMCPServer
}

// buildProbeEvidence classifies the observed auth posture and assembles the
// bounded evidence document the receipt digest signs.
func buildProbeEvidence(normalizedURL string, findings probeFindings) ProbeEvidence {
	posture := ProbeAuthPostureOpen
	switch {
	case findings.oauthDiscovered:
		posture = ProbeAuthPostureOAuthDiscovered
	case findings.authRejected:
		posture = ProbeAuthPostureAuthRequired
	}
	return ProbeEvidence{
		NormalizedURL: normalizedURL,
		ServerName:    findings.serverName,
		ServerVersion: findings.serverVersion,
		ToolCount:     findings.toolCount,
		ToolNames:     findings.toolNames,
		AuthPosture:   posture,
		Gaps:          findings.gaps,
	}
}

// probeEvidenceDigest fingerprints the evidence a receipt was issued for. The
// digest travels inside the signed receipt, binding what the user confirmed to
// what a registration may redeem.
func probeEvidenceDigest(evidence ProbeEvidence) (string, error) {
	payload, err := json.Marshal(evidence)
	if err != nil {
		return "", fmt.Errorf("encode platform mcp probe evidence: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// clipProbeEvidenceField bounds one server-declared free-text field, marking
// the cut explicitly and retreating to a rune boundary so a bounded field
// never carries a mangled half-rune.
func clipProbeEvidenceField(value string) string {
	if len(value) <= maxProbeEvidenceFieldBytes {
		return value
	}
	cut := maxProbeEvidenceFieldBytes - len(probeTruncationMarker)
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + probeTruncationMarker
}
