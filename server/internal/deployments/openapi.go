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

	"github.com/ettle/strcase"
	"github.com/google/uuid"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"gopkg.in/yaml.v3"

	gen "github.com/speakeasy-api/gram/gen/deployments"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/inv"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/openapi"
	"github.com/speakeasy-api/gram/internal/orderedmap"
	"github.com/speakeasy-api/gram/internal/tools"
)

var (
	preferredRequestTypes = []*regexp.Regexp{
		regexp.MustCompile(`\bjson\b`),
		regexp.MustCompile(`^application/x-www-form-urlencoded\b`),
		regexp.MustCompile(`^multipart/form-data\b`),
		regexp.MustCompile(`^text/`),
	}
)

type openapiV3Task struct {
	projectID    uuid.UUID
	deploymentID uuid.UUID
	openapiDocID uuid.UUID
	docInfo      *gen.OpenAPIv3DeploymentAsset
	docURL       *url.URL
}

func (s *Service) processOpenAPIv3Document(ctx context.Context, logger *slog.Logger, tx *repo.Queries, task openapiV3Task) error {
	docURL := task.docURL
	projectID := task.projectID
	deploymentID := task.deploymentID
	openapiDocID := task.openapiDocID
	docInfo := task.docInfo
	if err := inv.Check("processOpenAPIv3Document",
		"doc url set", docURL != nil,
		"project id set", projectID != uuid.Nil,
		"deployment id set", deploymentID != uuid.Nil,
		"openapi doc id set", openapiDocID != uuid.Nil,
		"doc info set", docInfo != nil && docInfo.Name != "" && docInfo.Slug != "",
	); err != nil {
		return err
	}

	rc, err := s.assetStorage.Read(ctx, docURL)
	if err != nil {
		logger.ErrorContext(ctx, "failed to fetch openapi document", slog.String("error", err.Error()))
		return s.logDeploymentError(ctx, logger, tx, projectID, deploymentID, "error fetching openapi document")
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		return rc.Close()
	})

	doc, err := io.ReadAll(rc)
	if err != nil {
		logger.ErrorContext(ctx, "failed to read openapi document", slog.String("error", err.Error()))
		return s.logDeploymentError(ctx, logger, tx, projectID, deploymentID, "error reading openapi document")
	}

	document, err := libopenapi.NewDocumentWithConfiguration(doc, &datamodel.DocumentConfiguration{
		AllowFileReferences:   false,
		AllowRemoteReferences: false,
		BundleInlineRefs:      true,
		ExcludeExtensionRefs:  true,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to open openapi document", slog.String("error", err.Error()))
		return s.logDeploymentError(ctx, logger, tx, projectID, deploymentID, "error opening openapi document")
	}

	v3Model, errs := document.BuildV3Model()
	if len(errs) > 0 {
		return fmt.Errorf("OpenAPI v3 document '%s' had %d errors: %w", docInfo.Name, len(errs), errors.Join(errs...))
	}

	globalSecurity, err := serializeSecurity(v3Model.Model.Security)
	if err != nil {
		return fmt.Errorf("error serializing global security: %w", err)
	}

	securitySchemesParams, errs := securitySchemesFromOpenAPIv3(v3Model.Model, task)
	if len(errs) > 0 {
		for _, err := range errs {
			if logErr := s.logDeploymentError(
				ctx,
				logger,
				tx,
				projectID,
				deploymentID,
				fmt.Sprintf("%s: error parsing security schemes: %s", docInfo.Name, err.Error()),
			); logErr != nil {
				logger.ErrorContext(ctx, "failed to log deployment event", slog.String("error", logErr.Error()))
			}
		}
	}

	var writeErrCount int
	var writeErr error
	securitySchemes := make(map[string]repo.HttpSecurity, len(securitySchemesParams))
	for key, scheme := range securitySchemesParams {
		sec, err := tx.CreateHTTPSecurity(ctx, *scheme)
		if err != nil {
			if logErr := s.logDeploymentError(
				ctx,
				logger,
				tx,
				projectID,
				deploymentID,
				fmt.Sprintf("%s: error parsing security scheme: %s", docInfo.Name, err.Error()),
			); logErr != nil {
				logger.ErrorContext(ctx, "failed to log deployment event", slog.String("error", logErr.Error()))
			}

			return fmt.Errorf("%s: error saving security scheme: %w", docInfo.Name, err)
		}

		securitySchemes[key] = sec
	}

	globalServerEnvVar := strcase.ToSNAKE(string(docInfo.Slug) + "_SERVER_URL")
	globalDefaultServer := s.extractDefaultServer(ctx, logger, tx, projectID, deploymentID, docInfo, v3Model.Model.Servers)

	for path, pathItem := range v3Model.Model.Paths.PathItems.FromOldest() {
		ops := []openapiV3Operation{
			{method: "GET", operation: pathItem.Get, path: path},
			{method: "POST", operation: pathItem.Post, path: path},
			{method: "PUT", operation: pathItem.Put, path: path},
			{method: "DELETE", operation: pathItem.Delete, path: path},
			{method: "HEAD", operation: pathItem.Head, path: path},
			{method: "PATCH", operation: pathItem.Patch, path: path},
		}

		sharedParameters := pathItem.Parameters

		for _, op := range ops {
			if op.operation == nil {
				continue
			}

			// TODO: Currently ignoring servers at path item level until we
			// figure out how to name env variable

			def, err := s.toolDefFromOpenAPIv3(ctx, logger, tx, openapiV3OperationTask{
				openapiV3Task:    task,
				method:           op.method,
				path:             path,
				operation:        op.operation,
				sharedParameters: sharedParameters,
				globalSecurity:   globalSecurity,
				serverEnvVar:     globalServerEnvVar,
				defaultServer:    globalDefaultServer,
			})
			if err != nil {
				if logErr := tx.LogDeploymentEvent(ctx, repo.LogDeploymentEventParams{
					DeploymentID: deploymentID,
					ProjectID:    projectID,
					Event:        "deployment:error",
					Message:      fmt.Sprintf("%s: %s: skipped operation due to error: %s", docInfo.Name, op.operation.OperationId, err.Error()),
				}); logErr != nil {
					logger.ErrorContext(ctx, "failed to log deployment event", slog.String("error", err.Error()), slog.String("log_error", logErr.Error()))
				}

				continue
			}

			if _, err := tx.CreateOpenAPIv3ToolDefinition(ctx, def); err != nil {
				writeErr = err
				writeErrCount++
			}
		}
	}

	if writeErrCount > 0 {
		return fmt.Errorf("%s: error writing tools definitions: %w", docInfo.Name, writeErr)
	}

	return nil
}

