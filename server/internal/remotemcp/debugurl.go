package remotemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

const (
	// DebugURLTimeout bounds the whole trace. Each network step keeps its own
	// tighter budget (probeURLTimeout, the discovery timeouts), so a slow step
	// fails on its own rather than starving the steps after it.
	DebugURLTimeout = 30 * time.Second

	// debugMaxListPages caps how many pages of tools, prompts or resources the
	// trace follows. The debugger shows what a server offers, not an export.
	debugMaxListPages = 5

	DebugStepReachServer          = "reach_server"
	DebugStepParseAuthChallenge   = "parse_auth_challenge"
	DebugStepFetchProtectedRes    = "fetch_protected_resource_metadata"
	DebugStepFetchAuthServer      = "fetch_authorization_server_metadata"
	DebugStepDetectAuthMethod     = "detect_auth_method"
	DebugStepInitialize           = "initialize"
	DebugStepListCapabilities     = "list_capabilities"
	DebugStatusPassed             = "passed"
	DebugStatusWarning            = "warning"
	DebugStatusFailed             = "failed"
	DebugStatusSkipped            = "skipped"
	DebugAuthMethodNone           = "none"
	DebugAuthMethodCIMD           = "client_id_metadata_document"
	DebugAuthMethodDCR            = "dynamic_client_registration"
	DebugAuthMethodPreRegistered  = "pre_registered"
	DebugAuthMethodNonOAuth       = "non_oauth"
	DebugAuthMethodUnknown        = "unknown"
	debugSignInRequiredSkipReason = "Sign-in is required. Add the server to the project and connect it to finish the trace."
)

// DebugReport is the result of a diagnostic trace. Steps always lists every
// step in debugStepOrder; the ones the trace never reached are skipped.
type DebugReport struct {
	URL             string
	Outcome         string
	DurationMs      int64
	Steps           []*DebugStep
	AuthMethod      string
	ProtocolVersion *string
	ServerName      *string
	ServerVersion   *string
	Instructions    *string
	// Tools, Prompts and Resources are set only when a session was opened.
	Tools     []*DebugTool
	Prompts   []*DebugPrompt
	Resources []*DebugResource
}

// DebugStep is one step of a trace.
type DebugStep struct {
	ID         string
	Title      string
	Status     string
	Summary    string
	DurationMs *int64
	RequestURL *string
	HTTPStatus *int
	Details    map[string]any
}

// DebugTool is a tool the traced server advertised.
type DebugTool struct {
	Name        string
	Title       *string
	Description *string
	InputSchema any
	Annotations any
}

// DebugPrompt is a prompt the traced server advertised.
type DebugPrompt struct {
	Name        string
	Title       *string
	Description *string
	Arguments   any
}

// DebugResource is a resource the traced server advertised.
type DebugResource struct {
	URI         string
	Name        string
	Title       *string
	Description *string
	MimeType    *string
}

var debugStepTitles = map[string]string{
	DebugStepReachServer:        "Reach server",
	DebugStepParseAuthChallenge: "Parse authentication challenge",
	DebugStepFetchProtectedRes:  "Fetch protected resource metadata",
	DebugStepFetchAuthServer:    "Fetch authorization server metadata",
	DebugStepDetectAuthMethod:   "Detect authentication method",
	DebugStepInitialize:         "Initialize MCP",
	DebugStepListCapabilities:   "List tools, prompts and resources",
}

var debugStepOrder = []string{
	DebugStepReachServer,
	DebugStepParseAuthChallenge,
	DebugStepFetchProtectedRes,
	DebugStepFetchAuthServer,
	DebugStepDetectAuthMethod,
	DebugStepInitialize,
	DebugStepListCapabilities,
}

// debugTrace accumulates steps and fills any the run never reached as skipped,
// so the report always lists every step in the same order.
type debugTrace struct {
	report *DebugReport
	steps  map[string]*DebugStep
}

func newDebugTrace(rawURL string) *debugTrace {
	return &debugTrace{
		report: &DebugReport{
			URL:             urls.DiagnosticURL(rawURL),
			Outcome:         ProbeOutcomeUnreachable,
			DurationMs:      0,
			Steps:           nil,
			AuthMethod:      DebugAuthMethodUnknown,
			ProtocolVersion: nil,
			ServerName:      nil,
			ServerVersion:   nil,
			Instructions:    nil,
			Tools:           nil,
			Prompts:         nil,
			Resources:       nil,
		},
		steps: map[string]*DebugStep{},
	}
}

