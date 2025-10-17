package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type resourceGetParams struct {
	URI string `json:"uri"`
}

type resourceGetResult struct {
	Contents []resourceContent `json:"contents"`
}

type resourceContent struct {
	URI      string  `json:"uri"`
	Name     *string `json:"name,omitempty"`
	MimeType *string `json:"mimeType,omitempty"`
	Text     string  `json:"text,omitempty"`
	Blob     string  `json:"blob,omitempty"`
}

func handleResourcesGet(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest, toolProxy *gateway.ToolProxy, env gateway.EnvironmentLoader) (json.RawMessage, error) {
	var params resourceGetParams
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
	var resourceName string
	for _, resource := range toolset.Resources {
		if resource.FunctionResourceDefinition != nil && resource.FunctionResourceDefinition.URI == params.URI {
			if err := resourceURN.UnmarshalText([]byte(resource.FunctionResourceDefinition.ResourceUrn)); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to parse resource URN").Log(ctx, logger)
			}
			resourceName = resource.FunctionResourceDefinition.Name
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

	ciEnv := gateway.NewCaseInsensitiveEnv()
	if payload.environment != "" && payload.authenticated {
		storedEnvVars, err := env.Load(ctx, payload.projectID, gateway.Slug(payload.environment))
		if err != nil && !errors.Is(err, gateway.ErrNotFound) {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, logger)
		}
		for k, v := range storedEnvVars {
			ciEnv.Set(k, v)
		}
	}

	for k, v := range payload.mcpEnvVariables {
		ciEnv.Set(k, v)
	}

	rw := &resourceResponseWriter{
		body: new(bytes.Buffer),
	}

	err = toolProxy.DoResource(ctx, rw, strings.NewReader("{}"), ciEnv, plan)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to execute resource call").Log(ctx, logger)
	}

	mimeType := "text/plain"
	if plan.Function != nil && plan.Function.MimeType != "" {
		mimeType = plan.Function.MimeType
	}

	isBinary := isBinaryMimeType(mimeType)

	content := resourceContent{
		URI:      params.URI,
		Name:     &resourceName,
		MimeType: &mimeType,
		Text:     "",
		Blob:     "",
	}

	if isBinary {
		content.Blob = base64.StdEncoding.EncodeToString(rw.body.Bytes())
	} else {
		content.Text = rw.body.String()
	}

	bs, err := json.Marshal(result[resourceGetResult]{
		ID: req.ID,
		Result: resourceGetResult{
			Contents: []resourceContent{content},
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/read result").Log(ctx, logger)
	}

	return bs, nil
}

// TODO: We are going to need a more comprehensive way to know this
func isBinaryMimeType(mimeType string) bool {
	mimeType = strings.ToLower(mimeType)

	if strings.HasPrefix(mimeType, "image/") {
		return true
	}
	if strings.HasPrefix(mimeType, "video/") {
		return true
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return true
	}
	if strings.HasPrefix(mimeType, "application/octet-stream") {
		return true
	}
	if strings.HasPrefix(mimeType, "application/pdf") {
		return true
	}
	if strings.HasPrefix(mimeType, "application/zip") {
		return true
	}

	return false
}

type resourceResponseWriter struct {
	body *bytes.Buffer
}

func (rw *resourceResponseWriter) Header() http.Header {
	return make(http.Header)
}

func (rw *resourceResponseWriter) Write(b []byte) (int, error) {
	n, err := rw.body.Write(b)
	if err != nil {
		return n, fmt.Errorf("write to resource buffer: %w", err)
	}
	return n, nil
}

func (rw *resourceResponseWriter) WriteHeader(statusCode int) {}
