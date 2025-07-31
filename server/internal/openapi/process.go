package openapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	libopenapiJSON "github.com/pb33f/libopenapi/json"
	slogmulti "github.com/samber/slog-multi"
	"gopkg.in/yaml.v3"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orderedmap"
	"github.com/speakeasy-api/gram/server/internal/tools"
	"github.com/speakeasy-api/gram/server/internal/tools/repo/models"
)

type ProcessError struct {
	reason string
	err    error
}

// truncateWithHash truncates a string to the specified length and appends a hash if needed
func truncateWithHash(s string, maxLength int) string {
	if len(s) <= maxLength {
		return s
	}

	hash := sha256.Sum256([]byte(s))
	hashStr := hex.EncodeToString(hash[:])[:8] // Use first 8 characters of hex hash
	truncateLength := maxLength - len(hashStr)
	if truncateLength < 0 {
		// If maxLength is too small to fit even the hash, just return the hash
		return hashStr
	}

	return s[:truncateLength] + hashStr
}

func tagError(reason string, msg string, keyvals ...any) *ProcessError {
	return &ProcessError{
		reason: reason,
		err:    fmt.Errorf(msg, keyvals...),
	}
}

func (e *ProcessError) Reason() string {
	return e.reason
}

func (e *ProcessError) Error() string {
	return e.err.Error()
}

func (e *ProcessError) Unwrap() error {
	return e.err
}

var preferredRequestTypes = []*regexp.Regexp{
	regexp.MustCompile(`\bjson\b`),
	regexp.MustCompile(`^application/x-www-form-urlencoded\b`),
	regexp.MustCompile(`^multipart/form-data\b`),
	regexp.MustCompile(`^text/`),
}

type ToolExtractorTask struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	DocumentID   uuid.UUID
	DocInfo      *types.OpenAPIv3DeploymentAsset
	DocURL       *url.URL
	ProjectSlug  string
	OrgSlug      string

	OnOperationSkipped func(err error)
}

type ToolExtractorResult struct {
	DocumentVersion         string
	DocumentUpgrade         *o11y.Outcome
	DocumentUpgradeDuration time.Duration
}

type ToolExtractor struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	assetStorage assets.BlobStore
}

func NewToolExtractor(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore) *ToolExtractor {
	return &ToolExtractor{
		logger:       logger,
		db:           db,
		assetStorage: assetStorage,
	}
}