func (t *debugTrace) step(id, status, summary string) *DebugStep {
	step := &DebugStep{
		ID:         id,
		Title:      debugStepTitles[id],
		Status:     status,
		Summary:    summary,
		DurationMs: nil,
		RequestURL: nil,
		HTTPStatus: nil,
		Details:    nil,
	}
	t.steps[id] = step
	return step
}

// skipRest marks every step not yet recorded as skipped with reason.
func (t *debugTrace) skipRest(reason string) {
	for _, id := range debugStepOrder {
		if _, ok := t.steps[id]; !ok {
			t.step(id, DebugStatusSkipped, reason)
		}
	}
}

func (t *debugTrace) finish(start time.Time) *DebugReport {
	t.skipRest("Not reached.")
	steps := make([]*DebugStep, 0, len(debugStepOrder))
	for _, id := range debugStepOrder {
		steps = append(steps, t.steps[id])
	}
	t.report.Steps = steps
	t.report.DurationMs = time.Since(start).Milliseconds()
	return t.report
}

func durationMs(start time.Time) *int64 {
	ms := time.Since(start).Milliseconds()
	return &ms
}

// DebugOptions carries deployment facts the trace checks the upstream against.
type DebugOptions struct {
	// ClientMetadataOrigin is the origin Speakeasy publishes its OAuth client
	// metadata documents under. When it is not reachable from the internet, a
	// provider that uses Client ID Metadata Documents cannot fetch Speakeasy's
	// document and sign-in fails. Nil skips the check.
	ClientMetadataOrigin *url.URL
}

// DebugRemoteMcpURL runs the diagnostic trace behind remoteMcp.debugURL. It
// never returns an error: every failure becomes a failed step so the caller
// can show where the trace stopped. The caller bounds the overall deadline.
func DebugRemoteMcpURL(ctx context.Context, policy *guardian.Policy, rawURL string, opts DebugOptions) *DebugReport {
	start := time.Now()
	trace := newDebugTrace(rawURL)

	if _, err := proxy.ValidateRemoteMCPURL(ctx, policy, rawURL); err != nil {
		result := classifyTransportError(ctx, err)
		step := trace.step(DebugStepReachServer, DebugStatusFailed, debugUnreachableSummary(result.Reason, nil))
		step.RequestURL = conv.PtrEmpty(trace.report.URL)
		trace.skipRest("The server could not be reached.")
		return trace.finish(start)
	}

	debugConnect(ctx, policy, rawURL, trace, opts)
	return trace.finish(start)
}

// debugExchange is one HTTP round trip the MCP client made.
type debugExchange struct {
	method          string
	url             string
	status          int
	wwwAuthenticate []string
	duration        time.Duration
}

// debugRoundTripper records every exchange, capping bodies like the probe.
type debugRoundTripper struct {
	base      http.RoundTripper
	mu        sync.Mutex
	exchanges []debugExchange
}

func (rt *debugRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err //nolint:wrapcheck // preserve the base transport's error for classification
	}

	if req.Method != http.MethodDelete {
		rt.mu.Lock()
		rt.exchanges = append(rt.exchanges, debugExchange{
			method:          req.Method,
			url:             urls.DiagnosticURL(req.URL.String()),
			status:          resp.StatusCode,
			wwwAuthenticate: resp.Header.Values("WWW-Authenticate"),
			duration:        time.Since(start),
		})
		rt.mu.Unlock()
	}

	resp.Body = http.MaxBytesReader(nil, resp.Body, probeURLMaxBodyBytes)
	return resp, nil
}

func (rt *debugRoundTripper) snapshot() []debugExchange {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return slices.Clone(rt.exchanges)
}

