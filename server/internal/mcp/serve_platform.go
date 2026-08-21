package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// PlatformToolsetRoute is the chi route pattern reserved for platform
// toolsets. The path prefix is distinct from /mcp/{slug} so a platform slug
// can never collide with a user-toolset slug; keep it in lockstep with
// platformtools.PlatformToolsetURL.
const PlatformToolsetRoute = "/platform/mcp/{toolsetSlug}"

const platformToolsetMaxBodyBytes = 1 << 20

// ServePlatformToolset is the runtime-only entrypoint for platform toolsets:
// only the assistant token is accepted, so user OAuth/API keys/chat sessions
// are intentionally not honored here.
func (s *Service) ServePlatformToolset(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	slug := chi.URLParam(r, "toolsetSlug")
	if slug == "" {
		return oops.E(oops.CodeBadRequest, nil, "a platform toolset slug must be provided")
	}

	toolset, ok := s.platformToolsets[slug]
	if !ok {
		return oops.E(oops.CodeNotFound, nil, "platform toolset not found")
	}

	token := httpheaders.AuthorizationBearerToken(r)
	if token == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	authedCtx, _, err := s.assistantTokens.Authorize(ctx, token)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to authorize platform toolset request").LogError(ctx, s.logger)
	}
	ctx = authedCtx

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "no project auth context").LogError(ctx, s.logger)
	}

	if err := s.authorizePlatformToolset(ctx, slug, authCtx); err != nil {
		return err
	}

	r.Body = http.MaxBytesReader(w, r.Body, platformToolsetMaxBodyBytes)

	bodyBytes, err := io.ReadAll(r.Body)
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.Is(err, io.EOF) || len(bodyBytes) == 0:
		return nil
	case errors.As(err, &maxBytesErr):
		return oops.E(oops.CodeRequestTooLarge, err, "platform toolset request body exceeds 1 MiB").LogError(ctx, s.logger)
	case err != nil:
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").LogError(ctx, s.logger)
	}

	if len(bodyBytes) > 0 && bodyBytes[0] == '[' {
		return oops.E(oops.CodeBadRequest, nil, "batch requests are not supported").LogError(ctx, s.logger)
	}

	var req rawRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to decode request body").LogError(ctx, s.logger)
	}
	if req.JSONRPC != "2.0" {
		return oops.E(oops.CodeBadRequest, errInvalidJSONRPCVersion, "unsupported JSON-RPC version").LogError(ctx, s.logger)
	}

	body, err := s.handlePlatformToolsetRequest(ctx, authCtx, toolset, &req, r.Header.Get("Gram-Chat-ID"), mcpversions.Sanitize(r.Header.Get(mcpversions.HTTPHeader)))
	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		bs, merr := json.Marshal(oops.NewMCPErrorFromCause(req.ID, err))
		if merr != nil {
			return oops.E(oops.CodeUnexpected, merr, "failed to serialize error response").LogError(ctx, s.logger)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write(bs); writeErr != nil {
			return oops.E(oops.CodeUnexpected, writeErr, "failed to write error response body").LogError(ctx, s.logger)
		}
		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, writeErr := w.Write(body); writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body")
	}
	return nil
}

