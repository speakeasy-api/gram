package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ettle/strcase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orderedmap"
	"github.com/speakeasy-api/openapi/hashing"
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/pointer"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/speakeasy-api/openapi/yml"
)

func (p *ToolExtractor) doSpeakeasy(
	ctx context.Context,
	logger *slog.Logger,
	tx *repo.Queries,
	data []byte,
	task ToolExtractorTask,
) (*ToolExtractorResult, error) {
	docInfo := task.DocInfo

	doc, _, err := openapi.Unmarshal(ctx, bytes.NewReader(data), openapi.WithSkipValidation())
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, oops.Perm(err), "error opening openapi document").Log(ctx, logger)
	}

	upgradeStart := time.Now()
	upgradeResult, err := upgradeOpenAPI30To31Speakeasy(ctx, doc)
	upgradeDuration := time.Since(upgradeStart)
	var upgradeOutcome *o11y.Outcome
	if err != nil {
		upgradeOutcome = pointer.From(o11y.OutcomeFailure)
		logger.ErrorContext(ctx, "Unable to upgrade OpenAPI v3.0 document to v3.1. Proceeding with v3.0 document.", attr.SlogEvent("openapi-upgrade:error"))
		logger.ErrorContext(ctx, err.Error(), attr.SlogEvent("openapi-upgrade:error"))
	} else {
		doc = upgradeResult.Document

		if upgradeResult.Upgraded {
			upgradeOutcome = pointer.From(o11y.OutcomeSuccess)
		}

		if len(upgradeResult.Issues) > 0 {
			msg := fmt.Sprintf("Found %d issues upgrading OpenAPI v3.0 document to v3.1", len(upgradeResult.Issues))
			logger.ErrorContext(ctx, msg, attr.SlogEvent("openapi-upgrade:error"))
			for i, issue := range upgradeResult.Issues {
				if i >= 30 {
					break
				}
				logger.ErrorContext(ctx, issue.Error(), attr.SlogEvent("openapi-upgrade:error"))
			}
		}
	}

	globalSecurity, err := serializeSecuritySpeakeasy(doc.GetSecurity())
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, oops.Perm(err), "error serializing global security").Log(ctx, logger)
	}

	securitySchemesParams, errs := extractSecuritySchemesSpeakeasy(ctx, logger, docInfo, doc, task)
	if len(errs) > 0 {
		for _, err := range errs {
			_ = oops.E(oops.CodeUnexpected, err, "%s: error parsing security schemes: %s", docInfo.Name, err.Error()).Log(ctx, logger)
		}
	}

	var writeErrCount int
	var writeErr error
	securitySchemes := make(map[string]repo.HttpSecurity, len(securitySchemesParams))
	for key, scheme := range securitySchemesParams {
		sec, err := tx.CreateHTTPSecurity(ctx, *scheme)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, oops.Perm(err), "%s: error writing security scheme: %s", docInfo.Name, err.Error()).Log(ctx, logger)
		}

		securitySchemes[key] = sec
	}

	globalServerEnvVar := strcase.ToSNAKE(string(docInfo.Slug) + "_SERVER_URL")
	globalDefaultServer := extractDefaultServerSpeakeasy(ctx, logger, docInfo, doc.GetServers())

	for path, pi := range doc.Paths.All() {
		_, err := pi.Resolve(ctx, openapi.ResolveOptions{
			TargetLocation:      "/",
			RootDocument:        doc,
			DisableExternalRefs: true,
			SkipValidation:      true,
		})
		if err != nil {
			logger.ErrorContext(ctx, fmt.Sprintf("%s: %s %s", docInfo.Name, "error resolving path", err.Error()), attr.SlogEvent("openapi:error"))
			continue
		}

		pathItem := pi.GetObject()

		ops := []operationMetadata[openapi.Operation]{
			{method: "GET", operation: pathItem.Get(), path: path},
			{method: "POST", operation: pathItem.Post(), path: path},
			{method: "PUT", operation: pathItem.Put(), path: path},
			{method: "DELETE", operation: pathItem.Delete(), path: path},
			{method: "HEAD", operation: pathItem.Head(), path: path},
			{method: "PATCH", operation: pathItem.Patch(), path: path},
		}

		sharedParameters := pathItem.GetParameters()

		for _, op := range ops {
			if op.operation == nil {
				continue
			}

			// TODO: Currently ignoring servers at path item level until we
			// figure out how to name env variable

			opID := op.operation.GetOperationID()
			if opID == "" {
				opID = fmt.Sprintf("%s_%s", op.method, path)
			}

			def, err := extractToolDefSpeakeasy(ctx, logger, tx, doc, operationTask[openapi.Operation, openapi.ReferencedParameter]{
				extractTask:      task,
				method:           op.method,
				path:             path,
				opID:             opID,
				operation:        op.operation,
				sharedParameters: sharedParameters,
				globalSecurity:   globalSecurity,
				serverEnvVar:     globalServerEnvVar,
				defaultServer:    globalDefaultServer,
			})
			if err != nil {
				if task.OnOperationSkipped != nil {
					task.OnOperationSkipped(err)
				}
				_ = oops.E(oops.CodeUnexpected, err, "%s: %s: skipped operation due to error: %s", docInfo.Name, opID, err.Error()).Log(ctx, logger)
				continue
			}

			if _, err := tx.CreateOpenAPIv3ToolDefinition(ctx, def); err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) {
					// Special logging for path constraint violations
					if pgErr.ConstraintName == "http_tool_definitions_path_check" {
						logger.ErrorContext(ctx, "path exceeds 2000 character limit",
							attr.SlogEvent("openapi:error:path-too-long"),
							attr.SlogOpenAPIOperationID(opID),
							attr.SlogOpenAPIPath(path),
							attr.SlogValueInt(len(path)),
							attr.SlogOpenAPIMethod(op.method),
						)
					}
					err = fmt.Errorf("%s: %s %s (SQLSTATE %s)", docInfo.Name, pgErr.Message, pgErr.Detail, pgErr.Code)
				}
				// Only capture the first error as the rest will just be transaction aborted errors
				if writeErr == nil {
					writeErr = err
				}
				writeErrCount++
			}
		}
	}

	if writeErrCount > 0 {
		err := oops.Perm(fmt.Errorf("%s: error writing tools definitions: %w", docInfo.Name, writeErr))
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save %d tool definitions", writeErrCount).Log(ctx, logger)
	}

	return &ToolExtractorResult{
		DocumentVersion:         doc.OpenAPI,
		DocumentUpgrade:         upgradeOutcome,
		DocumentUpgradeDuration: upgradeDuration,
	}, nil
}

