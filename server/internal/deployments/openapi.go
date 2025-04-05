package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	gen "github.com/speakeasy-api/gram/gen/deployments"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/orderedmap"
	"github.com/speakeasy-api/gram/internal/tools"
	"gopkg.in/yaml.v3"
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

		for _, op := range ops {
			if op.operation == nil {
				continue
			}

			def, err := toolDefFromOpenAPIv3(op.method, path, op.operation, projectID, deploymentID, openapiDocID, docInfo.Slug)
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

func toolDefFromOpenAPIv3(method string, path string, op *v3.Operation, projectID uuid.UUID, deploymentID uuid.UUID, openapiDocID uuid.UUID, slug string) (repo.CreateOpenAPIv3ToolDefinitionParams, error) {
	headerParams := []*v3.Parameter{}
	queryParams := []*v3.Parameter{}
	pathParams := []*v3.Parameter{}

	for _, param := range op.Parameters {
		switch param.In {
		case "header":
			headerParams = append(headerParams, param)
		case "path":
			pathParams = append(pathParams, param)
		case "query":
			queryParams = append(queryParams, param)
		}
	}

	headerSchema, _, err := captureParameters(headerParams)
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting header parameters: %w", err)
	}

	querySchema, _, err := captureParameters(queryParams)
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting query parameters: %w", err)
	}

	pathSchema, _, err := captureParameters(pathParams)
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting path parameters: %w", err)
	}

	merged, err := groupJSONSchemaObjects("pathParameters", pathSchema, "headerParameters", headerSchema, "queryParameters", querySchema)
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error merging operation schemas: %w", err)
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
		DeploymentID:        uuid.NullUUID{UUID: deploymentID, Valid: deploymentID != uuid.Nil},
		Openapiv3DocumentID: uuid.NullUUID{UUID: openapiDocID, Valid: openapiDocID != uuid.Nil},
		Path:                path,
		HttpMethod:          strings.ToUpper(method),
		Openapiv3Operation:  conv.ToPGText(op.OperationId),
		Name:                tools.SanitizeName(fmt.Sprintf("%s_%s", slug, op.OperationId)),
		Tags:                op.Tags,
		Summary:             op.Summary,
		Description:         op.Description,
		SchemaVersion:       "1.0.0",
		Schema:              schemaBytes,
	}, nil
}

type jsonSchemaObject struct {
	Type       string                                   `json:"type,omitempty" yaml:"type,omitempty"`
	Required   []string                                 `json:"required,omitempty" yaml:"required,omitempty"`
	Properties *orderedmap.Map[string, json.RawMessage] `json:"properties,omitempty" yaml:"properties,omitempty"`
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
		schema, err := param.Schema.MarshalYAMLInline()
		if err != nil {
			return nil, nil, fmt.Errorf("%s: error inlining parameter schema: %w", param.Name, err)
		}

		yamlBytes, err := yaml.Marshal(schema)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: error yaml marshalling parameter schema: %w", param.Name, err)
		}

		var raw interface{}
		if err := yaml.Unmarshal(yamlBytes, &raw); err != nil {
			return nil, nil, fmt.Errorf("%s: error yaml unmarshalling parameter schema: %w", param.Name, err)
		}

		schemaBytes, err := json.Marshal(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: error json marshalling parameter schema: %w", param.Name, err)
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
