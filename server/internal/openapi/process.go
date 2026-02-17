package openapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ettle/strcase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	slogmulti "github.com/samber/slog-multi"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/events"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	Parser string

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
	tracer       trace.Tracer
	db           *pgxpool.Pool
	feature      feature.Provider
	assetStorage assets.BlobStore
}

func NewToolExtractor(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, fp feature.Provider, assetStorage assets.BlobStore) *ToolExtractor {
	return &ToolExtractor{
		logger:       logger,
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/openapi"),
		db:           db,
		feature:      fp,
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
		return nil, oops.E(oops.CodeInvariantViolation, oops.Permanent(err), "unable to process openapi document").Log(ctx, p.logger)
	}

	doc, readErr := readDoc(ctx, readDocParams{
		docURL:  docURL,
		logger:  p.logger,
		storage: p.assetStorage,
	})

	if readErr != nil {
		return nil, readErr
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
		attr.SlogDeploymentOpenAPIParser(task.Parser),
		attr.SlogProjectID(projectID.String()),
		attr.SlogDeploymentOpenAPIName(docInfo.Name),
		attr.SlogDeploymentOpenAPISlug(string(docInfo.Slug)),
		attr.SlogDeploymentID(deploymentID.String()),
		attr.SlogDeploymentOpenAPIID(openapiDocID.String()),
		attr.SlogProjectSlug(task.ProjectSlug),
		attr.SlogOrganizationSlug(task.OrgSlug),
	}

	eventsHandler := events.NewLogHandler()
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

	// If we are re-processing a deployment, we need to clear out any existing
	// tools and security associated with the deployment + document, so we don't
	// end up with duplicates in the database.
	deletedTools, err := tx.DangerouslyClearDeploymentTools(ctx, repo.DangerouslyClearDeploymentToolsParams{
		DeploymentID:        deploymentID,
		ProjectID:           projectID,
		Openapiv3DocumentID: openapiDocID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error clearing deployment http tools").Log(ctx, p.logger)
	}
	if deletedTools > 0 {
		logger.InfoContext(ctx, "cleared http tools from previous deployment attempt", attr.SlogDBDeletedRowsCount(deletedTools))
	}

	deletedSecurity, err := tx.DangerouslyClearDeploymentHTTPSecurity(ctx, repo.DangerouslyClearDeploymentHTTPSecurityParams{
		DeploymentID:        deploymentID,
		ProjectID:           uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},
		Openapiv3DocumentID: uuid.NullUUID{UUID: openapiDocID, Valid: openapiDocID != uuid.Nil},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error clearing deployment http tools").Log(ctx, p.logger)
	}
	if deletedSecurity > 0 {
		logger.InfoContext(ctx, "cleared http security from previous deployment attempt", attr.SlogDBDeletedRowsCount(deletedSecurity))
	}

	var res *ToolExtractorResult
	if task.Parser != "speakeasy" {
		logger.ErrorContext(ctx, "unrecognized parser specified: defaulting to speakeasy", attr.SlogDeploymentOpenAPIParser(task.Parser))
	}

	res, err = p.doSpeakeasy(ctx, logger, p.tracer, tx, doc, task)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, oops.Permanent(err), "error saving processed deployment").Log(ctx, logger)
	}

	return res, nil
}

type operationTask[O any, P any] struct {
	extractTask      ToolExtractorTask
	method           string
	path             string
	opID             string
	operation        *O
	sharedParameters []*P
	globalSecurity   []byte
	serverEnvVar     string
	defaultServer    *string
}

type operationMetadata[T any] struct {
	method    string
	path      string
	operation *T
}