func serializeSecuritySpeakeasy(security []*openapi.SecurityRequirement) ([]byte, error) {
	if len(security) == 0 {
		return nil, nil
	}

	acc := make([]map[string][]string, 0, len(security))
	for _, group := range security {
		containsEmptyRequirement := group.Len() == 0
		if containsEmptyRequirement {
			acc = append(acc, make(map[string][]string))
			continue
		}

		req := make(map[string][]string, group.Len())
		for key, val := range group.All() {
			req[key] = append([]string{}, val...)
		}

		acc = append(acc, req)
	}

	bs, err := json.Marshal(acc)
	if err != nil {
		return nil, fmt.Errorf("error serializing security requirements: %w", err)
	}

	return bs, nil
}

func extractSecuritySchemesSpeakeasy(ctx context.Context, logger *slog.Logger, docInfo *types.OpenAPIv3DeploymentAsset, doc *openapi.OpenAPI, task ToolExtractorTask) (map[string]*repo.CreateHTTPSecurityParams, []error) {
	slug := string(task.DocInfo.Slug)

	if doc.Components == nil || doc.Components.SecuritySchemes == nil || doc.Components.SecuritySchemes.Len() == 0 {
		return nil, nil
	}

	var errs []error

	res := make(map[string]*repo.CreateHTTPSecurityParams)
	for key, s := range doc.GetComponents().GetSecuritySchemes().All() {
		_, err := s.Resolve(ctx, openapi.ResolveOptions{
			TargetLocation:      "/",
			RootDocument:        doc,
			DisableExternalRefs: true,
			SkipValidation:      true,
		})
		if err != nil {
			logger.ErrorContext(ctx, fmt.Sprintf("%s: %s %s", docInfo.Name, "error resolving security scheme", err.Error()), attr.SlogEvent("openapi:error"))
			continue
		}
		sec := s.GetObject()

		line, col := sec.GetRootNodeLine(), sec.GetRootNodeColumn()

		var envvars []string
		var oauthTypes []string
		var oauthFlows []byte

		switch sec.GetType() {
		case openapi.SecuritySchemeTypeAPIKey:
			envvars = append(envvars, strcase.ToSNAKE(slug+"_"+key))
		case openapi.SecuritySchemeTypeHTTP:
			switch sec.GetScheme() {
			case "bearer":
				envvars = append(envvars, strcase.ToSNAKE(slug+"_"+key))
			case "basic":
				envvars = append(envvars, strcase.ToSNAKE(slug+"_"+key+"_USERNAME"))
				envvars = append(envvars, strcase.ToSNAKE(slug+"_"+key+"_PASSWORD"))
			default:
				errs = append(errs, fmt.Errorf("%s (%d:%d) unsupported http security scheme: %s", key, line, col, sec.GetScheme()))
				continue
			}
		case openapi.SecuritySchemeTypeOAuth2:
			if sec.GetFlows() != nil {
				if sec.GetFlows().AuthorizationCode != nil || sec.GetFlows().ClientCredentials != nil || sec.GetFlows().Implicit != nil {
					envvars = append(envvars, strcase.ToSNAKE(slug+"_ACCESS_TOKEN"))
				}

				if sec.GetFlows().ClientCredentials != nil {
					oauthTypes = append(oauthTypes, "client_credentials")
					envvars = append(envvars, strcase.ToSNAKE(slug+"_CLIENT_SECRET"))
					envvars = append(envvars, strcase.ToSNAKE(slug+"_CLIENT_ID"))
					envvars = append(envvars, strcase.ToSNAKE(slug+"_TOKEN_URL"))
					envvars = append(envvars, strcase.ToSNAKE(slug+"_ACCESS_TOKEN"))
				}

				if sec.GetFlows().Implicit != nil {
					oauthTypes = append(oauthTypes, "implicit")
				}

				if sec.GetFlows().AuthorizationCode != nil {
					oauthTypes = append(oauthTypes, "authorization_code")
				}

				var flowBuf bytes.Buffer
				ctx = yml.ContextWithConfig(ctx, &yml.Config{
					OutputFormat: yml.OutputFormatJSON,
				})
				if err := marshaller.Marshal(ctx, sec.GetFlows(), &flowBuf); err != nil {
					errs = append(errs, fmt.Errorf("%s (%d:%d) error serializing oauth2 flows: %w", key, line, col, err))
					continue
				} else {
					oauthFlows = flowBuf.Bytes()
				}
			}
			if len(oauthTypes) == 0 {
				errs = append(errs, fmt.Errorf("%s (%d:%d) unsupported oauth2 security scheme: no supported flows found", key, line, col))
				continue
			}
		default:
			errs = append(errs, fmt.Errorf("%s (%d:%d) unsupported security scheme type: %s", key, line, col, sec.GetType()))
			continue
		}

		res[key] = &repo.CreateHTTPSecurityParams{
			Key:                 key,
			DeploymentID:        task.DeploymentID,
			ProjectID:           uuid.NullUUID{UUID: task.ProjectID, Valid: task.ProjectID != uuid.Nil},
			Openapiv3DocumentID: uuid.NullUUID{UUID: task.DocumentID, Valid: task.DocumentID != uuid.Nil},
			Type:                conv.ToPGText(sec.GetType().String()),
			Name:                conv.ToPGTextEmpty(sec.GetName()),
			InPlacement:         conv.ToPGTextEmpty(sec.GetIn().String()),
			Scheme:              conv.ToPGTextEmpty(sec.GetScheme()),
			// No real reason to store this since it's purely for documentation
			// purposes and we should eventually drop the DB column. Setting it
			// to NULL.
			BearerFormat: pgtype.Text{String: "", Valid: false},
			EnvVariables: envvars,
			OauthTypes:   oauthTypes,
			OauthFlows:   oauthFlows,
		}
	}

	return res, errs
}