// authorizePlatformToolset gates entry to a platform toolset before any work
// runs. The managed-assistant toolset carries the dashboard egress tool and is
// reserved for a project's managed assistant; any other assistant token for the
// project is rejected as if the toolset did not exist, rather than relying on
// downstream tools to refuse the call.
func (s *Service) authorizePlatformToolset(ctx context.Context, slug string, authCtx *contextvalues.AuthContext) error {
	switch slug {
	case platformtools.ResearchToolsetSlug:
		// Nobody reaches the research tools over HTTP. The research runner
		// constructs its executors privately — the slug is not even in the
		// toolset registry — so this refusal is a tripwire: an assistant
		// token arriving here was never meant to have billable web search
		// and public page fetch, and re-registering the toolset must not
		// quietly grant them. If the runner is ever moved onto the assistant
		// runtime, this is where its principal is checked.
		return oops.E(oops.CodeNotFound, nil, "platform toolset not found")
	case platformtools.ManagedAssistantPlatformToolsetSlug, platformtools.PlatformMCPReadToolsetSlug:
	default:
		return nil
	}

	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeNotFound, nil, "platform toolset not found")
	}

	managed, err := assistantrepo.New(s.db).GetManagedAssistantByProject(ctx, *authCtx.ProjectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "platform toolset not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "resolve managed assistant").LogError(ctx, s.logger)
	}

	if managed.ID != principal.AssistantID {
		return oops.E(oops.CodeNotFound, nil, "platform toolset not found")
	}

	// The two managed-only toolsets are rollout variants of each other: a
	// thread reaches exactly one, never both. The attachment decision in the
	// assistants service resolves the same variant, but the assistant token
	// lives inside the runner VM, so the serve path re-resolves rather than
	// trusting attachment. A variant that cannot be resolved falls back to
	// legacy, matching attachment, so an outage never leaves the managed
	// assistant with no toolset at all.
	//
	// The rollout is scoped to dashboard threads, so the calling thread's
	// source kind is part of the decision — the same managed assistant serves
	// Slack, cron and wake turns on the legacy toolsets. The principal carries
	// the thread it was minted for, which is what makes that resolvable here
	// without trusting anything the runner sends.
	//
	// A source kind that cannot be read — a thread deleted mid-turn, a database
	// blip — falls back to the flag alone, which is how this decision was made
	// before the rollout was scoped to the dashboard. Reporting it as
	// not-dashboard instead would 404 the Platform MCP toolset that bootstrap
	// attached to a dashboard thread, stripping the assistant of every tool it
	// has mid-turn; the reverse error only costs a non-dashboard thread its
	// tools on an organization that is already on the variant, and neither
	// error can reach an organization that is not.
	sourceKind, resolvedSourceKind := s.threadSourceKind(ctx, principal.ThreadID, *authCtx.ProjectID)
	dashboardScoped := !resolvedSourceKind || sourceKind == bgtriggers.DefinitionSlugDashboard

	variant := feature.VariantAssistantToolsLegacy
	if s.features != nil && dashboardScoped {
		resolved, err := feature.FlagVariant(ctx, s.features, feature.FlagAssistantPlatformMCP,
			authCtx.ActiveOrganizationID, feature.OrgProjectGroups(authCtx.OrganizationSlug, ""))
		if err != nil {
			s.logger.WarnContext(ctx, "resolve assistant platform mcp variant", attr.SlogError(err))
		} else {
			variant = feature.AssistantToolsVariant(resolved)
		}
	}

	if slug != wantedToolsetSlug(variant) {
		return oops.E(oops.CodeNotFound, nil, "platform toolset not found")
	}

	return nil
}

// threadSourceKind reads the surface the calling thread was opened from,
// reporting false when it cannot be read — a thread deleted mid-turn, or a
// database blip.
//
// Scoped to the request's project so a thread id belonging to another project
// cannot decide which toolset this one is served, even though the id comes
// from a signed principal rather than the request body.
func (s *Service) threadSourceKind(ctx context.Context, threadID, projectID uuid.UUID) (string, bool) {
	sourceKind, err := assistantrepo.New(s.db).GetAssistantThreadSourceKind(ctx, assistantrepo.GetAssistantThreadSourceKindParams{
		ThreadID:  threadID,
		ProjectID: projectID,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "resolve assistant thread source kind", attr.SlogError(err))
		return "", false
	}
	return sourceKind, true
}

// wantedToolsetSlug maps a resolved rollout variant to the single managed-only
// platform toolset that variant serves. Keep in lockstep with
// assistantPlatformSlugs in the assistants service — the two must agree or a
// toolset is attached at bootstrap and then 404s on every request.
func wantedToolsetSlug(variant feature.Variant) string {
	if variant == feature.VariantAssistantToolsPlatformMCP {
		return platformtools.PlatformMCPReadToolsetSlug
	}
	return platformtools.ManagedAssistantPlatformToolsetSlug
}

