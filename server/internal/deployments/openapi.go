package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	gen "github.com/speakeasy-api/gram/gen/deployments"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/orderedmap"
	"github.com/speakeasy-api/gram/internal/tools"
	"gopkg.in/yaml.v3"
)

var (
	preferredRequestTypes = []*regexp.Regexp{
		regexp.MustCompile(`\bjson\b`),
		regexp.MustCompile(`^application/x-www-form-urlencoded\b`),
		regexp.MustCompile(`^multipart/form-data\b`),
		regexp.MustCompile(`^text/`),
	}
)

func (s *Service) processOpenAPIv3Document(ctx context.Context, logger *slog.Logger, tx *repo.Queries, projectID uuid.UUID, deploymentID uuid.UUID, openapiDocID uuid.UUID, docURL *url.URL, docInfo *gen.OpenAPIv3DeploymentAsset) ([]repo.CreateOpenAPIv3ToolDefinitionParams, error) {
	rc, err := s.assetStorage.Read(ctx, docURL.Path)
	if err != nil {
		logger.ErrorContext(ctx, "failed to fetch openapi document", slog.String("error", err.Error()))
		return nil, s.logDeploymentError(ctx, logger, tx, projectID, deploymentID, "error fetching openapi document")
	}
	defer rc.Close()

	doc, err := io.ReadAll(rc)
	if err != nil {
		logger.ErrorContext(ctx, "failed to read openapi document", slog.String("error", err.Error()))
		return nil, s.logDeploymentError(ctx, logger, tx, projectID, deploymentID, "error reading openapi document")
	}

	document, err := libopenapi.NewDocumentWithConfiguration(doc, &datamodel.DocumentConfiguration{
		AllowFileReferences:   false,
		AllowRemoteReferences: false,
		BundleInlineRefs:      true,
		ExcludeExtensionRefs:  true,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to open openapi document", slog.String("error", err.Error()))
		return nil, s.logDeploymentError(ctx, logger, tx, projectID, deploymentID, "error opening openapi document")
	}

	v3Model, errs := document.BuildV3Model()
	if len(errs) > 0 {
		return nil, fmt.Errorf("OpenAPI v3 document %s had %d errors: %s", docInfo.Name, len(errs), errors.Join(errs...))
	}

	defs := []repo.CreateOpenAPIv3ToolDefinitionParams{}
	for path, pitem := range v3Model.Model.Paths.PathItems.FromOldest() {
		ops := []openapiV3Operation{
			{method: "GET", operation: pitem.Get},
			{method: "POST", operation: pitem.Post},
			{method: "PUT", operation: pitem.Put},
			{method: "DELETE", operation: pitem.Delete},
			{method: "HEAD", operation: pitem.Head},
			{method: "PATCH", operation: pitem.Patch},
		}

		sharedParameters := pitem.Parameters

		for _, op := range ops {
			if op.operation == nil {
				continue
			}

			def, err := toolDefFromOpenAPIv3(ctx, logger, tx, op.method, path, op.operation, sharedParameters, projectID, deploymentID, openapiDocID, docInfo)
			if err != nil {
				if err := tx.LogDeploymentEvent(ctx, repo.LogDeploymentEventParams{
					DeploymentID: deploymentID,
					ProjectID:    projectID,
					Event:        "deployment:error",
					Message:      fmt.Sprintf("%s: %s: skipped operation due to error: %s", docInfo.Name, op.operation.OperationId, err.Error()),
				}); err != nil {
					logger.ErrorContext(ctx, "failed to log deployment event", slog.String("error", err.Error()))
				}

				continue
			}

			defs = append(defs, def)
		}
	}

	return defs, nil
}

type openapiV3Operation struct {
	method    string
	path      string
	operation *v3.Operation
}

func toolDefFromOpenAPIv3(ctx context.Context, logger *slog.Logger, tx *repo.Queries, method string, path string, op *v3.Operation, sharedParameters []*v3.Parameter, projectID uuid.UUID, deploymentID uuid.UUID, openapiDocID uuid.UUID, docInfo *gen.OpenAPIv3DeploymentAsset) (repo.CreateOpenAPIv3ToolDefinitionParams, error) {
	switch {
	case op.OperationId == "":
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("operation id is required [line: %d]", op.GoLow().KeyNode.Line)
	case len(op.Servers) > 0:
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("per-operation servers are not currently supported [line: %d]", op.GoLow().Servers.NodeLineNumber())
	// case len(op.Security) == 1 && op.Security[0].Requirements.Len() == 1 && op.Security[0].ContainsEmptyRequirement:
	// 	// This operation is public so we can allow it
	// case len(op.Security) > 0:
	// 	return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("per-operation security is not currently supported [line: %d]", op.GoLow().Security.NodeLineNumber())
	case op.Deprecated != nil && *op.Deprecated:
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("operation is deprecated [line: %d]", op.GoLow().Deprecated.NodeLineNumber())
	}

	if op.RequestBody != nil && op.RequestBody.Content != nil && op.RequestBody.Content.Len() > 1 {
		if err := tx.LogDeploymentEvent(ctx, repo.LogDeploymentEventParams{
			DeploymentID: deploymentID,
			ProjectID:    projectID,
			Event:        "deployment:warning",
			Message:      fmt.Sprintf("%s: %s: only one request body content type processed for operation", docInfo.Name, op.OperationId),
		}); err != nil {
			logger.ErrorContext(ctx, "failed to log deployment event", slog.String("error", err.Error()))
		}
	}

	requestBody, bodyRequired, err := captureRequestBody(op)
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error parsing request body: %w", err)
	}

	headerParams := orderedmap.New[string, *v3.Parameter]()
	queryParams := orderedmap.New[string, *v3.Parameter]()
	pathParams := orderedmap.New[string, *v3.Parameter]()

	for _, param := range append(sharedParameters, op.Parameters...) {
		switch param.In {
		case "header":
			headerParams.Set(param.Name, param)
		case "path":
			pathParams.Set(param.Name, param)
		case "query":
			queryParams.Set(param.Name, param)
		}
	}

	headerSchema, _, err := captureParameters(slices.Collect(headerParams.Values()))
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting header parameters: %w", err)
	}

	querySchema, _, err := captureParameters(slices.Collect(queryParams.Values()))
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting query parameters: %w", err)
	}

	pathSchema, _, err := captureParameters(slices.Collect(pathParams.Values()))
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting path parameters: %w", err)
	}

	merged, err := groupJSONSchemaObjects("pathParameters", pathSchema, "headerParameters", headerSchema, "queryParameters", querySchema)
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error merging operation schemas: %w", err)
	}

	if len(requestBody) > 0 {
		merged.Properties.Set("body", json.RawMessage(requestBody))
		if bodyRequired {
			merged.Required = append(merged.Required, "body")
		}
	}

	var schemaBytes []byte
	if merged.Properties.Len() > 0 {
		schemaBytes, err = json.Marshal(merged)
		if err != nil {
			return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error serializing operation schema: %w", err)
		}
	}

	return repo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           projectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{UUID: openapiDocID, Valid: openapiDocID != uuid.Nil},
		Path:                path,
		HttpMethod:          strings.ToUpper(method),
		Openapiv3Operation:  conv.ToPGText(op.OperationId),
		Name:                tools.SanitizeName(fmt.Sprintf("%s_%s", docInfo.Slug, op.OperationId)),
		Tags:                op.Tags,
		Summary:             op.Summary,
		Description:         op.Description,
		SchemaVersion:       "1.0.0",
		Schema:              schemaBytes,
	}, nil
}