func extractDefaultServerSpeakeasy(ctx context.Context, logger *slog.Logger, docInfo *types.OpenAPIv3DeploymentAsset, servers []*openapi.Server) *string {
	for _, server := range servers {
		line, col := server.GetRootNodeLine(), server.GetRootNodeColumn()

		if server.GetVariables().Len() == 0 {
			u, err := url.Parse(server.URL)
			if err != nil {
				_ = oops.E(oops.CodeUnauthorized, err, "%s: %s: skipping server due to malformed url [%d:%d]: %s", docInfo.Name, server.URL, line, col, err.Error()).Log(ctx, logger)
				continue
			}

			if u.Scheme != "https" {
				_ = oops.E(oops.CodeUnauthorized, err, "%s: %s: skipping non-https server url [%d:%d]", docInfo.Name, server.URL, line, col).Log(ctx, logger)
				continue
			}

			return &server.URL
		}
	}

	return nil
}

func extractToolDefSpeakeasy(ctx context.Context, logger *slog.Logger, tx *repo.Queries, doc *openapi.OpenAPI, task operationTask[openapi.Operation, openapi.ReferencedParameter]) (repo.CreateOpenAPIv3ToolDefinitionParams, error) {
	empty := repo.CreateOpenAPIv3ToolDefinitionParams{} //nolint:exhaustruct //empty struct

	projectID := task.extractTask.ProjectID
	deploymentID := task.extractTask.DeploymentID
	openapiDocID := task.extractTask.DocumentID
	docInfo := task.extractTask.DocInfo
	method := task.method
	path := task.path
	opID := task.opID
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
		"op id set", opID != "",
		"method set", method != "",
		"path set", path != "",
		"operation set", op != nil,
		"server env var set", serverEnvVar != "",
	); err != nil {
		return empty, tagError("invariants-violated", "not enough information to create tool definition: %w", err)
	}

	switch {
	case len(op.Servers) > 0:
		return empty, tagError("op-servers", "per-operation servers are not currently supported [line: %d]", op.GetCore().Servers.GetKeyNodeOrRootLine(op.GetRootNode()))
	case op.Deprecated != nil && *op.Deprecated:
		return empty, tagError("deprecated-op", "operation is deprecated [line: %d]", op.GetCore().Deprecated.GetKeyNodeOrRootLine(op.GetRootNode()))
	}

	defs := sequencedmap.New[string, *oas3.JSONSchema[oas3.Referenceable]]()

	var bodyResult capturedRequestBodySpeakeasy

	if op.RequestBody != nil {
		_, err := op.GetRequestBody().Resolve(ctx, openapi.ResolveOptions{
			TargetLocation:      "/",
			RootDocument:        doc,
			DisableExternalRefs: true,
			SkipValidation:      true,
		})
		if err != nil {
			return empty, fmt.Errorf("error resolving request body: %w", err)
		}
		requestBody := op.GetRequestBody().GetObject()
		if requestBody.GetContent().Len() > 1 {
			if err := tx.LogDeploymentEvent(ctx, repo.LogDeploymentEventParams{
				DeploymentID:   deploymentID,
				ProjectID:      projectID,
				Event:          "deployment:warning",
				Message:        fmt.Sprintf("%s: %s: only one request body content type processed for operation", docInfo.Name, opID),
				AttachmentID:   uuid.NullUUID{UUID: openapiDocID, Valid: openapiDocID != uuid.Nil},
				AttachmentType: conv.ToPGText("openapi"),
			}); err != nil {
				logger.ErrorContext(ctx, "failed to log deployment event", attr.SlogError(err))
			}
		}

		bodyResult, err = captureRequestBodySpeakeasy(ctx, doc, requestBody)
		if err != nil {
			return empty, fmt.Errorf("error parsing request body: %w", err)
		}
	}

	headerParams := orderedmap.New[string, *openapi.Parameter]()
	queryParams := orderedmap.New[string, *openapi.Parameter]()
	pathParams := orderedmap.New[string, *openapi.Parameter]()

	for _, p := range append(sharedParameters, op.Parameters...) {
		_, err := p.Resolve(ctx, openapi.ResolveOptions{
			TargetLocation:      "/",
			RootDocument:        doc,
			DisableExternalRefs: true,
			SkipValidation:      true,
		})
		if err != nil {
			return empty, fmt.Errorf("error resolving parameter: %w", err)
		}
		param := p.GetObject()

		switch param.GetIn() {
		case openapi.ParameterInHeader:
			headerParams.Set(param.Name, param)
		case openapi.ParameterInPath:
			pathParams.Set(param.Name, param)
		case openapi.ParameterInQuery:
			queryParams.Set(param.Name, param)
		default:
			continue
		}
	}

	schema := &oas3.Schema{
		Type:                 oas3.NewTypeFromString("object"),
		Properties:           sequencedmap.New[string, *oas3.JSONSchema[oas3.Referenceable]](),
		AdditionalProperties: oas3.NewJSONSchemaFromBool(false),
	}

	headerSchema, headerSettings, d, err := captureParametersSpeakeasy(ctx, logger, doc, slices.Collect(headerParams.Values()))
	if err != nil {
		return empty, fmt.Errorf("error collecting header parameters: %w", err)
	}
	mergeDefs(ctx, logger, defs, d)

	querySchema, querySettings, d, err := captureParametersSpeakeasy(ctx, logger, doc, slices.Collect(queryParams.Values()))
	if err != nil {
		return empty, fmt.Errorf("error collecting query parameters: %w", err)
	}
	mergeDefs(ctx, logger, defs, d)

	pathSchema, pathSettings, d, err := captureParametersSpeakeasy(ctx, logger, doc, slices.Collect(pathParams.Values()))
	if err != nil {
		return empty, fmt.Errorf("error collecting path parameters: %w", err)
	}
	mergeDefs(ctx, logger, defs, d)

	addParametersToSchema(schema, sequencedmap.New(sequencedmap.NewElem("pathParameters", pathSchema), sequencedmap.NewElem("headerParameters", headerSchema), sequencedmap.NewElem("queryParameters", querySchema)))

	var requestContentType *string
	if bodyResult.valid {
		schema.Properties.Set("body", bodyResult.schema)
		if bodyResult.required {
			schema.Required = append(schema.Required, "body")
		}
		requestContentType = &bodyResult.contentType
		mergeDefs(ctx, logger, defs, bodyResult.defs)
	}

	descriptor := parseToolDescriptor(ctx, logger, docInfo, opID, operation{
		summary:               op.GetSummary(),
		description:           op.GetDescription(),
		gramExtension:         op.GetExtensions().GetOrZero("x-gram"),
		speakeasyMCPExtension: op.GetExtensions().GetOrZero("x-speakeasy-mcp"),
	})

	responseFilter, responseFilterSchema, err := getResponseFilterSpeakeasy(ctx, logger, doc, op, descriptor.responseFilterType)
	if err != nil {
		return empty, fmt.Errorf("error getting response filter: %w", err)
	}
	if responseFilterSchema != nil {
		schema.Properties.Set("responseFilter", responseFilterSchema)
	}

	var schemaBytes bytes.Buffer
	if schema.Properties.Len() > 0 {
		ctx = yml.ContextWithConfig(ctx, &yml.Config{
			OutputFormat: yml.OutputFormatJSON,
		})
		err = marshaller.Marshal(ctx, oas3.NewJSONSchemaFromSchema[oas3.Referenceable](schema), &schemaBytes)
		if err != nil {
			return empty, fmt.Errorf("error serializing operation schema: %w", err)
		}
	}

	security, err := serializeSecuritySpeakeasy(op.GetSecurity())
	if err != nil {
		loc := "-"
		node := op.GetCore().Security.GetKeyNodeOrRoot(op.GetRootNode())
		if node != nil {
			loc = fmt.Sprintf("%d:%d", node.Line, node.Column)
		}

		return empty, fmt.Errorf("error serializing operation security [%s]: %w", loc, err)
	}

	if len(security) == 0 {
		security = globalSecurity
	}

	if len(op.Servers) > 0 {
		serverEnvVar = strcase.ToSNAKE(fmt.Sprintf("%s_%s_SERVER_URL", docInfo.Slug, opID))
		defaultServer = extractDefaultServerSpeakeasy(ctx, logger, docInfo, op.GetServers())
	}

	var confirm *string
	if descriptor.confirm != nil {
		confirm = conv.Ptr(string(*descriptor.confirm))
	}

	tags := op.Tags
	if tags == nil {
		tags = []string{}
	}

	return repo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           projectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{UUID: openapiDocID, Valid: openapiDocID != uuid.Nil},
		Security:            security,
		Path:                path,
		HttpMethod:          strings.ToUpper(method),
		Openapiv3Operation:  conv.ToPGText(truncateWithHash(opID, 255)),
		Name:                descriptor.name,
		UntruncatedName:     conv.ToPGTextEmpty(descriptor.untruncatedName),
		Tags:                tags,
		Summary:             descriptor.summary,
		Description:         descriptor.description,
		Confirm:             conv.PtrToPGTextEmpty(confirm),
		ConfirmPrompt:       conv.PtrToPGTextEmpty(descriptor.confirmPrompt),
		OriginalName:        conv.PtrToPGTextEmpty(descriptor.originalName),
		OriginalSummary:     conv.PtrToPGTextEmpty(descriptor.originalSummary),
		OriginalDescription: conv.PtrToPGTextEmpty(descriptor.originalDescription),
		XGram:               pgtype.Bool{Bool: descriptor.xGramFound, Valid: true},
		SchemaVersion:       "1.0.0",
		Schema:              schemaBytes.Bytes(),
		ServerEnvVar:        serverEnvVar,
		DefaultServerUrl:    conv.PtrToPGText(defaultServer),
		HeaderSettings:      headerSettings,
		QuerySettings:       querySettings,
		PathSettings:        pathSettings,
		RequestContentType:  conv.PtrToPGText(requestContentType),
		ResponseFilter:      responseFilter,
	}, nil
}