func (s *Service) handlePlatformToolsetRequest(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	toolset platformtools.Toolset,
	req *rawRequest,
	chatIDHeader string,
	protocolVersionHeader string,
) (json.RawMessage, error) {
	// Census parity with the hosted dispatch.
	s.metrics.RecordMCPRequest(ctx, mcprequests.DeclaredProtocolVersion(protocolVersionHeader, req.Params), req.Method, mcpmetrics.SurfacePlatform)

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		start := time.Now()
		defer func() {
			s.metrics.RecordMCPRequestDuration(ctx, req.Method, requestContext.Host+requestContext.ReqURL, time.Since(start))
		}()
	}

	switch req.Method {
	case "ping":
		return handlePing(ctx, s.logger, req.ID, serverInfoPlatformToolset)
	case "initialize":
		return handlePlatformInitialize(ctx, s.logger, s.metrics, req)
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "tools/list":
		return s.listPlatformToolsetTools(ctx, authCtx, toolset, req)
	case "tools/call":
		return s.callPlatformToolsetTool(ctx, authCtx, toolset, req, chatIDHeader)
	default:
		return nil, oops.E(oops.CodeNotImplemented, nil, "%s: %s", req.Method, oops.MCPCodeMethodNotFound.Message())
	}
}

func handlePlatformInitialize(ctx context.Context, logger *slog.Logger, telemetry *mcpmetrics.Metrics, req *rawRequest) (json.RawMessage, error) {
	// This path answers ServedPlatformToolset unconditionally and does not
	// otherwise read the request params. Parsing them purely for telemetry is
	// the point: without it the platform surface is the one inbound path where
	// the client's requested revision is invisible, which reads as a hole in
	// the data rather than as "platform clients don't negotiate". Malformed
	// params must not fail the handshake, so a parse error is logged and the
	// requested version is simply left unrecorded.
	params, _, err := parseInitializeParams(req.Params)
	if err != nil {
		logger.WarnContext(ctx, "failed to parse platform mcp initialize params", attr.SlogError(err))
	}

	recordMCPProtocolVersionSpan(ctx, params.ProtocolVersion, mcpversions.ServedPlatformToolset)
	telemetry.RecordMCPInitialize(ctx, params.ProtocolVersion, mcpversions.ServedPlatformToolset)

	result := &result[initializeResult]{
		ID: req.ID,
		Result: initializeResult{
			ProtocolVersion: mcpversions.ServedPlatformToolset,
			Capabilities: map[string]json.RawMessage{
				"tools": json.RawMessage("{}"),
			},
			ServerInfo:   serverInfoPlatformToolset,
			Instructions: "",
		},
		serverIdentity: serverInfoPlatformToolset,
	}
	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize initialize response").LogError(ctx, logger)
	}
	return bs, nil
}