func debugConnect(ctx context.Context, policy *guardian.Policy, rawURL string, trace *debugTrace, opts DebugOptions) {
	client := newProbeHTTPClient(policy)
	recorder := &debugRoundTripper{base: client.Transport, mu: sync.Mutex{}, exchanges: nil}
	client.Transport = recorder

	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:        "speakeasy-debugger",
		Title:       "",
		Description: "",
		Version:     "1",
		WebsiteURL:  "",
		Icons:       nil,
	}, nil)
	transport := &probeTransport{
		inner: &mcp.StreamableClientTransport{
			Endpoint:             rawURL,
			HTTPClient:           client,
			MaxRetries:           -1,
			DisableStandaloneSSE: true,
			OAuthHandler:         nil,
		},
		conn: nil,
	}

	connectCtx, cancel := context.WithTimeout(ctx, probeURLTimeout)
	defer cancel()
	connectStart := time.Now()
	session, err := mcpClient.Connect(connectCtx, transport, nil)
	connectDuration := durationMs(connectStart)
	exchanges := recorder.snapshot()

	reach := trace.step(DebugStepReachServer, DebugStatusPassed, "The server answered.")
	reach.RequestURL = conv.PtrEmpty(trace.report.URL)
	if len(exchanges) > 0 {
		first := exchanges[0]
		ms := first.duration.Milliseconds()
		reach.DurationMs = &ms
		reach.HTTPStatus = &first.status
		reach.Summary = fmt.Sprintf("The server answered %s with HTTP %d.", first.method, first.status)
		reach.Details = map[string]any{"exchanges": debugExchangeDetails(exchanges)}
	}

	if err != nil {
		if transport.conn != nil {
			go o11y.NoLogDefer(transport.conn.Close)
		}
		var last *probeResponse
		if len(exchanges) > 0 {
			final := exchanges[len(exchanges)-1]
			last = &probeResponse{status: final.status, wwwAuthenticate: final.wwwAuthenticate}
		}
		debugConnectFailed(ctx, policy, rawURL, trace, opts, classifyProbeError(connectCtx, err, last).result, last, err, connectDuration)
		return
	}
	defer func() { go o11y.NoLogDefer(session.Close) }()

	trace.report.Outcome = ProbeOutcomeMCPAvailable
	trace.report.AuthMethod = DebugAuthMethodNone
	trace.step(DebugStepParseAuthChallenge, DebugStatusSkipped, "The server did not ask for credentials.")
	trace.step(DebugStepFetchProtectedRes, DebugStatusSkipped, "Not needed: the server accepts unauthenticated requests.")
	trace.step(DebugStepFetchAuthServer, DebugStatusSkipped, "Not needed: the server accepts unauthenticated requests.")
	trace.step(DebugStepDetectAuthMethod, DebugStatusPassed, "No authentication required.")

	initialize := trace.step(DebugStepInitialize, DebugStatusPassed, "Session initialized.")
	initialize.DurationMs = connectDuration
	if init := session.InitializeResult(); init != nil {
		trace.report.ProtocolVersion = conv.PtrEmpty(init.ProtocolVersion)
		trace.report.Instructions = conv.PtrEmpty(init.Instructions)
		if init.ServerInfo != nil {
			trace.report.ServerName = conv.PtrEmpty(init.ServerInfo.Name)
			trace.report.ServerVersion = conv.PtrEmpty(init.ServerInfo.Version)
		}
		initialize.Summary = fmt.Sprintf("Negotiated protocol %s.", init.ProtocolVersion)
		initialize.Details = map[string]any{
			"protocol_version": init.ProtocolVersion,
			"server_info":      init.ServerInfo,
			"capabilities":     init.Capabilities,
		}
	}

	debugListCapabilities(ctx, session, trace)
}

func debugExchangeDetails(exchanges []debugExchange) []map[string]any {
	out := make([]map[string]any, 0, len(exchanges))
	for _, ex := range exchanges {
		entry := map[string]any{
			"method":      ex.method,
			"url":         ex.url,
			"status":      ex.status,
			"duration_ms": ex.duration.Milliseconds(),
		}
		if len(ex.wwwAuthenticate) > 0 {
			entry["www_authenticate"] = ex.wwwAuthenticate
		}
		out = append(out, entry)
	}
	return out
}

func debugUnreachableSummary(reason *string, status *int) string {
	var msg string
	switch conv.PtrValOr(reason, "") {
	case ProbeReasonTimeout:
		msg = "The request timed out."
	case ProbeReasonRateLimited:
		msg = "The server rate limited the request."
	case ProbeReasonServerError:
		msg = "The server returned an error."
	case ProbeReasonDNSError:
		msg = "The host name did not resolve."
	case ProbeReasonTLSError:
		msg = "The TLS connection failed."
	case ProbeReasonGuardianRejected:
		msg = "Network policy does not allow this host."
	default:
		msg = "Could not connect to the server."
	}
	if status != nil {
		msg = fmt.Sprintf("%s (HTTP %d)", strings.TrimSuffix(msg, "."), *status)
	}
	return msg
}

