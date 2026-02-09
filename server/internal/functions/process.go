package functions

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	slogmulti "github.com/samber/slog-multi"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/deployments/events"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var errPermanent = oops.Permanent(errors.New("permanent error"))
var attrError = attr.SlogEvent("functions:error")

var nodeEntrypoints = map[string]struct{}{
	"functions.js":  {},
	"functions.mjs": {},
	"functions.cjs": {},
	"functions.mts": {},
	"functions.cts": {},
	"functions.ts":  {},
}
var pythonEntrypoints = map[string]struct{}{
	"functions.py": {},
}

type ProcessError struct {
	reason string
	err    error
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

type ToolExtractorTask struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	AttachmentID uuid.UUID
	Attachment   *types.DeploymentFunctions
	AssetURL     *url.URL
	ProjectSlug  string
	OrgSlug      string

	OnToolSkipped func(err error)
}

type ToolExtractorResult struct {
	ManifestVersion string
	NumTools        int
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
	assetURL := task.AssetURL
	projectID := task.ProjectID
	deploymentID := task.DeploymentID
	attachementID := task.AttachmentID
	attachement := task.Attachment
	if err := inv.Check("functions tool extractor task",
		"asset url set", assetURL != nil,
		"project id set", projectID != uuid.Nil,
		"deployment id set", deploymentID != uuid.Nil,
		"functions attachment id set", attachementID != uuid.Nil,
		"functions attachement info set", attachement != nil && attachement.Name != "" && attachement.Slug != "",
	); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, oops.Permanent(err), "unable to verify functions attachement").Log(ctx, p.logger)
	}

	slug := attachement.Slug

	slogArgs := []any{
		attr.SlogProjectID(projectID.String()),
		attr.SlogDeploymentFunctionsName(attachement.Name),
		attr.SlogDeploymentFunctionsSlug(attachement.Slug),
		attr.SlogDeploymentID(deploymentID.String()),
		attr.SlogDeploymentFunctionsID(attachementID.String()),
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
				attr.SlogDeploymentFunctionsID(attachementID.String()),
			)
		}
	}()

	if !IsSupportedRuntime(attachement.Runtime) {
		return nil, oops.E(
			oops.CodeBadRequest,
			nil,
			"%s: unsupported functions runtime: %s (allowed: %s)", slug, attachement.Runtime, supportedRuntimes,
		).Log(ctx, logger, attrError)
	}

	var validEntrypoint map[string]struct{}
	switch {
	case strings.HasPrefix(attachement.Runtime, "nodejs:"):
		validEntrypoint = nodeEntrypoints
	case strings.HasPrefix(attachement.Runtime, "python:"):
		validEntrypoint = pythonEntrypoints
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "%s: unrecognized functions runtime", slug).Log(ctx, logger)
	}
	entrypointKeys := slices.Sorted(maps.Keys(validEntrypoint))

	rc, size, err := p.assetStorage.ReadAt(ctx, assetURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "%s: error fetching functions zip file", slug).Log(ctx, logger)
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		return rc.Close()
	})

	rdr, err := zip.NewReader(rc, size)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s: error opening functions zip file", slug).Log(ctx, logger)
	}

	foundEntrypoint := false
	var manifestFile *zip.File
	for _, f := range rdr.File {
		if _, ok := validEntrypoint[f.Name]; ok {
			foundEntrypoint = true
			continue
		}
		if f.Name == "manifest.json" {
			manifestFile = f
		}
	}

	if !foundEntrypoint {
		return nil, oops.E(oops.CodeBadRequest, errPermanent, "%s: functions zip file is missing entrypoint file: %v", entrypointKeys, slug).Log(ctx, logger, attrError)
	}

	if manifestFile == nil {
		return nil, oops.E(oops.CodeBadRequest, errPermanent, "%s: functions zip file is missing manifest.json file", slug).Log(ctx, logger, attrError)
	}

	manifestRdr, err := manifestFile.Open()
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s: error opening manifest file", slug).Log(ctx, logger, attrError)
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		return manifestRdr.Close()
	})

	manifestBs, err := io.ReadAll(manifestRdr)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s: error reading manifest file", slug).Log(ctx, logger, attrError)
	}

	var manifest Manifest
	if err := manifest.UnmarshalJSON(manifestBs); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s: error parsing manifest file", slug).Log(ctx, logger, attrError)
	}

	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "%s: error opening database transaction", slug).Log(ctx, p.logger, attrError)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := repo.New(dbtx)

	numTools := 0
	for idx, tool := range manifest.V0.Tools {
		_, err := processManifestToolV0(
			ctx,
			tx,
			tool,
			projectID,
			deploymentID,
			attachementID,
			slug,
			attachement.Runtime,
		)
		if err != nil {
			msg := fmt.Sprintf("%s: skipping tool %d (%q): %v", slug, idx, tool.Name, err)
			logger.ErrorContext(ctx, msg, attrError)
			if task.OnToolSkipped != nil {
				task.OnToolSkipped(err)
			}
			continue
		}

		numTools += 1
	}

	numResources := 0
	for idx, resource := range manifest.V0.Resources {
		_, err := processManifestResourceV0(
			ctx,
			tx,
			resource,
			projectID,
			deploymentID,
			attachementID,
			slug,
			attachement.Runtime,
		)
		if err != nil {
			msg := fmt.Sprintf("%s: skipping resource %d (%q): %v", slug, idx, resource.Name, err)
			logger.ErrorContext(ctx, msg, attrError)
			if task.OnToolSkipped != nil {
				task.OnToolSkipped(err)
			}
			continue
		}

		numResources += 1
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, oops.Permanent(err), "%s: error saving tools and resources", slug).Log(ctx, logger)
	}

	return &ToolExtractorResult{
		ManifestVersion: manifest.Version,
		NumTools:        numTools,
	}, nil
}