func (s *Service) listPlatformToolsetTools(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	toolset platformtools.Toolset,
	req *rawRequest,
) (json.RawMessage, error) {
	// Memoize per-request: every assistant memory tool currently shares one
	// feature flag, so a naive loop would issue N Redis lookups for the same
	// (org, feature) pair.
	featureCache := map[string]bool{}
	available := func(feature string) bool {
		return s.platformToolFeatureAvailable(ctx, authCtx.ActiveOrganizationID, feature, featureCache)
	}

	tools := make([]*toolListEntry, 0, len(toolset.Tools))
	for _, extra := range toolset.Tools {
		if extra.Executor == nil {
			continue
		}
		if !available(extra.RequiredFeature) {
			continue
		}
		entry := toolToListEntry(extra.Executor.Descriptor().ToTool(*authCtx.ProjectID))
		if entry != nil {
			tools = append(tools, entry)
		}
	}

	bs, err := json.Marshal(&result[toolsListResultTools]{
		ID:             req.ID,
		Result:         toolsListResultTools{Tools: tools},
		serverIdentity: serverInfoPlatformToolset,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").LogError(ctx, s.logger)
	}
	return bs, nil
}

func (s *Service) callPlatformToolsetTool(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	toolset platformtools.Toolset,
	req *rawRequest,
	chatIDHeader string,
) (json.RawMessage, error) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse tool call request").LogError(ctx, s.logger)
	}
	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "tool name is required").LogError(ctx, s.logger)
	}

	var matched platformtools.ExternalTool
	var found bool
	for _, extra := range toolset.Tools {
		if extra.Executor == nil {
			continue
		}
		if extra.Executor.Descriptor().Name == params.Name {
			matched = extra
			found = true
			break
		}
	}
	if !found {
		return nil, oops.E(oops.CodeNotFound, errors.New("tool not found"), "tool not found").LogError(ctx, s.logger)
	}
	if !s.platformToolFeatureAvailable(ctx, authCtx.ActiveOrganizationID, matched.RequiredFeature, nil) {
		return nil, oops.E(oops.CodeNotFound, nil, "tool not found").LogError(ctx, s.logger)
	}

	desc := matched.Executor.Descriptor()
	descriptor := &gateway.ToolDescriptor{
		ID:               desc.SyntheticID(),
		Name:             desc.Name,
		Description:      conv.PtrEmpty(desc.Description),
		DeploymentID:     "",
		ProjectID:        authCtx.ProjectID.String(),
		ProjectSlug:      conv.PtrValOrEmpty(authCtx.ProjectSlug, ""),
		OrganizationID:   authCtx.ActiveOrganizationID,
		OrganizationSlug: authCtx.OrganizationSlug,
		URN:              desc.ToolURN(),
	}
	plan := gateway.NewPlatformToolCallPlan(descriptor, &gateway.PlatformToolCallPlan{
		SourceSlug:  desc.SourceSlug,
		Managed:     desc.Managed,
		OwnerKind:   conv.PtrValOrEmpty(desc.OwnerKind, ""),
		OwnerID:     conv.PtrValOrEmpty(desc.OwnerID, ""),
		InputSchema: desc.InputSchema,
		// The toolset slice is authoritative; the runtime's URN registry can
		// hold a differently scoped variant of the same tool.
		Executor: matched.Executor,
	})

	ctx, logger := o11y.EnrichToolCallContext(ctx, s.logger, descriptor.OrganizationSlug, descriptor.ProjectSlug)

	rw := &toolCallResponseWriter{
		headers:    make(http.Header),
		body:       new(bytes.Buffer),
		statusCode: http.StatusOK,
	}

	gramEmail := conv.PtrValOrEmpty(authCtx.Email, "")
	toolCallEnv := toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  gramEmail,
		GramChatID: chatIDHeader,
		// Platform toolsets serve Gram's own tools, never customer functions.
		MCPClient: toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}

	var mcpURL string
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		mcpURL = requestContext.Host + requestContext.ReqURL
		s.metrics.RecordMCPToolCall(ctx, authCtx.ActiveOrganizationID, mcpURL, params.Name)
	}

	if err := checkToolUsageLimits(ctx, logger, authCtx.ActiveOrganizationID, authCtx.AccountType, s.billingRepository); err != nil {
		return nil, err
	}

	requestBodyBytes := params.Arguments
	requestBytes := int64(len(requestBodyBytes))
	var outputBytes int64
	platformToolsetSlug := toolset.Slug
	chatID := chatIDHeader
	if chatID == "" {
		if principal, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
			chatID = principal.ThreadID.String()
		}
	}

	// Platform toolsets are runtime-only, so every call here is assistant-
	// initiated; record the durable audit trail entry on dispatch, regardless
	// of how the tool execution turns out.
	if principal, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		recordAssistantToolCallAudit(ctx, logger, s.auditLogger, s.db, assistantToolCallAudit{
			organizationID: authCtx.ActiveOrganizationID,
			projectID:      *authCtx.ProjectID,
			principal:      principal,
			chatID:         chatID,
			toolsetSlug:    platformToolsetSlug,
			toolName:       params.Name,
			toolURN:        descriptor.URN,
			params:         params.Arguments,
		})
	}

	logAttrs := tm.HTTPLogAttributes{}
	defer func() {
		go s.billingTracker.TrackToolCallUsage(context.WithoutCancel(ctx), billing.ToolCallUsageEvent{
			OrganizationID:        authCtx.ActiveOrganizationID,
			RequestBytes:          requestBytes,
			OutputBytes:           outputBytes,
			ToolURN:               descriptor.URN.String(),
			ToolName:              params.Name,
			ProjectID:             authCtx.ProjectID.String(),
			ProjectSlug:           &descriptor.ProjectSlug,
			OrganizationSlug:      &descriptor.OrganizationSlug,
			ToolsetSlug:           &platformToolsetSlug,
			ToolsetID:             nil,
			ResponseStatusCode:    rw.statusCode,
			MCPURL:                &mcpURL,
			MCPSessionID:          nil,
			ChatID:                conv.PtrEmpty(chatID),
			Type:                  plan.BillingType,
			ResourceURI:           "",
			FunctionCPUUsage:      nil,
			FunctionMemUsage:      nil,
			FunctionExecutionTime: nil,
		})

		logAttrs[attr.EventSourceKey] = string(tm.EventSourceToolCall)
		logAttrs.RecordStatusCode(rw.statusCode)
		logAttrs.RecordRequestBody(requestBytes)
		logAttrs.RecordResponseBody(outputBytes)
		logAttrs.RecordTraceContext(ctx)
		logAttrs.RecordRequestBodyContent(requestBodyBytes)
		logAttrs.RecordResponseBodyContent(rw.body.Bytes())

		if chatID != "" {
			logAttrs[attr.GenAIConversationIDKey] = chatID
		}
		logAttrs.RecordToolsetSlug(platformToolsetSlug)
		logAttrs.RecordMCPURL(mcpURL)
		s.telemLogger.Log(ctx, tm.LogParams{
			Timestamp: time.Now(),
			ToolInfo: tm.ToolInfo{
				ID:             descriptor.ID,
				URN:            descriptor.URN.String(),
				Name:           descriptor.Name,
				ProjectID:      descriptor.ProjectID,
				DeploymentID:   descriptor.DeploymentID,
				OrganizationID: descriptor.OrganizationID,
				FunctionID:     nil,
			},
			UserInfo:   tm.UserInfoByID(authCtx.UserID),
			Attributes: logAttrs,
		})
	}()

	if err := s.toolProxy.Do(ctx, rw, bytes.NewReader(requestBodyBytes), toolCallEnv, plan, logAttrs); err != nil {
		failure := platformToolCallError(ctx, logger, err, attr.SlogToolName(params.Name))
		recordToolCallErrorStatus(ctx, rw, failure)
		return nil, failure
	}
	outputBytes = int64(rw.body.Len())

	chunk, structured, err := formatResult(*rw, plan.Kind)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to format platform tool call result").LogError(ctx, logger)
	}

	bs, err := json.Marshal(result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: structured,
			IsError:           rw.statusCode < 200 || rw.statusCode >= 300,
		},
		serverIdentity: serverInfoPlatformToolset,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call result").LogError(ctx, logger, attr.SlogToolName(params.Name))
	}
	return bs, nil
}