type operation struct {
	summary               string
	description           string
	gramExtension         *yaml.Node
	speakeasyMCPExtension *yaml.Node
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

func parseToolDescriptor(ctx context.Context, logger *slog.Logger, docInfo *types.OpenAPIv3DeploymentAsset, opID string, op operation) toolDescriptor {
	// gramExtNode, _ := op.Extensions.Get("x-gram")
	// speakeasyExtNode, _ := op.Extensions.Get("x-speakeasy-mcp")
	// Convert doc slug hyphens to underscores for consistency with tool naming
	sanitizedSlug := strings.ReplaceAll(string(docInfo.Slug), "-", "_")
	snakeCasedOp := strcase.ToSnake(opID)
	untruncatedName := tools.SanitizeName(fmt.Sprintf("%s_%s", sanitizedSlug, snakeCasedOp))
	// we limit actual tool name to 60 character by default to stay in line with common MCP client restrictions
	name := truncateWithHash(untruncatedName, 60)

	description := op.description
	summary := op.summary

	// Soon we will stop storing summary. Still we want to make sure that we do a best-effort to set a description.
	if description == "" {
		description = summary
	}

	toolDesc := toolDescriptor{
		xGramFound:          false,
		xSpeakeasyMCPFound:  false,
		confirm:             new(mv.ConfirmAlways),
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
	if op.speakeasyMCPExtension != nil {
		if err := op.speakeasyMCPExtension.Decode(&speakeasyExt); err != nil {
			msg := fmt.Sprintf("error parsing x-speakeasy-mcp extension: [%d:%d]: %s", op.speakeasyMCPExtension.Line, op.speakeasyMCPExtension.Column, err.Error())
			logger.WarnContext(ctx, msg)
		} else {
			xspeakeasy = true
		}
	}

	var gramExt gramExtension
	if op.gramExtension != nil {
		if err := op.gramExtension.Decode(&gramExt); err != nil {
			msg := fmt.Sprintf("error parsing x-gram extension: [%d:%d]: %s", op.gramExtension.Line, op.gramExtension.Column, err.Error())
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
		extLine, extColumn = op.gramExtension.Line, op.gramExtension.Column
		customName = gramExt.Name
		customSummary = gramExt.Summary
		customDescription = gramExt.Description
		customConfirm = gramExt.Confirm
		customConfirmPrompt = gramExt.ConfirmPrompt
		responseFilterType = gramExt.ResponseFilterType
	case xspeakeasy:
		extLine, extColumn = op.speakeasyMCPExtension.Line, op.speakeasyMCPExtension.Column
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
	finalName := tools.SanitizeName(conv.Default(sanitizedName, name))

	confirm, valid := mv.SanitizeConfirmPtr(customConfirm)
	if !valid {
		msg := fmt.Sprintf("invalid tool confirmation mode: [%d:%d]: %v", extLine, extColumn, customConfirm)
		logger.WarnContext(ctx, msg)
		confirm = mv.ConfirmAlways
	}

	return toolDescriptor{
		xGramFound:          xgram,
		xSpeakeasyMCPFound:  xspeakeasy,
		name:                finalName,
		untruncatedName:     untruncatedName,
		summary:             conv.PtrValOr(customSummary, summary),
		description:         conv.PtrValOr(customDescription, description),
		originalName:        conv.PtrEmpty(name),
		originalSummary:     conv.PtrEmpty(summary),
		originalDescription: conv.PtrEmpty(description),
		confirm:             new(confirm),
		confirmPrompt:       customConfirmPrompt,
		responseFilterType:  responseFilterType,
	}
}

type readDocParams struct {
	docURL  *url.URL
	logger  *slog.Logger
	storage assets.BlobStore
}

func readDoc(ctx context.Context, params readDocParams) ([]byte, error) {
	rc, err := params.storage.Read(ctx, params.docURL)
	if err != nil {
		return nil, oops.E(
			oops.CodeUnexpected,
			err,
			"error fetching openapi document",
		).Log(ctx, params.logger)
	}

	defer o11y.LogDefer(ctx, params.logger, func() error {
		return rc.Close()
	})

	doc, err := io.ReadAll(rc)
	if err != nil {
		return nil, oops.E(
			oops.CodeUnexpected,
			err,
			"error reading openapi document",
		).Log(ctx, params.logger)
	}

	return doc, nil
}