type Defs = *sequencedmap.Map[string, *oas3.JSONSchema[oas3.Referenceable]]

type capturedRequestBodySpeakeasy struct {
	valid       bool
	schema      *oas3.JSONSchema[oas3.Referenceable]
	required    bool
	contentType string
	defs        Defs
}

func captureRequestBodySpeakeasy(ctx context.Context, doc *openapi.OpenAPI, requestBody *openapi.RequestBody) (capturedRequestBodySpeakeasy, error) {
	empty := capturedRequestBodySpeakeasy{} //nolint:exhaustruct // empty struct

	if requestBody == nil || requestBody.GetContent().Len() == 0 {
		return empty, nil
	}

	required := requestBody.GetRequired()

	contentType := ""
	var spec *openapi.MediaType

	for mt, mtspec := range requestBody.GetContent().All() {
		if slices.ContainsFunc(preferredRequestTypes, func(t *regexp.Regexp) bool {
			return t.MatchString(mt)
		}) {
			contentType = mt
			spec = mtspec
			break
		}
	}

	if contentType == "" {
		types := slices.Collect(requestBody.GetContent().Keys())
		return empty, tagError("unsupported-request", "no supported request body content type found: %s", strings.Join(types, ", "))
	}

	if spec == nil {
		return capturedRequestBodySpeakeasy{
			valid:       true,
			schema:      oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Type: oas3.NewTypeFromString("object"), AdditionalProperties: oas3.NewJSONSchemaFromBool(true)}),
			required:    required,
			contentType: contentType,
			defs:        nil,
		}, nil
	}

	schema, defs, err := extractJSONSchemaSpeakeasy(ctx, doc, "requestBody", spec.Schema)
	if err != nil {
		return empty, fmt.Errorf("failed to extract json schema: %w", err)
	}

	return capturedRequestBodySpeakeasy{
		valid:       true,
		schema:      schema,
		required:    required,
		contentType: contentType,
		defs:        defs,
	}, nil
}

