package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tm "github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (tp *ToolProxy) DoResource(
	ctx context.Context,
	w http.ResponseWriter,
	requestBody io.Reader,
	env *CaseInsensitiveEnv,
	plan *ResourceCallPlan,
) (err error) {
	ctx, span := tp.tracer.Start(ctx, "gateway.resourceCall", trace.WithAttributes(
		attr.ProjectID(plan.Descriptor.ProjectID),
		attr.DeploymentID(plan.Descriptor.DeploymentID),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	logger := tp.logger.With(
		attr.SlogProjectID(plan.Descriptor.ProjectID),
		attr.SlogDeploymentID(plan.Descriptor.DeploymentID),
	)

	if env == nil {
		env = NewCaseInsensitiveEnv()
	}

	switch plan.Kind {
	case "":
		return oops.E(oops.CodeInvariantViolation, nil, "tool kind is not set").Log(ctx, tp.logger)
	case ResourceKindFunction:
		return tp.doFunctionResource(ctx, logger, w, requestBody, env, plan.Descriptor, plan.Function)
	default:
		return fmt.Errorf("tool type not supported: %s", plan.Kind)
	}
}

// a lot of this is just a POC to get it working chating on some logging stuff.
// Resources will probably need its own proxy layer and we'll want to share what we can perhaps the reverse proxy layer
func (tp *ToolProxy) doFunctionResource(
	ctx context.Context,
	logger *slog.Logger,
	w http.ResponseWriter,
	requestBody io.Reader,
	env *CaseInsensitiveEnv,
	descriptor *ResourceDescriptor,
	plan *ResourceFunctionCallPlan,
) error {
	span := trace.SpanFromContext(ctx)
	invocationID, err := uuid.NewV7()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to generate function invocation ID").Log(ctx, logger)
	}

	projectID, err := uuid.Parse(descriptor.ProjectID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid project id received for function tool call").Log(ctx, logger)
	}
	deploymentID, err := uuid.Parse(descriptor.DeploymentID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid deployment id received for function tool call").Log(ctx, logger)
	}
	functionID, err := uuid.Parse(plan.FunctionID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid function id received for function tool call").Log(ctx, logger)
	}
	accessID, err := uuid.Parse(plan.FunctionsAccessID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid function access id received for function tool call").Log(ctx, logger)
	}

	var input json.RawMessage
	if err := json.NewDecoder(requestBody).Decode(&input); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, logger)
	}

	payloadEnv := make(map[string]string, len(plan.Variables))
	for _, v := range plan.Variables {
		if val := env.Get(v); val != "" {
			payloadEnv[v] = val
		}
	}

	// TODO: We cheat here. Resources probably need their own exposed fly entrypoint. We are stealing the tool one right now
	req, err := tp.functions.ToolCall(ctx, functions.RunnerToolCallRequest{
		InvocationID:      invocationID,
		OrganizationID:    descriptor.OrganizationID,
		OrganizationSlug:  descriptor.OrganizationSlug,
		ProjectID:         projectID,
		ProjectSlug:       descriptor.ProjectSlug,
		DeploymentID:      deploymentID,
		FunctionsID:       functionID,
		FunctionsAccessID: accessID,
		ToolURN:           urn.NewTool(urn.ToolKindFunction, "blah", "blah"), // cheating placholder
		ToolName:          descriptor.URI,                                    // cheating we treat the URI as the name right now without a separate entry point
		ToolInput:         input,
		ToolEnvironment:   payloadEnv,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create function tool call request").Log(ctx, logger)
	}

	var responseStatusCode int
	defer func() {
		rawct := w.Header().Get("content-type")
		ct, _, err := mime.ParseMediaType(rawct)
		if err != nil {
			ct = rawct
		}
		ct = ct[:min(len(ct), 100)]

		logger.InfoContext(ctx, "resource call",
			attr.SlogHTTPResponseStatusCode(responseStatusCode),
			attr.SlogHTTPResponseHeaderContentType(ct),
		)

		span.SetAttributes(attr.HTTPResponseStatusCode(responseStatusCode))
	}()

	return reverseProxyResource(
		ctx,
		logger,
		tp.tracer,
		w,
		req,
		descriptor,
		&FilterRequest{Type: "none", Filter: ""},
		DisableResponseFiltering,
		tp.policy,
		&responseStatusCode,
		tp.toolMetrics,
		func(resp *http.Response) error {
			if resp.Header.Get("Gram-Invoke-ID") != invocationID.String() {
				return fmt.Errorf("failed to verify function invocation ID")
			}
			return nil
		},
	)
}

