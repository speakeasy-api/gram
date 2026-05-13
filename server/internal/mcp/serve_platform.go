package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gateway"
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
const PlatformToolsetRoute = "/x/platform-mcp/{toolsetSlug}"

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

	token := AuthorizationBearerToken(r)
	if token == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	authedCtx, _, err := s.assistantTokens.Authorize(ctx, token)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to authorize platform toolset request").Log(ctx, s.logger)
	}
	ctx = authedCtx

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "no project auth context").Log(ctx, s.logger)
	}

	bodyBytes, err := io.ReadAll(r.Body)
	switch {
	case errors.Is(err, io.EOF) || len(bodyBytes) == 0:
		return nil
	case err != nil:
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, s.logger)
	}

	if len(bodyBytes) > 0 && bodyBytes[0] == '[' {
		return oops.E(oops.CodeBadRequest, nil, "batch requests are not supported").Log(ctx, s.logger)
	}

	var req rawRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to decode request body").Log(ctx, s.logger)
	}
	if req.JSONRPC != "2.0" {
		return oops.E(oops.CodeBadRequest, errInvalidJSONRPCVersion, "unsupported JSON-RPC version").Log(ctx, s.logger)
	}

	body, err := s.handlePlatformToolsetRequest(ctx, authCtx, toolset, &req, r.Header.Get("Gram-Chat-ID"))
	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		bs, merr := json.Marshal(NewErrorFromCause(req.ID, err))
		if merr != nil {
			return oops.E(oops.CodeUnexpected, merr, "failed to serialize error response").Log(ctx, s.logger)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write(bs); writeErr != nil {
			return oops.E(oops.CodeUnexpected, writeErr, "failed to write error response body").Log(ctx, s.logger)
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

func (s *Service) handlePlatformToolsetRequest(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	toolset platformtools.Toolset,
	req *rawRequest,
	chatIDHeader string,
) (json.RawMessage, error) {
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		start := time.Now()
		defer func() {
			s.metrics.RecordMCPRequestDuration(ctx, req.Method, requestContext.Host+requestContext.ReqURL, time.Since(start))
		}()
	}

	switch req.Method {
	case "ping":
		return handlePing(ctx, s.logger, req.ID)
	case "initialize":
		return handlePlatformInitialize(ctx, s.logger, req)
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "tools/list":
		return s.listPlatformToolsetTools(ctx, authCtx, toolset, req)
	case "tools/call":
		return s.callPlatformToolsetTool(ctx, authCtx, toolset, req, chatIDHeader)
	default:
		return nil, &rpcError{
			ID:      req.ID,
			Code:    methodNotFound,
			Message: fmt.Sprintf("%s: %s", req.Method, methodNotFound.UserMessage()),
			Data:    nil,
		}
	}
}

func handlePlatformInitialize(ctx context.Context, logger *slog.Logger, req *rawRequest) (json.RawMessage, error) {
	result := &result[initializeResult]{
		ID: req.ID,
		Result: initializeResult{
			ProtocolVersion: "2025-03-26",
			Capabilities: map[string]json.RawMessage{
				"tools": json.RawMessage("{}"),
			},
			ServerInfo: serverInfo{
				Name:    "Gram Platform Toolset",
				Version: "0.0.0",
			},
			Instructions: "",
		},
	}
	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize initialize response").Log(ctx, logger)
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
		ID:     req.ID,
		Result: toolsListResultTools{Tools: tools},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse tool call request").Log(ctx, s.logger)
	}
	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "tool name is required").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeNotFound, errors.New("tool not found"), "tool not found").Log(ctx, s.logger)
	}
	if !s.platformToolFeatureAvailable(ctx, authCtx.ActiveOrganizationID, matched.RequiredFeature, nil) {
		return nil, oops.E(oops.CodeNotFound, nil, "tool not found").Log(ctx, s.logger)
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
		if authCtx.UserID != "" {
			logAttrs[attr.UserIDKey] = authCtx.UserID
		}
		if gramEmail != "" {
			logAttrs[attr.UserEmailKey] = gramEmail
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
			Attributes: logAttrs,
		})
	}()

	if err := s.toolProxy.Do(ctx, rw, bytes.NewReader(requestBodyBytes), toolCallEnv, plan, logAttrs); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to execute platform tool call").Log(ctx, logger, attr.SlogToolName(params.Name))
	}
	outputBytes = int64(rw.body.Len())

	chunk, err := formatResult(*rw, plan.Kind)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to format platform tool call result").Log(ctx, logger)
	}

	bs, err := json.Marshal(result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content: []json.RawMessage{chunk},
			IsError: rw.statusCode < 200 || rw.statusCode >= 300,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call result").Log(ctx, logger, attr.SlogToolName(params.Name))
	}
	return bs, nil
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