func extractJSONSchemaSpeakeasy(ctx context.Context, doc *openapi.OpenAPI, name string, js *oas3.JSONSchema[oas3.Referenceable]) (*oas3.JSONSchema[oas3.Referenceable], Defs, error) {
	line, col := js.GetRootNodeLine(), js.GetRootNodeColumn()

	inlined, err := oas3.Inline(ctx, js, oas3.InlineOptions{
		ResolveOptions: oas3.ResolveOptions{
			TargetLocation:      "/",
			RootDocument:        doc,
			DisableExternalRefs: true,
		},
		RemoveUnusedDefs: true,
	})
	if err != nil {
		return nil, nil, tagError("inline-error", "%s (%d:%d): error inlining schema: %w", name, line, col, err)
	}

	// Extract definitions from inlined schema as we need to bubble them up to the top-level schema
	var defs *sequencedmap.Map[string, *oas3.JSONSchema[oas3.Referenceable]]
	if inlined.IsLeft() {
		defs = inlined.GetLeft().Defs
		inlined.GetLeft().Defs = nil
	}

	return inlined, defs, nil
}

func captureParametersSpeakeasy(ctx context.Context, logger *slog.Logger, doc *openapi.OpenAPI, params []*openapi.Parameter) (*oas3.JSONSchema[oas3.Referenceable], []byte, Defs, error) {
	if len(params) == 0 {
		return nil, nil, nil, nil
	}

	obj := createEmptyObjectSchema()

	specs := make(map[string]*OpenapiV3ParameterProxy, len(params))

	defs := sequencedmap.New[string, *oas3.JSONSchema[oas3.Referenceable]]()

	for _, param := range params {
		var paramSchema *oas3.JSONSchema[oas3.Referenceable]

		if param.Schema == nil {
			paramSchema = oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{
				Type: oas3.NewTypeFromString("string"),
			})
		} else {
			_, err := param.Schema.Resolve(ctx, oas3.ResolveOptions{
				TargetLocation:      "/",
				RootDocument:        doc,
				DisableExternalRefs: true,
				SkipValidation:      true,
			})
			if err != nil {
				return nil, nil, nil, fmt.Errorf("error resolving parameter schema: %w", err)
			}
			s := param.Schema.GetResolvedSchema()

			if s.IsLeft() && s.GetLeft().GetDescription() == "" && param.GetDescription() != "" {
				s.GetLeft().Description = pointer.From(param.GetDescription())
			}

			es, d, err := extractJSONSchemaSpeakeasy(ctx, doc, param.Name, param.Schema)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to extract json schema: %w", err)
			}

			paramSchema = es
			mergeDefs(ctx, logger, defs, d)
		}

		required := param.Required
		if param.GetIn() == openapi.ParameterInPath {
			required = pointer.From(true)
		}

		proxy := &OpenapiV3ParameterProxy{
			// We don't need the schema when plucking out the serialzating settings
			// for a parameter. It would only bloat the database so we're stripping
			// it out before storing.
			Schema:          nil,
			In:              param.GetIn().String(),
			Name:            param.GetName(),
			Description:     param.GetDescription(),
			Required:        required,
			Deprecated:      param.GetDeprecated(),
			AllowEmptyValue: param.GetAllowEmptyValue(),
			Style:           pointer.Value(param.Style).String(),
			Explode:         param.Explode,
		}

		obj.Properties.Set(param.Name, paramSchema)
		if pointer.Value(required) {
			obj.Required = append(obj.Required, param.Name)
		}

		specs[param.Name] = proxy
	}

	spec, err := json.Marshal(specs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error marshalling parameter specifications: %w", err)
	}

	return oas3.NewJSONSchemaFromSchema[oas3.Referenceable](obj), spec, defs, nil
}