func reverseProxyResource(
	ctx context.Context,
	logger *slog.Logger,
	tracer trace.Tracer,
	w http.ResponseWriter,
	req *http.Request,
	resource *ResourceDescriptor,
	expression *FilterRequest,
	filterConfig *ResponseFilter,
	policy *guardian.Policy,
	responseStatusCodeCapture *int,
	tcm tm.ToolMetricsProvider,
	verifyResponse func(*http.Response) error,
) error {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("resource_proxy.%s", resource.URI))
	defer span.End()

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           policy.Dialer().DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
	}

	// Wrap with HTTP logging round tripper
	loggingTransport := tm.NewHTTPLoggingRoundTripper(transport, tcm, logger, tracer)

	otelTransport := otelhttp.NewTransport(
		loggingTransport,
		otelhttp.WithPropagators(propagation.TraceContext{}),
	)

	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: otelTransport,
	}

	// Add resource info to context for the round tripper
	resourceInfo := &tm.ToolInfo{
		ID:             resource.ID,
		Urn:            resource.URN.String(),
		Name:           resource.Name,
		ProjectID:      resource.ProjectID,
		DeploymentID:   resource.DeploymentID,
		OrganizationID: resource.OrganizationID,
	}

	ctx = context.WithValue(ctx, tm.ToolInfoContextKey, resourceInfo)

	// Track request body size
	var requestBodySize int

	executeRequest := func() (*http.Response, error) {
		// Clone the request for each retry attempt
		retryReq := req.Clone(ctx)

		// Set the fresh body on the cloned request and wrap with counter
		if req.Body != nil && req.GetBody != nil {
			freshBody, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("retry: clone request body: %w", err)
			}

			// Wrap body to count bytes as they're sent
			retryReq.Body = tm.NewCountingReadCloser(freshBody, func(count int) {
				requestBodySize = count
			})
		}

		retryCtx := context.WithValue(retryReq.Context(), tm.RequestBodyContextKey, &requestBodySize)
		retryReq = retryReq.WithContext(retryCtx)

		return client.Do(retryReq)
	}
	resp, err := retryWithBackoff(ctx, retryConfig{
		initialInterval: 500 * time.Millisecond,
		maxInterval:     5 * time.Second,
		maxAttempts:     3,
		backoffFactor:   2,
		statusCodes: []int{ // reasonable status code presets
			408, // Request Timeout
			429, // Rate Limit Exceeded
			500, // Internal Server Error
			502, // Bad Gateway
			503, // Service Unavailable
			504, // Gateway Timeout
			509, // Bandwidth Limit Exceeded
			521, // Web Server Is Down (Cloudflare)
			522, // Connection Timed Out (Cloudflare)
			523, // Origin Is Unreachable (Cloudflare)
			524, // A Timeout Occurred (Cloudflare)
		},
		methods: []string{
			http.MethodGet,
		},
	}, executeRequest)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return oops.E(oops.CodeGatewayError, err, "failed to execute request").Log(ctx, logger)
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		return resp.Body.Close()
	})

	if err := verifyResponse(resp); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return oops.E(oops.CodeGatewayError, err, "tool call response verification failed").Log(ctx, logger)
	}

	if len(resp.Trailer) > 0 {
		var trailerKeys []string
		for key := range resp.Trailer {
			trailerKeys = append(trailerKeys, key)
		}
		w.Header().Set("Trailer", strings.Join(trailerKeys, ", "))
	}

	// We proxy over approved headers
	for key, values := range resp.Header {
		for _, value := range values {
			if slices.Contains(proxiedHeaders, key) {
				w.Header().Add(key, value)
			}
		}
	}

	// Copy cookies from response
	for _, cookie := range resp.Cookies() {
		http.SetCookie(w, cookie)
	}

	span.SetAttributes(attr.HTTPResponseExternal(true))
	w.Header().Set(constants.HeaderProxiedResponse, "1")

	finalStatusCode := resp.StatusCode
	var body io.Reader = resp.Body

	result := handleResponseFiltering(ctx, logger, filterConfig, expression, resp)
	if result != nil {
		w.Header().Set("Content-Type", result.contentType)
		w.Header().Set(constants.HeaderFilteredResponse, "1")
		span.SetAttributes(attr.HTTPResponseFiltered(true))
		finalStatusCode = result.statusCode
		body = result.resp
	}

	w.WriteHeader(finalStatusCode)
	if _, err := io.Copy(w, body); err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.ErrorContext(ctx, "failed to copy response body", attr.SlogError(err))
	}

	if responseStatusCodeCapture != nil {
		*responseStatusCodeCapture = finalStatusCode
	}

	return nil
}