type jsonSchemaObject struct {
	Type                 string                                   `json:"type,omitempty" yaml:"type,omitempty"`
	Required             []string                                 `json:"required,omitempty" yaml:"required,omitempty"`
	Properties           *orderedmap.Map[string, json.RawMessage] `json:"properties,omitempty" yaml:"properties,omitempty"`
	AdditionalProperties *bool                                    `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
}

func groupJSONSchemaObjects(keyvals ...any) (*jsonSchemaObject, error) {
	if len(keyvals) == 0 {
		return nil, nil
	}
	if len(keyvals)%2 != 0 {
		panic("groupJSONSchemaObjects: odd number of arguments")
	}

	result := jsonSchemaObject{
		Type:       "object",
		Required:   make([]string, 0, len(keyvals)/2),
		Properties: orderedmap.NewWithCapacity[string, json.RawMessage](len(keyvals) / 2),
	}

	for i, v := range keyvals {
		if (i+1)%2 != 0 {
			continue
		}

		key := keyvals[i-1].(string)
		schema := v.(*jsonSchemaObject)
		if schema == nil || schema.Properties == nil || schema.Properties.Len() == 0 {
			continue
		}

		serialized, err := json.Marshal(schema)
		if err != nil {
			return nil, fmt.Errorf("error marshalling '%s' schema: %w", key, err)
		}

		result.Properties.Set(key, json.RawMessage(serialized))
		if len(schema.Required) > 0 {
			result.Required = append(result.Required, key)
		}
	}

	return &result, nil
}

func captureRequestBody(op *v3.Operation) ([]byte, bool, error) {
	if op.RequestBody == nil || op.RequestBody.Content == nil || op.RequestBody.Content.Len() == 0 {
		return nil, false, nil
	}

	required := false
	if op.RequestBody.Required != nil {
		required = *op.RequestBody.Required
	}

	mediaType := ""
	var spec *v3.MediaType

	for mt, mtspec := range op.RequestBody.Content.FromOldest() {
		if slices.ContainsFunc(preferredRequestTypes, func(t *regexp.Regexp) bool {
			return t.MatchString(mt)
		}) {
			mediaType = mt
			spec = mtspec
			break
		}
	}

	if mediaType == "" {
		types := slices.Collect(op.RequestBody.Content.KeysFromOldest())
		return nil, false, fmt.Errorf("no supported request body content type found: %s", strings.Join(types, ", "))
	}

	if spec == nil {
		return []byte(`{"type":"object","additionalProperties":true}`), required, nil
	}

	schemaBytes, err := extractJSONSchemaFromYaml("requestBody", spec.Schema)
	if err != nil {
		return nil, false, fmt.Errorf("failed to extract json schema: %w", err)
	}

	return schemaBytes, required, nil
}

type openapiV3ParameterProxy struct {
	Schema          json.RawMessage `json:"schema,omitempty" yaml:"schema,omitempty"`
	In              string          `json:"in,omitempty" yaml:"in,omitempty"`
	Name            string          `json:"name,omitempty" yaml:"name,omitempty"`
	Description     string          `json:"description,omitempty" yaml:"description,omitempty"`
	Required        *bool           `json:"required,renderZero,omitempty" yaml:"required,renderZero,omitempty"`
	Deprecated      bool            `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	AllowEmptyValue bool            `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Style           string          `json:"style,omitempty" yaml:"style,omitempty"`
	Explode         *bool           `json:"explode,renderZero,omitempty" yaml:"explode,renderZero,omitempty"`
}

func captureParameters(params []*v3.Parameter) (objectSchema *jsonSchemaObject, spec []byte, err error) {
	if len(params) == 0 {
		return nil, nil, nil
	}

	obj := jsonSchemaObject{
		Type:       "object",
		Required:   make([]string, 0, len(params)),
		Properties: orderedmap.NewWithCapacity[string, json.RawMessage](len(params)),
	}

	specs := make(map[string]*openapiV3ParameterProxy, len(params))

	for _, param := range params {
		s := param.Schema.Schema()
		if s.Description == "" && param.Description != "" {
			s.Description = param.Description
		}

		schemaBytes, err := extractJSONSchemaFromYaml(param.Name, param.Schema)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to extract json schema: %w", err)
		}

		proxy := &openapiV3ParameterProxy{
			Schema:          json.RawMessage(schemaBytes),
			In:              param.In,
			Name:            param.Name,
			Description:     param.Description,
			Required:        param.Required,
			Deprecated:      param.Deprecated,
			AllowEmptyValue: param.AllowEmptyValue,
			Style:           param.Style,
			Explode:         param.Explode,
		}

		obj.Properties.Set(param.Name, proxy.Schema)
		if param.Required != nil && *param.Required {
			obj.Required = append(obj.Required, param.Name)
		}

		clone := *proxy
		clone.Schema = nil
		specs[param.Name] = &clone
	}

	spec, err = json.Marshal(specs)
	if err != nil {
		return nil, nil, fmt.Errorf("error marshalling parameter specifications: %w", err)
	}

	return &obj, spec, nil
}

func extractJSONSchemaFromYaml(name string, schemaProxy *base.SchemaProxy) ([]byte, error) {
	keyNode := schemaProxy.GoLow().GetKeyNode()
	line, col := keyNode.Line, keyNode.Column
	schema, err := schemaProxy.MarshalYAMLInline()
	if err != nil {
		return nil, fmt.Errorf("%s (%d:%d): error inlining schema: %w", name, line, col, err)
	}

	yamlBytes, err := yaml.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("%s (%d:%d): error yaml marshalling schema: %w", name, line, col, err)
	}

	var raw interface{}
	if err := yaml.Unmarshal(yamlBytes, &raw); err != nil {
		return nil, fmt.Errorf("%s (%d:%d): error yaml unmarshalling schema: %w", name, line, col, err)
	}

	schemaBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%s (%d:%d): error json marshalling schema: %w", name, line, col, err)
	}

	return schemaBytes, nil
}