func platformToolCallError(ctx context.Context, logger *slog.Logger, err error, args ...slog.Attr) error {
	if rejected, ok := toolCallRejection(ctx, logger, err, args...); ok {
		return rejected
	}

	var shareableErr *oops.ShareableError
	if errors.As(err, &shareableErr) {
		return fmt.Errorf("execute platform tool: %w", err)
	}

	return oops.E(oops.CodeUnexpected, err, "failed to execute platform tool call").LogError(ctx, logger, args...)
}

// platformToolFeatureAvailable reports whether a platform tool gated on
// `feature` should be visible to `orgID`. Tools without a required feature
// are always available; gated tools without a wired-in checker fail closed so
// a missing dependency can't silently unmask a gated tool. A nil cache
// disables memoization; a non-nil cache is read and written so callers
// iterating over many tools avoid duplicate checker calls.
func (s *Service) platformToolFeatureAvailable(ctx context.Context, orgID, feature string, cache map[string]bool) bool {
	if feature == "" {
		return true
	}
	if s.platformFeatureChecker == nil {
		return false
	}
	if cache != nil {
		if v, ok := cache[feature]; ok {
			return v
		}
	}
	v := s.platformFeatureChecker(ctx, orgID, feature)
	if cache != nil {
		cache[feature] = v
	}
	return v
}