func (s *Service) extractDefaultServer(ctx context.Context, logger *slog.Logger, tx *repo.Queries, projectID, deploymentID uuid.UUID, docInfo *gen.OpenAPIv3DeploymentAsset, servers []*v3.Server) *string {
	for _, server := range servers {
		low := server.GoLow()
		line, col := low.KeyNode.Line, low.KeyNode.Column

		if server.Variables == nil || server.Variables.Len() == 0 {
			u, err := url.Parse(server.URL)
			if err != nil {
				if logErr := s.logDeploymentError(
					ctx,
					logger,
					tx,
					projectID,
					deploymentID,
					fmt.Sprintf("%s: %s: skipping server due to malformed url [%d:%d]: %s", docInfo.Name, server.URL, line, col, err.Error()),
				); logErr != nil {
					logger.ErrorContext(ctx, "failed to log deployment event", slog.String("error", logErr.Error()))
				}
				continue
			}

			if u.Scheme != "https" {
				if logErr := s.logDeploymentError(
					ctx,
					logger,
					tx,
					projectID,
					deploymentID,
					fmt.Sprintf("%s: %s: skipping non-https server url [%d:%d]", docInfo.Name, server.URL, line, col),
				); logErr != nil {
					logger.ErrorContext(ctx, "failed to log deployment event", slog.String("error", logErr.Error()))
				}
				continue
			}

			return &server.URL
		}
	}

	return nil
}

type openapiV3OperationTask struct {
	openapiV3Task    openapiV3Task
	method           string
	path             string
	operation        *v3.Operation
	sharedParameters []*v3.Parameter
	globalSecurity   []byte
	serverEnvVar     string
	defaultServer    *string
}

type openapiV3Operation struct {
	method    string
	path      string
	operation *v3.Operation
}