func addParametersToSchema(schema *oas3.Schema, params *sequencedmap.Map[string, *oas3.JSONSchema[oas3.Referenceable]]) {
	for key, val := range params.All() {
		if val == nil {
			continue
		}
		schema.Properties.Set(key, val)
		if val.GetResolvedSchema().IsLeft() && len(val.GetResolvedSchema().GetLeft().GetRequired()) > 0 {
			schema.Required = append(schema.Required, key)
		}
	}
}

func mergeDefs(ctx context.Context, logger *slog.Logger, a, b Defs) Defs {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}

	for key, val := range b.All() {
		if a.Has(key) {
			aHash := hashing.Hash(a.GetOrZero(key))
			bHash := hashing.Hash(val)
			if aHash != bHash {
				_ = oops.E(oops.CodeUnexpected, oops.Perm(fmt.Errorf("hash mismatch for defs schema %s", key)), "hash mismatch for defs schema %s", key).Log(ctx, logger)
				continue
			}
		} else {
			a.Set(key, val)
		}
	}

	return a
}

func createEmptyObjectSchema() *oas3.Schema {
	return &oas3.Schema{
		Type:                 oas3.NewTypeFromString("object"),
		Properties:           sequencedmap.New[string, *oas3.JSONSchema[oas3.Referenceable]](),
		AdditionalProperties: oas3.NewJSONSchemaFromBool(false),
	}
}