func debugConnectFailed(ctx context.Context, policy *guardian.Policy, rawURL string, trace *debugTrace, opts DebugOptions, result ProbeResult, last *probeResponse, connectErr error, connectDuration *int64) {
	trace.report.Outcome = result.Outcome

	switch result.Outcome {
	case ProbeOutcomeUnreachable:
		reach := trace.steps[DebugStepReachServer]
		reach.Status = DebugStatusFailed
		reach.Summary = debugUnreachableSummary(result.Reason, result.HTTPStatus)
		trace.skipRest("The server could not be reached.")
	case ProbeOutcomeInvalidMCPResponse:
		trace.step(DebugStepParseAuthChallenge, DebugStatusSkipped, "The server did not ask for credentials.")
		trace.step(DebugStepFetchProtectedRes, DebugStatusSkipped, "Not reached.")
		trace.step(DebugStepFetchAuthServer, DebugStatusSkipped, "Not reached.")
		trace.step(DebugStepDetectAuthMethod, DebugStatusSkipped, "Not reached.")
		initialize := trace.step(DebugStepInitialize, DebugStatusFailed, "The server's answer was not a valid MCP response.")
		initialize.DurationMs = connectDuration
		initialize.HTTPStatus = result.HTTPStatus
		initialize.Details = map[string]any{"error": connectErr.Error()}
		trace.skipRest("Initialize failed.")
	case ProbeOutcomeAuthenticationRequired:
		debugAuth(ctx, policy, rawURL, trace, opts, last)
		trace.skipRest(debugSignInRequiredSkipReason)
	default:
		trace.skipRest("Not reached.")
	}
}

