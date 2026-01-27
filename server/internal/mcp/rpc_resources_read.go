package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type resourceReadParams struct {
	URI string `json:"uri"`
}

type resourceReadResult struct {
	Contents []resourceContent `json:"contents"`
}

type resourceContent struct {
	URI      string  `json:"uri"`
	Name     *string `json:"name,omitempty"`
	MimeType *string `json:"mimeType,omitempty"`
	Text     string  `json:"text,omitempty"`
	Blob     string  `json:"blob,omitempty"`
}

func handleResourcesRead(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	payload *mcpInputs,
	req *rawRequest,
	toolProxy *gateway.ToolProxy,
	env gateway.EnvironmentLoader,
	billingTracker billing.Tracker,
	billingRepository billing.Repository,
	telemSvc *tm.Service,
	featuresClient *productfeatures.Client) (json.RawMessage, error) {
	var params resourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse get resource request").Log(ctx, logger)
	}

	if params.URI == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "resource URI is required").Log(ctx, logger)
	}

	projectID := mv.ProjectID(payload.projectID)

	toolsetHelpers := toolsets.NewToolsets(db)
	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get toolset").Log(ctx, logger)
	}

	var resourceURN urn.Resource
	var resourceDef *types.FunctionResourceDefinition
	for _, resource := range toolset.Resources {
		if resource.FunctionResourceDefinition != nil && resource.FunctionResourceDefinition.URI == params.URI {
			if err := resourceURN.UnmarshalText([]byte(resource.FunctionResourceDefinition.ResourceUrn)); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to parse resource URN").Log(ctx, logger)
			}
			resourceDef = resource.FunctionResourceDefinition
			break
		}
	}

	if resourceURN.Kind == "" {
		return nil, oops.E(oops.CodeNotFound, nil, "resource not found").Log(ctx, logger)
	}

	plan, err := toolsetHelpers.GetResourceCallPlanByURN(ctx, resourceURN, uuid.UUID(projectID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get resource call plan").Log(ctx, logger)
	}

	descriptor := plan.Descriptor
	
	ctx, logger = o11y.EnrichToolCallContext(ctx, logger, descriptor.OrganizationSlug, descriptor.ProjectSlug)

	userConfig, err := resolveUserConfiguration(ctx, logger, env, payload, nil)
	if err != nil {
		return nil, err
	}

	toolsetID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "invalid toolset ID").Log(ctx, logger)
	}

	systemConfig, err := env.LoadSystemEnv(ctx, payload.projectID, toolsetID, string(resourceURN.Kind), resourceURN.Source)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load system environment").Log(ctx, logger)
	}

	rw := &resourceResponseWriter{
		statusCode: http.StatusOK,
		headers:    make(http.Header),
		body:       new(bytes.Buffer),
	}

	requestBodyBytes := []byte("{}")
	requestBytes := int64(len(requestBodyBytes))
	var outputBytes int64
	var functionCPU *float64
	var functionMem *float64
	var functionsExecutionTime *float64

	mcpURL := payload.sessionID
	err = checkToolUsageLimits(ctx, logger, toolset.OrganizationID, toolset.AccountType, billingRepository)
	if err != nil {
		return nil, err
	}

	logAttrs := tm.HTTPLogAttributes{}
	defer func() {
		// for billing purposes we still treat fetching a resource as a type of tool call right now
		go billingTracker.TrackToolCallUsage(context.WithoutCancel(ctx), billing.ToolCallUsageEvent{
			OrganizationID:        toolset.OrganizationID,
			RequestBytes:          requestBytes,
			OutputBytes:           outputBytes,
			ToolURN:               resourceURN.String(),
			ToolName:              resourceDef.Name,
			ResourceURI:           plan.Descriptor.URI,
			ProjectID:             payload.projectID.String(),
			ProjectSlug:           &descriptor.ProjectSlug,
			OrganizationSlug:      &descriptor.OrganizationSlug,
			ToolsetSlug:           &payload.toolset,
			ToolsetID:             &toolset.ID,
			MCPURL:                &mcpURL,
			MCPSessionID:          &payload.sessionID,
			ChatID:                nil,
			Type:                  plan.BillingType,
			ResponseStatusCode:    rw.statusCode,
			FunctionCPUUsage:      functionCPU,
			FunctionMemUsage:      functionMem,
			FunctionExecutionTime: functionsExecutionTime,
		})

		logAttrs.RecordStatusCode(rw.statusCode)
		logAttrs.RecordRequestBody(requestBytes)
		logAttrs.RecordResponseBody(outputBytes)

		params := tm.LogParams{
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
		}
		telemSvc.CreateLog(params)
	}()

	err = toolProxy.ReadResource(ctx, rw, strings.NewReader("{}"), gateway.ToolCallEnv{
		UserConfig: userConfig,
		SystemEnv:  systemConfig,
	}, plan, logAttrs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to execute resource call").Log(ctx, logger)
	}

	outputBytes = int64(rw.body.Len())

	// Extract function metrics from headers (originally trailers from functions runner)
	if cpuStr := rw.headers.Get(functions.FunctionsCPUHeader); cpuStr != "" {
		if cpu, err := strconv.ParseFloat(cpuStr, 64); err == nil {
			functionCPU = &cpu
		}
	}
	if memStr := rw.headers.Get(functions.FunctionsMemoryHeader); memStr != "" {
		if mem, err := strconv.ParseFloat(memStr, 64); err == nil {
			functionMem = &mem
		}
	}

	if execTimeStr := rw.headers.Get(functions.FunctionsExecutionTimeHeader); execTimeStr != "" {
		if execTime, err := strconv.ParseFloat(execTimeStr, 64); err == nil {
			functionsExecutionTime = &execTime
		}
	}

	if isMCPPassthrough(resourceDef.Meta) {
		// For MCP passthrough tools, return the raw result we get from the underlying mcp server
		bs, err := json.Marshal(result[json.RawMessage]{
			ID:     req.ID,
			Result: json.RawMessage(rw.body.Bytes()),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize MCP passthrough result").Log(ctx, logger)
		}

		return bs, nil
	}

	mimeType := "text/plain"
	if plan.Function != nil && plan.Function.MimeType != "" {
		mimeType = plan.Function.MimeType
	}

	isBinary := isBinaryMimeType(ctx, logger, mimeType)

	content := resourceContent{
		URI:      params.URI,
		Name:     &resourceDef.Name,
		MimeType: &mimeType,
		Text:     "",
		Blob:     "",
	}

	if isBinary {
		content.Blob = base64.StdEncoding.EncodeToString(rw.body.Bytes())
	} else {
		content.Text = rw.body.String()
	}

	bs, err := json.Marshal(result[resourceReadResult]{
		ID: req.ID,
		Result: resourceReadResult{
			Contents: []resourceContent{content},
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/read result").Log(ctx, logger)
	}

	return bs, nil
}

var textSuffixPattern = regexp.MustCompile(`\+(json|xml|yaml|yml|csv|toml)$`)

// isBinaryMimeType determines if a MIME type represents binary content that should be base64 encoded.
// According to MCP spec, binary resources should use base64 encoding while text resources use plain text.
func isBinaryMimeType(ctx context.Context, logger *slog.Logger, mimeType string) bool {
	parsedType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		logger.ErrorContext(ctx, "failed to parse MIME type", attr.SlogMimeType(mimeType), attr.SlogError(err))
		return false
	}

	// Check for structured syntax suffixes that indicate text formats e.g., application/vnd.api+json, application/hal+xml, etc.
	if textSuffixPattern.MatchString(parsedType) {
		return false
	}

	// Accepted binary MIME type prefixes
	binaryPrefixes := []string{
		"image/",
		"video/",
		"audio/",
		"application/octet-stream",
		"application/pdf",
		"application/zip",
		"application/gzip",
		"application/x-gzip",
		"application/x-tar",
		"application/x-bzip",
		"application/x-bzip2",
		"application/x-7z-compressed",
		"application/x-rar-compressed",
		"application/vnd.",
		"application/x-",
		"font/",
	}

	for _, prefix := range binaryPrefixes {
		if strings.HasPrefix(parsedType, prefix) {
			return true
		}
	}

	// Treat everything else as default text
	return false
}

type resourceResponseWriter struct {
	statusCode int
	headers    http.Header
	body       *bytes.Buffer
}

func (rw *resourceResponseWriter) Header() http.Header {
	return rw.headers
}

func (rw *resourceResponseWriter) Write(b []byte) (int, error) {
	n, err := rw.body.Write(b)
	if err != nil {
		return n, fmt.Errorf("write to resource buffer: %w", err)
	}
	return n, nil
}

func (rw *resourceResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
}