func (s *Service) toolDefFromOpenAPIv3(ctx context.Context, logger *slog.Logger, tx *repo.Queries, task openapiV3OperationTask) (repo.CreateOpenAPIv3ToolDefinitionParams, error) {
	projectID := task.openapiV3Task.projectID
	deploymentID := task.openapiV3Task.deploymentID
	openapiDocID := task.openapiV3Task.openapiDocID
	docInfo := task.openapiV3Task.docInfo
	method := task.method
	path := task.path
	op := task.operation
	sharedParameters := task.sharedParameters
	globalSecurity := task.globalSecurity
	serverEnvVar := task.serverEnvVar
	defaultServer := task.defaultServer
	if err := inv.Check("toolDefFromOpenAPIv3",
		"project id set", projectID != uuid.Nil,
		"deployment id set", deploymentID != uuid.Nil,
		"openapi doc id set", openapiDocID != uuid.Nil,
		"doc info set", docInfo != nil && docInfo.Name != "" && docInfo.Slug != "",
		"method set", method != "",
		"path set", path != "",
		"operation set", op != nil,
		"server env var set", serverEnvVar != "",
	); err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, err
	}

	switch {
	case op.OperationId == "":
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("operation id is required [line: %d]", op.GoLow().KeyNode.Line)
	case len(op.Servers) > 0:
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("per-operation servers are not currently supported [line: %d]", op.GoLow().Servers.NodeLineNumber())
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

	bodyResult, err := captureRequestBody(op)
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

	headerSchema, headerSettings, err := captureParameters(slices.Collect(headerParams.Values()))
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting header parameters: %w", err)
	}

	querySchema, querySettings, err := captureParameters(slices.Collect(queryParams.Values()))
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting query parameters: %w", err)
	}

	pathSchema, pathSettings, err := captureParameters(slices.Collect(pathParams.Values()))
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error collecting path parameters: %w", err)
	}

	merged, err := groupJSONSchemaObjects("pathParameters", pathSchema, "headerParameters", headerSchema, "queryParameters", querySchema)
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error merging operation schemas: %w", err)
	}

	var requestContentType *string
	if bodyResult.valid {
		merged.Properties.Set("body", json.RawMessage(bodyResult.schema))
		if bodyResult.required {
			merged.Required = append(merged.Required, "body")
		}
		requestContentType = &bodyResult.contentType
	}

	var schemaBytes []byte
	if merged.Properties.Len() > 0 {
		schemaBytes, err = json.Marshal(merged)
		if err != nil {
			return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error serializing operation schema: %w", err)
		}
	}

	security, err := serializeSecurity(op.Security)
	if err != nil {
		low := op.GoLow()
		loc := "-"
		if low.Security.KeyNode != nil {
			loc = fmt.Sprintf("%d:%d", low.Security.KeyNode.Line, low.Security.KeyNode.Column)
		}

		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error serializing operation security [%s]: %w", loc, err)
	}

	if len(security) == 0 {
		security = globalSecurity
	}

	if len(op.Servers) > 0 {
		serverEnvVar = strcase.ToSNAKE(fmt.Sprintf("%s_%s_SERVER_URL", docInfo.Slug, op.OperationId))
		defaultServer = s.extractDefaultServer(ctx, logger, tx, projectID, deploymentID, docInfo, op.Servers)
	}

	return repo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           projectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{UUID: openapiDocID, Valid: openapiDocID != uuid.Nil},
		Security:            security,
		Path:                path,
		HttpMethod:          strings.ToUpper(method),
		Openapiv3Operation:  conv.ToPGText(op.OperationId),
		Name:                tools.SanitizeName(fmt.Sprintf("%s_%s", docInfo.Slug, op.OperationId)),
		Tags:                op.Tags,
		Summary:             op.Summary,
		Description:         op.Description,
		SchemaVersion:       "1.0.0",
		Schema:              schemaBytes,
		ServerEnvVar:        serverEnvVar,
		DefaultServerUrl:    conv.PtrToPGText(defaultServer),
		HeaderSettings:      headerSettings,
		QuerySettings:       querySettings,
		PathSettings:        pathSettings,
		RequestContentType:  conv.PtrToPGText(requestContentType),
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
		Type:                 "object",
		Required:             make([]string, 0, len(keyvals)/2),
		Properties:           orderedmap.NewWithCapacity[string, json.RawMessage](len(keyvals) / 2),
		AdditionalProperties: conv.Ptr(false),
	}

	for i, v := range keyvals {
		if (i+1)%2 != 0 {
			continue
		}

		key, ok := keyvals[i-1].(string)
		if !ok {
			panic(fmt.Sprintf("groupJSONSchemaObjects: expected string key, got %T", keyvals[i-1]))
		}

		schema, ok := v.(*jsonSchemaObject)
		if !ok {
			panic(fmt.Sprintf("groupJSONSchemaObjects: expected *jsonSchemaObject value, got %T", v))
		}

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

type capturedRequestBody struct {
	valid       bool
	schema      []byte
	required    bool
	contentType string
}

func captureRequestBody(op *v3.Operation) (capturedRequestBody, error) {
	empty := capturedRequestBody{
		valid:       false,
		schema:      nil,
		required:    false,
		contentType: "",
	}

	if op.RequestBody == nil || op.RequestBody.Content == nil || op.RequestBody.Content.Len() == 0 {
		return empty, nil
	}

	required := false
	if op.RequestBody.Required != nil {
		required = *op.RequestBody.Required
	}

	contentType := ""
	var spec *v3.MediaType

	for mt, mtspec := range op.RequestBody.Content.FromOldest() {
		if slices.ContainsFunc(preferredRequestTypes, func(t *regexp.Regexp) bool {
			return t.MatchString(mt)
		}) {
			contentType = mt
			spec = mtspec
			break
		}
	}

	if contentType == "" {
		types := slices.Collect(op.RequestBody.Content.KeysFromOldest())
		return empty, fmt.Errorf("no supported request body content type found: %s", strings.Join(types, ", "))
	}

	if spec == nil {
		return capturedRequestBody{
			valid:       true,
			schema:      []byte(`{"type":"object","additionalProperties":true}`),
			required:    required,
			contentType: contentType,
		}, nil
	}

	schemaBytes, err := extractJSONSchemaFromYaml("requestBody", spec.Schema)
	if err != nil {
		return empty, fmt.Errorf("failed to extract json schema: %w", err)
	}

	return capturedRequestBody{
		valid:       true,
		schema:      schemaBytes,
		required:    required,
		contentType: contentType,
	}, nil
}

func captureParameters(params []*v3.Parameter) (objectSchema *jsonSchemaObject, spec []byte, err error) {
	if len(params) == 0 {
		return nil, nil, nil
	}

	obj := jsonSchemaObject{
		Type:                 "object",
		Required:             make([]string, 0, len(params)),
		Properties:           orderedmap.NewWithCapacity[string, json.RawMessage](len(params)),
		AdditionalProperties: conv.Ptr(false),
	}

	specs := make(map[string]*openapi.OpenapiV3ParameterProxy, len(params))

	for _, param := range params {
		var schemaBytes []byte

		if param.Schema == nil {
			schemaBytes = []byte(`{"type":"string"}`)
		} else {
			s := param.Schema.Schema()
			if s != nil && s.Description == "" && param.Description != "" {
				s.Description = param.Description
			}

			sb, err := extractJSONSchemaFromYaml(param.Name, param.Schema)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to extract json schema: %w", err)
			}

			schemaBytes = sb
		}

		proxy := &openapi.OpenapiV3ParameterProxy{
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
		// We don't need the schema when plucking out the serialzating settings
		// for a parameter. It would only bloat the database so we're stripping
		// it out before storing.
		clone.Schema = nil
		specs[param.Name] = &clone
	}

	spec, err = json.Marshal(specs)
	if err != nil {
		return nil, nil, fmt.Errorf("error marshalling parameter specifications: %w", err)
	}

	return &obj, spec, nil
}

func serializeSecurity(security []*base.SecurityRequirement) ([]byte, error) {
	if len(security) == 0 {
		return nil, nil
	}

	acc := make([]map[string][]string, 0, len(security))
	for _, group := range security {
		if group.ContainsEmptyRequirement {
			acc = append(acc, make(map[string][]string))
			continue
		}

		req := make(map[string][]string, group.Requirements.Len())
		for key, val := range group.Requirements.FromOldest() {
			req[key] = append([]string{}, val...)
		}

		acc = append(acc, req)
	}

	return json.Marshal(acc)
}

func securitySchemesFromOpenAPIv3(doc v3.Document, task openapiV3Task) (map[string]*repo.CreateHTTPSecurityParams, []error) {
	slug := string(task.docInfo.Slug)
	if doc.Components == nil || doc.Components.SecuritySchemes == nil || doc.Components.SecuritySchemes.Len() == 0 {
		return nil, nil
	}

	var errs []error

	res := make(map[string]*repo.CreateHTTPSecurityParams)
	for key, sec := range doc.Components.SecuritySchemes.FromOldest() {
		low := sec.GoLow()
		line, col := low.KeyNode.Line, low.KeyNode.Column
		var envvars []string

		switch sec.Type {
		case "apiKey":
			envvars = append(envvars, strcase.ToSNAKE(slug+"_"+key))
		case "http":
			switch sec.Scheme {
			case "bearer":
				envvars = append(envvars, strcase.ToSNAKE(slug+"_"+key))
			case "basic":
				envvars = append(envvars, strcase.ToSNAKE(slug+"_"+key+"_USERNAME"))
				envvars = append(envvars, strcase.ToSNAKE(slug+"_"+key+"_PASSWORD"))
			default:
				errs = append(errs, fmt.Errorf("%s (%d:%d) unsupported http security scheme: %s", key, line, col, sec.Scheme))
				continue
			}
		default:
			errs = append(errs, fmt.Errorf("%s (%d:%d) unsupported security scheme type: %s", key, line, col, sec.Type))
			continue
		}

		res[key] = &repo.CreateHTTPSecurityParams{
			Key:          key,
			DeploymentID: task.deploymentID,
			Type:         conv.ToPGText(sec.Type),
			Name:         conv.ToPGTextEmpty(sec.Name),
			InPlacement:  conv.ToPGTextEmpty(sec.In),
			Scheme:       conv.ToPGTextEmpty(sec.Scheme),
			BearerFormat: conv.ToPGTextEmpty(sec.BearerFormat),
			EnvVariables: envvars,
		}
	}

	return res, errs
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