func processManifestToolV0(
	ctx context.Context,
	tx *repo.Queries,
	tool ManifestToolV0,
	projectID uuid.UUID,
	deploymentID uuid.UUID,
	attachementID uuid.UUID,
	attachementSlug types.Slug,
	runtime string,
) (*repo.FunctionToolDefinition, error) {
	if err := validateManifestToolV0(tool); err != nil {
		return nil, tagError("invalid-manifest", "validate tool: %w", err)
	}

	name := tool.Name
	description := tool.Description
	inputSchema := tool.InputSchema
	variables := tool.Variables
	metaTags := tool.Meta

	if variables == nil {
		variables = map[string]*ManifestVariableAttributeV0{}
	}

	varBs, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("serialize variables to json: %w", err)
	}

	var metaBs []byte
	if metaTags != nil {
		metaBs, err = json.Marshal(metaTags)
		if err != nil {
			return nil, fmt.Errorf("serialize meta to json: %w", err)
		}
	}

	var authInput []byte
	if tool.AuthInput != nil {
		authInput, err = json.Marshal(tool.AuthInput)
		if err != nil {
			return nil, fmt.Errorf("serialize auth input to json: %w", err)
		}
	}

	t, err := tx.CreateFunctionsTool(ctx, repo.CreateFunctionsToolParams{
		DeploymentID: deploymentID,
		FunctionID:   attachementID,
		ToolUrn:      urn.NewTool(urn.ToolKindFunction, string(attachementSlug), name),
		ProjectID:    projectID,
		Runtime:      runtime,
		Name:         name,
		Description:  description,
		InputSchema:  inputSchema,
		Variables:    varBs,
		AuthInput:    authInput,
		Meta:            metaBs,
		ReadOnlyHint:    false,
		DestructiveHint: true,
		IdempotentHint:  false,
		OpenWorldHint:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("save tool: %w", err)
	}

	return &t, nil
}

func processManifestResourceV0(
	ctx context.Context,
	tx *repo.Queries,
	resource ManifestResourceV0,
	projectID uuid.UUID,
	deploymentID uuid.UUID,
	attachementID uuid.UUID,
	attachementSlug types.Slug,
	runtime string,
) (*repo.FunctionResourceDefinition, error) {
	if err := validateManifestResourceV0(resource); err != nil {
		return nil, tagError("invalid-manifest", "validate resource: %w", err)
	}

	name := resource.Name
	description := resource.Description
	uri := resource.URI
	variables := resource.Variables
	metaTags := resource.Meta

	if variables == nil {
		variables = map[string]*ManifestVariableAttributeV0{}
	}

	varBs, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("serialize variables to json: %w", err)
	}

	var metaBs []byte
	if metaTags != nil {
		metaBs, err = json.Marshal(metaTags)
		if err != nil {
			return nil, fmt.Errorf("serialize meta to json: %w", err)
		}
	}

	var title, mimeType pgtype.Text
	if resource.Title != nil {
		title.String = *resource.Title
		title.Valid = true
	}

	if resource.MimeType != nil {
		mimeType.String = *resource.MimeType
		mimeType.Valid = true
	}

	params := repo.CreateFunctionsResourceParams{
		DeploymentID: deploymentID,
		FunctionID:   attachementID,
		ResourceUrn:  urn.NewResource(urn.ResourceKindFunction, string(attachementSlug), uri),
		ProjectID:    projectID,
		Runtime:      runtime,
		Name:         name,
		Description:  description,
		Uri:          uri,
		Title:        title,
		MimeType:     mimeType,
		Variables:    varBs,
		Meta:         metaBs,
	}

	r, err := tx.CreateFunctionsResource(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("save resource: %w", err)
	}

	return &r, nil
}