// debugAuth runs the OAuth discovery steps for a server that refused the
// unauthenticated initialize.
func debugAuth(ctx context.Context, policy *guardian.Policy, rawURL string, trace *debugTrace, opts DebugOptions, last *probeResponse) {
	var challenges []string
	if last != nil {
		challenges = last.wwwAuthenticate
	}

	challenge := trace.step(DebugStepParseAuthChallenge, DebugStatusPassed, "")
	if last != nil {
		challenge.HTTPStatus = &last.status
	}
	schemes := challengeSchemes(challenges)
	resourceMetadataURL := parseProtectedResourceMetadataURL(challenges)
	scopes := parseChallengeScopes(challenges)
	bearer := slices.ContainsFunc(schemes, func(s string) bool { return strings.EqualFold(s, "Bearer") || strings.EqualFold(s, "DPoP") })
	challenge.Details = map[string]any{
		"www_authenticate":  challenges,
		"schemes":           schemes,
		"resource_metadata": conv.PtrValOr(resourceMetadataURL, ""),
		"scopes":            scopes,
	}
	switch {
	case len(challenges) == 0:
		challenge.Status = DebugStatusWarning
		challenge.Summary = "The server refused the request without a WWW-Authenticate header, so clients cannot tell how to sign in."
	case !bearer:
		challenge.Status = DebugStatusWarning
		challenge.Summary = fmt.Sprintf("The server asked for %s credentials, not an OAuth bearer token.", strings.Join(schemes, ", "))
	case resourceMetadataURL == nil:
		challenge.Status = DebugStatusWarning
		challenge.Summary = "Bearer challenge without a resource_metadata URL; clients must fall back to the well-known location."
	default:
		challenge.Summary = "Bearer challenge advertises protected resource metadata."
	}

	prmStart := time.Now()
	prm, warnings, err := wellknown.DiscoverProtectedResourceMetadata(ctx, policy, rawURL)
	prmStep := trace.step(DebugStepFetchProtectedRes, DebugStatusPassed, "Found an RFC 9728 protected resource metadata document.")
	prmStep.DurationMs = durationMs(prmStart)
	if resourceMetadataURL != nil {
		prmStep.RequestURL = conv.PtrEmpty(urls.DiagnosticURL(*resourceMetadataURL))
	}
	if err != nil {
		prmStep.Status = DebugStatusFailed
		prmStep.Summary = err.Error()
		if typed, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok {
			prmStep.Summary = typed.UserMessage()
			prmStep.RequestURL = conv.PtrEmpty(urls.DiagnosticURL(typed.ProbeURL))
			if typed.Status != 0 {
				prmStep.HTTPStatus = &typed.Status
			}
		}
		if bearer {
			trace.report.AuthMethod = DebugAuthMethodUnknown
			trace.step(DebugStepDetectAuthMethod, DebugStatusFailed, "Without protected resource metadata, clients cannot find the authorization server.")
			return
		}
		// A server that never offered OAuth has no reason to publish the
		// document, so its absence is expected rather than a fault.
		if typed, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok && typed.Code() == "not_found" {
			prmStep.Status = DebugStatusSkipped
			prmStep.Summary = "Not published, as expected for a server that does not use OAuth."
		}
		trace.report.AuthMethod = DebugAuthMethodNonOAuth
		trace.step(DebugStepDetectAuthMethod, DebugStatusPassed, "The server expects a credential such as an API key; send it as a header.")
		return
	}
	prmStep.Details = map[string]any{"metadata": prm, "warnings": warnings}
	if len(warnings) > 0 {
		prmStep.Status = DebugStatusWarning
		prmStep.Summary = strings.Join(warnings, " ")
	}

	if len(prm.AuthorizationServers) == 0 {
		trace.step(DebugStepFetchAuthServer, DebugStatusFailed, "The metadata lists no authorization_servers.")
		trace.report.AuthMethod = DebugAuthMethodUnknown
		trace.step(DebugStepDetectAuthMethod, DebugStatusFailed, "No authorization server to register with.")
		return
	}

	issuer := prm.AuthorizationServers[0]
	asStep := trace.step(DebugStepFetchAuthServer, DebugStatusPassed, fmt.Sprintf("Found RFC 8414 metadata for %s.", urls.DiagnosticURL(issuer)))
	if metadataURL, urlErr := wellknown.AuthorizationServerMetadataURL(issuer); urlErr == nil {
		asStep.RequestURL = conv.PtrEmpty(urls.DiagnosticURL(metadataURL))
	}
	asStart := time.Now()
	as, err := wellknown.DiscoverAuthorizationServerMetadata(ctx, policy, issuer)
	asStep.DurationMs = durationMs(asStart)
	if err != nil {
		asStep.Status = DebugStatusFailed
		asStep.Summary = err.Error()
		trace.report.AuthMethod = DebugAuthMethodUnknown
		trace.step(DebugStepDetectAuthMethod, DebugStatusFailed, "Without authorization server metadata, clients cannot register or sign in.")
		return
	}

	var extra struct {
		CIMDSupported         bool     `json:"client_id_metadata_document_supported"`
		CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
		GrantTypes            []string `json:"grant_types_supported"`
		TokenEndpointAuthMeth []string `json:"token_endpoint_auth_methods_supported"`
	}
	var doc map[string]any
	_ = json.Unmarshal(as.RawMetadata, &extra)
	_ = json.Unmarshal(as.RawMetadata, &doc)
	asStep.Details = map[string]any{"metadata": doc}
	if len(extra.CodeChallengeMethods) > 0 && !slices.Contains(extra.CodeChallengeMethods, "S256") {
		asStep.Status = DebugStatusWarning
		asStep.Summary = "The authorization server does not advertise PKCE S256, which MCP clients require."
	}

	detect := trace.step(DebugStepDetectAuthMethod, DebugStatusPassed, "")
	detect.Details = map[string]any{
		"client_id_metadata_document_supported": extra.CIMDSupported,
		"registration_endpoint":                 as.RegistrationEndpoint,
		"token_endpoint_auth_methods_supported": extra.TokenEndpointAuthMeth,
		"grant_types_supported":                 extra.GrantTypes,
	}
	switch {
	case extra.CIMDSupported:
		trace.report.AuthMethod = DebugAuthMethodCIMD
		detect.Summary = "Clients identify themselves with a Client ID Metadata Document."
		if origin := opts.ClientMetadataOrigin; origin != nil && !isInternetReachableOrigin(origin) {
			detect.Status = DebugStatusWarning
			detect.Details["speakeasy_client_metadata_origin"] = origin.String()
			fix := "This deployment needs a public HTTPS address for that to work."
			if as.RegistrationEndpoint != "" {
				fix = "Choose DCR under the server's Authentication settings, in Advanced options, to register with this provider instead."
			}
			detect.Summary = fmt.Sprintf("Clients identify themselves with a Client ID Metadata Document, but this provider would fetch Speakeasy's document from %s, which it cannot reach, so connecting will fail. %s", origin.Host, fix)
		}
	case as.RegistrationEndpoint != "":
		trace.report.AuthMethod = DebugAuthMethodDCR
		detect.Summary = "Clients register with Dynamic Client Registration."
	default:
		trace.report.AuthMethod = DebugAuthMethodPreRegistered
		detect.Status = DebugStatusWarning
		detect.Summary = "No automatic registration: you need a client ID and secret registered with this provider."
	}
}