func (p *ToolExtractor) Do(
	ctx context.Context,
	task ToolExtractorTask,
) (*ToolExtractorResult, error) {
	docURL := task.DocURL
	projectID := task.ProjectID
	deploymentID := task.DeploymentID
	openapiDocID := task.DocumentID
	docInfo := task.DocInfo
	if err := inv.Check("processOpenAPIv3Document",
		"doc url set", docURL != nil,
		"project id set", projectID != uuid.Nil,
		"deployment id set", deploymentID != uuid.Nil,
		"openapi doc id set", openapiDocID != uuid.Nil,
		"doc info set", docInfo != nil && docInfo.Name != "" && docInfo.Slug != "",
	); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, oops.Perm(err), "unable to process openapi document").Log(ctx, p.logger)
	}

	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error opening database transaction").Log(ctx, p.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := repo.New(dbtx)

	slogArgs := []any{
		attr.SlogProjectID(projectID.String()),
		attr.SlogDeploymentOpenAPIName(docInfo.Name),
		attr.SlogDeploymentOpenAPISlug(string(docInfo.Slug)),
		attr.SlogDeploymentID(deploymentID.String()),
		attr.SlogDeploymentOpenAPIID(openapiDocID.String()),
		attr.SlogProjectSlug(task.ProjectSlug),
		attr.SlogOrganizationSlug(task.OrgSlug),
	}

	eventsHandler := NewLogHandler()
	logger := slog.New(slogmulti.Fanout(
		p.logger.Handler(),
		eventsHandler,
	)).With(slogArgs...)

	defer func() {
		if _, err := eventsHandler.Flush(ctx, p.db); err != nil {
			p.logger.ErrorContext(
				ctx,
				"failed to flush deployment events",
				attr.SlogError(err),
				attr.SlogProjectID(projectID.String()),
				attr.SlogDeploymentID(deploymentID.String()),
				attr.SlogDeploymentOpenAPIID(openapiDocID.String()),
			)
		}
	}()

	rc, err := p.assetStorage.Read(ctx, docURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching openapi document").Log(ctx, logger)
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		return rc.Close()
	})

	doc, err := io.ReadAll(rc)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading openapi document").Log(ctx, logger)
	}

	document, err := libopenapi.NewDocumentWithConfiguration(doc, &datamodel.DocumentConfiguration{
		AllowFileReferences:                 false,
		AllowRemoteReferences:               false,
		BundleInlineRefs:                    false,
		ExcludeExtensionRefs:                true,
		IgnorePolymorphicCircularReferences: true,
		IgnoreArrayCircularReferences:       true,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, oops.Perm(err), "error opening openapi document").Log(ctx, logger)
	}

	v3Model, errs := document.BuildV3Model()
	if len(errs) > 0 {
		for _, err := range errs {
			logger.ErrorContext(ctx, fmt.Sprintf("%s: %s", docInfo.Name, err.Error()), attr.SlogEvent("openapi:error"))
		}

		return nil, oops.E(
			oops.CodeBadRequest,
			oops.Perm(errors.Join(errs...)),
			"openapi v3 document '%s' had %d errors", docInfo.Name, len(errs),
		).Log(ctx, logger, attr.SlogEvent("openapi:error"))
	}

	upgradeStart := time.Now()
	upgradeResult, err := UpgradeOpenAPI30To31(document, v3Model)
	upgradeDuration := time.Since(upgradeStart)
	var upgradeOutcome *o11y.Outcome
	if err != nil {
		upgradeOutcome = conv.Ptr(o11y.OutcomeFailure)
		logger.ErrorContext(ctx, "Unable to upgrade OpenAPI v3.0 document to v3.1. Proceeding with v3.0 document.", attr.SlogEvent("openapi-upgrade:error"))
		logger.ErrorContext(ctx, err.Error(), attr.SlogEvent("openapi-upgrade:error"))
	} else {
		v3Model = upgradeResult.Model

		if upgradeResult.Upgraded {
			upgradeOutcome = conv.Ptr(o11y.OutcomeSuccess)
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

	globalSecurity, err := serializeSecurity(v3Model.Model.Security)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, oops.Perm(err), "error serializing global security").Log(ctx, logger)
	}

	securitySchemesParams, errs := extractSecuritySchemes(v3Model.Model, task)
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
	globalDefaultServer := p.extractDefaultServer(ctx, logger, docInfo, v3Model.Model.Servers)

	for path, pathItem := range v3Model.Model.Paths.PathItems.FromOldest() {
		ops := []operation{
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

			opID := op.operation.OperationId
			if opID == "" {
				opID = fmt.Sprintf("%s_%s", op.method, path)
			}

			def, err := p.extractToolDef(ctx, logger, tx, operationTask{
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

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, oops.Perm(err), "error saving processed deployment").Log(ctx, logger)
	}

	return &ToolExtractorResult{
		DocumentVersion:         document.GetVersion(),
		DocumentUpgrade:         upgradeOutcome,
		DocumentUpgradeDuration: upgradeDuration,
	}, nil
}

func (s *ToolExtractor) extractDefaultServer(ctx context.Context, logger *slog.Logger, docInfo *types.OpenAPIv3DeploymentAsset, servers []*v3.Server) *string {
	for _, server := range servers {
		low := server.GoLow()
		line, col := low.KeyNode.Line, low.KeyNode.Column

		if server.Variables == nil || server.Variables.Len() == 0 {
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

type operationTask struct {
	extractTask      ToolExtractorTask
	method           string
	path             string
	opID             string
	operation        *v3.Operation
	sharedParameters []*v3.Parameter
	globalSecurity   []byte
	serverEnvVar     string
	defaultServer    *string
}

type operation struct {
	method    string
	path      string
	operation *v3.Operation
}

func (s *ToolExtractor) extractToolDef(ctx context.Context, logger *slog.Logger, tx *repo.Queries, task operationTask) (repo.CreateOpenAPIv3ToolDefinitionParams, error) {
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
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, tagError("invariants-violated", "not enough information to create tool definition: %w", err)
	}

	switch {
	case len(op.Servers) > 0:
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, tagError("op-servers", "per-operation servers are not currently supported [line: %d]", op.GoLow().Servers.NodeLineNumber())
	case op.Deprecated != nil && *op.Deprecated:
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, tagError("deprecated-op", "operation is deprecated [line: %d]", op.GoLow().Deprecated.NodeLineNumber())
	}

	if op.RequestBody != nil && op.RequestBody.Content != nil && op.RequestBody.Content.Len() > 1 {
		if err := tx.LogDeploymentEvent(ctx, repo.LogDeploymentEventParams{
			DeploymentID: deploymentID,
			ProjectID:    projectID,
			Event:        "deployment:warning",
			Message:      fmt.Sprintf("%s: %s: only one request body content type processed for operation", docInfo.Name, opID),
		}); err != nil {
			logger.ErrorContext(ctx, "failed to log deployment event", attr.SlogError(err))
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

	descriptor := parseToolDescriptor(ctx, logger, docInfo, opID, op)

	responseFilter, responseFilterSchema, err := getResponseFilter(ctx, logger, op, descriptor.responseFilterType)
	if err != nil {
		return repo.CreateOpenAPIv3ToolDefinitionParams{}, fmt.Errorf("error getting response filter: %w", err)
	}
	if responseFilterSchema != nil {
		merged.Properties.Set("responseFilter", json.RawMessage(responseFilterSchema))
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
		serverEnvVar = strcase.ToSNAKE(fmt.Sprintf("%s_%s_SERVER_URL", docInfo.Slug, opID))
		defaultServer = s.extractDefaultServer(ctx, logger, docInfo, op.Servers)
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
		Schema:              schemaBytes,
		ServerEnvVar:        serverEnvVar,
		DefaultServerUrl:    conv.PtrToPGText(defaultServer),
		HeaderSettings:      headerSettings,
		QuerySettings:       querySettings,
		PathSettings:        pathSettings,
		RequestContentType:  conv.PtrToPGText(requestContentType),
		ResponseFilter:      responseFilter,
	}, nil
}

type gramExtension struct {
	Confirm            *string            `yaml:"confirm"`
	ConfirmPrompt      *string            `yaml:"confirmPrompt"`
	Name               *string            `yaml:"name"`
	Summary            *string            `yaml:"summary"`
	Description        *string            `yaml:"description"`
	ResponseFilterType *models.FilterType `yaml:"responseFilterType"`
}

type speakeasyExtension struct {
	Name        *string `yaml:"name"`
	Description *string `yaml:"description"`
}

type toolDescriptor struct {
	xGramFound          bool
	xSpeakeasyMCPFound  bool
	name                string
	untruncatedName     string
	summary             string
	description         string
	confirm             *mv.Confirm
	confirmPrompt       *string
	originalName        *string
	originalSummary     *string
	originalDescription *string
	responseFilterType  *models.FilterType
}

func parseToolDescriptor(ctx context.Context, logger *slog.Logger, docInfo *types.OpenAPIv3DeploymentAsset, opID string, op *v3.Operation) toolDescriptor {
	gramExtNode, _ := op.Extensions.Get("x-gram")
	speakeasyExtNode, _ := op.Extensions.Get("x-speakeasy-mcp")
	untruncatedName := strcase.ToSnake(tools.SanitizeName(fmt.Sprintf("%s_%s", docInfo.Slug, opID)))
	// we limit actual tool name to 60 character by default to stay in line with common MCP client restrictions
	name := truncateWithHash(untruncatedName, 60)

	description := op.Description
	summary := op.Summary

	toolDesc := toolDescriptor{
		xGramFound:          false,
		xSpeakeasyMCPFound:  false,
		confirm:             conv.Ptr(mv.ConfirmAlways),
		confirmPrompt:       nil,
		name:                name,
		untruncatedName:     untruncatedName,
		summary:             summary,
		description:         description,
		originalName:        nil,
		originalSummary:     nil,
		originalDescription: nil,
		responseFilterType:  nil,
	}

	var xgram, xspeakeasy bool

	var speakeasyExt speakeasyExtension
	if speakeasyExtNode != nil {
		if err := speakeasyExtNode.Decode(&speakeasyExt); err != nil {
			msg := fmt.Sprintf("error parsing x-speakeasy-mcp extension: [%d:%d]: %s", speakeasyExtNode.Line, speakeasyExtNode.Column, err.Error())
			logger.WarnContext(ctx, msg)
		} else {
			xspeakeasy = true
		}
	}

	var gramExt gramExtension
	if gramExtNode != nil {
		if err := gramExtNode.Decode(&gramExt); err != nil {
			msg := fmt.Sprintf("error parsing x-gram extension: [%d:%d]: %s", gramExtNode.Line, gramExtNode.Column, err.Error())
			logger.WarnContext(ctx, msg)
		} else {
			xgram = true
		}
	}

	var extLine, extColumn int
	var customName, customSummary, customDescription, customConfirm, customConfirmPrompt *string
	var responseFilterType *models.FilterType
	switch {
	case xgram:
		extLine, extColumn = gramExtNode.Line, gramExtNode.Column
		customName = gramExt.Name
		customSummary = gramExt.Summary
		customDescription = gramExt.Description
		customConfirm = gramExt.Confirm
		customConfirmPrompt = gramExt.ConfirmPrompt
		responseFilterType = gramExt.ResponseFilterType
	case xspeakeasy:
		extLine, extColumn = speakeasyExtNode.Line, speakeasyExtNode.Column
		customName = speakeasyExt.Name
		customSummary = nil
		customDescription = speakeasyExt.Description
		customConfirm = nil
		customConfirmPrompt = nil
		// These are the only extension properties we care about. If they
		// are not set then we should assume that the tool descriptor should
		// remain unchanged.
		if customName == nil && customDescription == nil {
			return toolDesc
		}
	default:
		return toolDesc
	}

	sanitizedName := strcase.ToSnake(tools.SanitizeName(conv.PtrValOr(customName, "")))

	confirm, valid := mv.SanitizeConfirmPtr(customConfirm)
	if !valid {
		msg := fmt.Sprintf("invalid tool confirmation mode: [%d:%d]: %v", extLine, extColumn, customConfirm)
		logger.WarnContext(ctx, msg)
		confirm = mv.ConfirmAlways
	}

	return toolDescriptor{
		xGramFound:          xgram,
		xSpeakeasyMCPFound:  xspeakeasy,
		name:                conv.Default(sanitizedName, name),
		untruncatedName:     untruncatedName,
		summary:             conv.PtrValOr(customSummary, summary),
		description:         conv.PtrValOr(customDescription, description),
		originalName:        conv.PtrEmpty(name),
		originalSummary:     conv.PtrEmpty(summary),
		originalDescription: conv.PtrEmpty(description),
		confirm:             conv.Ptr(confirm),
		confirmPrompt:       customConfirmPrompt,
		responseFilterType:  responseFilterType,
	}
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
		return empty, tagError("unsupported-request", "no supported request body content type found: %s", strings.Join(types, ", "))
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

	specs := make(map[string]*OpenapiV3ParameterProxy, len(params))

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

		proxy := &OpenapiV3ParameterProxy{
			// We don't need the schema when plucking out the serialzating settings
			// for a parameter. It would only bloat the database so we're stripping
			// it out before storing.
			Schema:          nil,
			In:              param.In,
			Name:            param.Name,
			Description:     param.Description,
			Required:        param.Required,
			Deprecated:      param.Deprecated,
			AllowEmptyValue: param.AllowEmptyValue,
			Style:           param.Style,
			Explode:         param.Explode,
		}

		obj.Properties.Set(param.Name, json.RawMessage(schemaBytes))
		if param.Required != nil && *param.Required {
			obj.Required = append(obj.Required, param.Name)
		}

		specs[param.Name] = proxy
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

	bs, err := json.Marshal(acc)
	if err != nil {
		return nil, fmt.Errorf("error serializing security requirements: %w", err)
	}

	return bs, nil
}

func extractSecuritySchemes(doc v3.Document, task ToolExtractorTask) (map[string]*repo.CreateHTTPSecurityParams, []error) {
	slug := string(task.DocInfo.Slug)

	if doc.Components == nil || doc.Components.SecuritySchemes == nil || doc.Components.SecuritySchemes.Len() == 0 {
		return nil, nil
	}

	var errs []error

	res := make(map[string]*repo.CreateHTTPSecurityParams)
	for key, sec := range doc.Components.SecuritySchemes.FromOldest() {
		low := sec.GoLow()
		line, col := low.KeyNode.Line, low.KeyNode.Column
		var envvars []string
		var oauthTypes []string
		var oauthFlows []byte

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
		case "oauth2":
			if sec.Flows != nil {
				if sec.Flows.AuthorizationCode != nil || sec.Flows.ClientCredentials != nil {
					envvars = append(envvars, strcase.ToSNAKE(slug+"_ACCESS_TOKEN"))
				}

				if sec.Flows.ClientCredentials != nil {
					oauthTypes = append(oauthTypes, "client_credentials")
					envvars = append(envvars, strcase.ToSNAKE(slug+"_CLIENT_SECRET"))
					envvars = append(envvars, strcase.ToSNAKE(slug+"_CLIENT_ID"))
					envvars = append(envvars, strcase.ToSNAKE(slug+"_TOKEN_URL"))
				}

				if sec.Flows.AuthorizationCode != nil {
					oauthTypes = append(oauthTypes, "authorization_code")
				}
				if flow, err := json.Marshal(sec.Flows); err != nil {
					errs = append(errs, fmt.Errorf("%s (%d:%d) error serializing oauth2 flows: %w", key, line, col, err))
					continue
				} else {
					oauthFlows = flow
				}
			}
			if len(oauthTypes) == 0 {
				errs = append(errs, fmt.Errorf("%s (%d:%d) unsupported oauth2 security scheme: no supported flows found", key, line, col))
				continue
			}
		default:
			errs = append(errs, fmt.Errorf("%s (%d:%d) unsupported security scheme type: %s", key, line, col, sec.Type))
			continue
		}

		res[key] = &repo.CreateHTTPSecurityParams{
			Key:          key,
			DeploymentID: task.DeploymentID,
			Type:         conv.ToPGText(sec.Type),
			Name:         conv.ToPGTextEmpty(sec.Name),
			InPlacement:  conv.ToPGTextEmpty(sec.In),
			Scheme:       conv.ToPGTextEmpty(sec.Scheme),
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

func extractJSONSchemaFromYaml(name string, schemaProxy *base.SchemaProxy) ([]byte, error) {
	keyNode := schemaProxy.GoLow().GetKeyNode()
	line, col := keyNode.Line, keyNode.Column
	schema, err := schemaProxy.MarshalYAMLInline()
	if err != nil {
		return nil, tagError("inline-error", "%s (%d:%d): error inlining schema: %w", name, line, col, err)
	}

	schemaNode, ok := schema.(*yaml.Node)
	if !ok {
		return nil, tagError("non-yaml-node", "%s (%d:%d): error inlining schema: expected *yaml.Node, got %T", name, line, col, schema)
	}

	schemaBytes, err := libopenapiJSON.YAMLNodeToJSON(schemaNode, "")
	if err != nil {
		return nil, tagError("yaml-to-json-error", "%s (%d:%d): error json marshalling schema: %w", name, line, col, err)
	}

	// Check if any $ref values are present in the schema
	// NB: this could result in false positives if any values in json schema contain `"$ref":`
	if strings.Contains(string(schemaBytes), `"$ref":`) {
		return nil, tagError("circular-ref", "%s (%d:%d): error inlining schema: circular reference detected", name, line, col)
	}

	return schemaBytes, nil
}
