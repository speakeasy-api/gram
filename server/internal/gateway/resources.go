package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func (tp *ToolProxy) ReadResource(
	ctx context.Context,
	w http.ResponseWriter,
	requestBody io.Reader,
	env ToolCallEnv,
	plan *ResourceCallPlan,
	attrRecorder tm.HTTPLogAttributes,
) (err error) {
	ctx, span := tp.tracer.Start(ctx, "gateway.readResource", trace.WithAttributes(
		attr.ResourceName(plan.Descriptor.Name),
		attr.ResourceID(plan.Descriptor.ID),
		attr.ProjectID(plan.Descriptor.ProjectID),
		attr.DeploymentID(plan.Descriptor.DeploymentID),
		attr.ToolCallSource(string(tp.source)),
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
		attr.SlogResourceID(plan.Descriptor.ID),
		attr.SlogResourceName(plan.Descriptor.Name),
		attr.SlogToolCallSource(string(tp.source)),
	)

	switch plan.Kind {
	case "":
		return oops.E(oops.CodeInvariantViolation, nil, "resource kind is not set").Log(ctx, tp.logger)
	case ResourceKindFunction:
		return tp.doFunctionResource(ctx, logger, w, requestBody, env, plan.Descriptor, plan.Function, attrRecorder)
	default:
		return fmt.Errorf("resource type not supported: %s", plan.Kind)
	}
}

func (tp *ToolProxy) doFunctionResource(
	ctx context.Context,
	logger *slog.Logger,
	w http.ResponseWriter,
	requestBody io.Reader,
	env ToolCallEnv,
	descriptor *ResourceDescriptor,
	plan *ResourceFunctionCallPlan,
	attrs tm.HTTPLogAttributes,
) error {
	span := trace.SpanFromContext(ctx)
	invocationID, err := uuid.NewV7()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to generate function invocation ID").Log(ctx, logger)
	}
	projectID, err := uuid.Parse(descriptor.ProjectID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid project id received for function resource call").Log(ctx, logger)
	}
	deploymentID, err := uuid.Parse(descriptor.DeploymentID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid deployment id received for function resource call").Log(ctx, logger)
	}
	functionID, err := uuid.Parse(plan.FunctionID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid function id received for function resource call").Log(ctx, logger)
	}
	accessID, err := uuid.Parse(plan.FunctionsAccessID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid function access id received for function resource call").Log(ctx, logger)
	}

	var input json.RawMessage
	if err := json.NewDecoder(requestBody).Decode(&input); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, logger)
	}

	payloadEnv := make(map[string]string)

	// Start with system environment variables (uppercase keys)
	for k, v := range env.SystemEnv.All() {
		payloadEnv[strings.ToUpper(k)] = v
	}

	// For each variable required by the function, allow user config to merge/override
	for _, varName := range plan.Variables {
		if val := env.UserConfig.Get(varName); val != "" {
			payloadEnv[varName] = val
		}
	}

	req, err := tp.functions.ReadResource(ctx, functions.RunnerResourceReadRequest{
		RunnerBaseRequest: functions.RunnerBaseRequest{
			InvocationID:      invocationID,
			OrganizationID:    descriptor.OrganizationID,
			OrganizationSlug:  descriptor.OrganizationSlug,
			ProjectID:         projectID,
			ProjectSlug:       descriptor.ProjectSlug,
			DeploymentID:      deploymentID,
			FunctionsID:       functionID,
			FunctionsAccessID: accessID,
			Input:             input,
			Environment:       payloadEnv,
		},
		ResourceURN:  descriptor.URN,
		ResourceURI:  descriptor.URI,
		ResourceName: descriptor.Name,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create function resource call request").Log(ctx, logger)
	}

	var responseStatusCode int
	defer func() {
		rawct := w.Header().Get("content-type")
		ct, _, err := mime.ParseMediaType(rawct)
		if err != nil {
			ct = rawct
		}
		ct = ct[:min(len(ct), 100)]

		logger.InfoContext(ctx, "function read resource completed",
			attr.SlogHTTPResponseStatusCode(responseStatusCode),
			attr.SlogHTTPRequestMethod(req.Method),
			attr.SlogHTTPResponseHeaderContentType(ct),
		)
		// Record metrics for the resource call, some cardinality is introduced with org and resource name we will keep an eye on it
		tp.metrics.RecordResourceCall(ctx, descriptor.OrganizationID, descriptor.URN, responseStatusCode)

		span.SetAttributes(attr.HTTPResponseStatusCode(responseStatusCode))
	}()

	return reverseProxyRequest(ctx, ReverseProxyOptions{
		Logger:                    logger,
		Tracer:                    tp.tracer,
		Writer:                    w,
		Request:                   req,
		URN:                       descriptor.URN.String(),
		Expression:                &FilterRequest{Type: "none", Filter: ""},
		FilterConfig:              DisableResponseFiltering,
		Policy:                    tp.policy,
		ResponseStatusCodeCapture: &responseStatusCode,
		Attributes:                attrs,
		VerifyResponse: func(resp *http.Response) error {
			if resp.Header.Get("Gram-Invoke-ID") != invocationID.String() {
				return fmt.Errorf("failed to verify function invocation ID")
			}
			return nil
		},
		ID:               descriptor.ID,
		Name:             descriptor.Name,
		DeploymentID:     descriptor.DeploymentID,
		ProjectID:        descriptor.ProjectID,
		ProjectSlug:      descriptor.ProjectSlug,
		OrganizationID:   descriptor.OrganizationID,
		OrganizationSlug: descriptor.OrganizationSlug,
	})
}