// challengeSchemes lists the auth schemes named across the challenge headers.
func challengeSchemes(headers []string) []string {
	var schemes []string
	for _, header := range headers {
		for part := range strings.SplitSeq(header, ",") {
			fields := strings.Fields(part)
			if len(fields) == 0 || strings.Contains(fields[0], "=") {
				continue
			}
			if !slices.ContainsFunc(schemes, func(s string) bool { return strings.EqualFold(s, fields[0]) }) {
				schemes = append(schemes, fields[0])
			}
		}
	}
	return schemes
}

func debugListCapabilities(ctx context.Context, session *mcp.ClientSession, trace *debugTrace) {
	step := trace.step(DebugStepListCapabilities, DebugStatusPassed, "")
	start := time.Now()
	listCtx, cancel := context.WithTimeout(ctx, probeURLTimeout)
	defer cancel()

	var caps *mcp.ServerCapabilities
	if init := session.InitializeResult(); init != nil {
		caps = init.Capabilities
	}
	var failures []string

	tools := []*DebugTool{}
	if caps == nil || caps.Tools != nil {
		cursor := ""
		for range debugMaxListPages {
			res, err := session.ListTools(listCtx, &mcp.ListToolsParams{Meta: nil, Cursor: cursor})
			if err != nil {
				failures = append(failures, "tools/list: "+err.Error())
				break
			}
			for _, tool := range res.Tools {
				var annotations any
				if tool.Annotations != nil {
					annotations = tool.Annotations
				}
				tools = append(tools, &DebugTool{
					Name:        tool.Name,
					Title:       conv.PtrEmpty(tool.Title),
					Description: conv.PtrEmpty(tool.Description),
					InputSchema: tool.InputSchema,
					Annotations: annotations,
				})
			}
			if res.NextCursor == "" {
				break
			}
			cursor = res.NextCursor
		}
	}

	prompts := []*DebugPrompt{}
	if caps != nil && caps.Prompts != nil {
		cursor := ""
		for range debugMaxListPages {
			res, err := session.ListPrompts(listCtx, &mcp.ListPromptsParams{Meta: nil, Cursor: cursor})
			if err != nil {
				failures = append(failures, "prompts/list: "+err.Error())
				break
			}
			for _, prompt := range res.Prompts {
				var args any
				if len(prompt.Arguments) > 0 {
					args = prompt.Arguments
				}
				prompts = append(prompts, &DebugPrompt{
					Name:        prompt.Name,
					Title:       conv.PtrEmpty(prompt.Title),
					Description: conv.PtrEmpty(prompt.Description),
					Arguments:   args,
				})
			}
			if res.NextCursor == "" {
				break
			}
			cursor = res.NextCursor
		}
	}

	resources := []*DebugResource{}
	if caps != nil && caps.Resources != nil {
		cursor := ""
		for range debugMaxListPages {
			res, err := session.ListResources(listCtx, &mcp.ListResourcesParams{Meta: nil, Cursor: cursor})
			if err != nil {
				failures = append(failures, "resources/list: "+err.Error())
				break
			}
			for _, resource := range res.Resources {
				resources = append(resources, &DebugResource{
					URI:         resource.URI,
					Name:        resource.Name,
					Title:       conv.PtrEmpty(resource.Title),
					Description: conv.PtrEmpty(resource.Description),
					MimeType:    conv.PtrEmpty(resource.MIMEType),
				})
			}
			if res.NextCursor == "" {
				break
			}
			cursor = res.NextCursor
		}
	}

	trace.report.Tools = tools
	trace.report.Prompts = prompts
	trace.report.Resources = resources
	step.DurationMs = durationMs(start)
	step.Summary = fmt.Sprintf("%d tools, %d prompts, %d resources.", len(tools), len(prompts), len(resources))
	if len(failures) > 0 {
		step.Status = DebugStatusWarning
		step.Details = map[string]any{"errors": failures}
	}
}

// isInternetReachableOrigin reports whether a provider on the internet could
// fetch documents from origin. It reads the address only, without resolving
// it: loopback, private and link-local addresses, local-only host names, and
// plain HTTP all fail.
func isInternetReachableOrigin(origin *url.URL) bool {
	if !strings.EqualFold(origin.Scheme, "https") {
		return false
	}
	host := strings.ToLower(origin.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
	}
	return true
}
