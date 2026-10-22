package deployments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetsRepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/openapi"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// openAPIv3AssetKind is the asset kind uploaded OpenAPI documents are stored
// under.
const openAPIv3AssetKind = "openapiv3"

// evaluationStatus describes how an OpenAPI source differs between two
// deployments.
type evaluationStatus string

const (
	evaluationStatusNew       evaluationStatus = "new"
	evaluationStatusChanged   evaluationStatus = "changed"
	evaluationStatusUnchanged evaluationStatus = "unchanged"
	evaluationStatusRemoved   evaluationStatus = "removed"
)

// codeSourceRemoved identifies the change reported when an OpenAPI source is
// dropped from a deployment.
const codeSourceRemoved = "source_removed"

func (s *Service) EvaluateOpenAPIv3(ctx context.Context, form *gen.EvaluateOpenAPIv3Payload) (*gen.OpenAPIv3Evaluation, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID := *authCtx.ProjectID

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	assetID, err := uuid.Parse(form.AssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid asset id").LogWarn(ctx, s.logger)
	}

	slug := strings.TrimSpace(form.Slug)
	if slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").LogWarn(ctx, s.logger)
	}

	var base *repo.DeploymentsOpenapiv3Asset
	activeID, err := s.repo.GetActiveDeploymentID(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error getting active deployment").LogError(ctx, s.logger)
	default:
		docs, err := s.repo.ListProjectDeploymentOpenAPIv3(ctx, repo.ListProjectDeploymentOpenAPIv3Params{DeploymentID: activeID, ProjectID: projectID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error listing deployed openapi documents").LogError(ctx, s.logger)
		}

		if i := slices.IndexFunc(docs, func(d repo.DeploymentsOpenapiv3Asset) bool { return d.Slug == slug }); i >= 0 {
			base = &docs[i]
		}
	}

	name := slug
	switch {
	case form.Name != nil && strings.TrimSpace(*form.Name) != "":
		name = strings.TrimSpace(*form.Name)
	case base != nil:
		name = base.Name
	}

	next := &repo.DeploymentsOpenapiv3Asset{
		ID:           uuid.Nil,
		DeploymentID: uuid.Nil,
		AssetID:      assetID,
		Name:         name,
		Slug:         slug,
	}

	return s.evaluateSource(ctx, projectID, base, next)
}

func (s *Service) GetDeploymentEvaluation(ctx context.Context, form *gen.GetDeploymentEvaluationPayload) (*gen.GetDeploymentEvaluationResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID := *authCtx.ProjectID

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	deploymentID, err := uuid.Parse(form.DeploymentID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid deployment id").LogWarn(ctx, s.logger)
	}

	if _, err := s.repo.GetDeployment(ctx, repo.GetDeploymentParams{ID: deploymentID, ProjectID: projectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error getting deployment").LogError(ctx, s.logger)
	}

	nextDocs, err := s.repo.ListProjectDeploymentOpenAPIv3(ctx, repo.ListProjectDeploymentOpenAPIv3Params{DeploymentID: deploymentID, ProjectID: projectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing deployment openapi documents").LogError(ctx, s.logger)
	}

	var baseDeploymentID *string
	var baseDocs []repo.DeploymentsOpenapiv3Asset
	prevID, err := s.repo.GetEvaluationBaseDeploymentID(ctx, repo.GetEvaluationBaseDeploymentIDParams{ProjectID: projectID, DeploymentID: deploymentID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error getting base deployment").LogError(ctx, s.logger)
	default:
		baseDeploymentID = new(prevID.String())

		baseDocs, err = s.repo.ListProjectDeploymentOpenAPIv3(ctx, repo.ListProjectDeploymentOpenAPIv3Params{DeploymentID: prevID, ProjectID: projectID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error listing base deployment openapi documents").LogError(ctx, s.logger)
		}
	}

	baseBySlug := make(map[string]*repo.DeploymentsOpenapiv3Asset, len(baseDocs))
	for i := range baseDocs {
		baseBySlug[baseDocs[i].Slug] = &baseDocs[i]
	}

	documents := make([]*gen.OpenAPIv3Evaluation, 0, len(nextDocs))
	seen := make(map[string]struct{}, len(nextDocs))
	for i := range nextDocs {
		next := &nextDocs[i]
		seen[next.Slug] = struct{}{}

		base := baseBySlug[next.Slug]
		if base != nil && base.AssetID == next.AssetID {
			continue
		}

		eval, err := s.evaluateSource(ctx, projectID, base, next)
		if err != nil {
			return nil, err
		}
		documents = append(documents, eval)
	}

	for i := range baseDocs {
		base := &baseDocs[i]
		if _, ok := seen[base.Slug]; ok {
			continue
		}

		eval, err := s.evaluateSource(ctx, projectID, base, nil)
		if err != nil {
			return nil, err
		}
		documents = append(documents, eval)
	}

	slices.SortFunc(documents, func(a, b *gen.OpenAPIv3Evaluation) int {
		return strings.Compare(a.Slug, b.Slug)
	})

	return &gen.GetDeploymentEvaluationResult{
		DeploymentID:     deploymentID.String(),
		BaseDeploymentID: baseDeploymentID,
		Documents:        documents,
	}, nil
}

// evaluateSource compares two versions of one OpenAPI source. A nil base is a
// new source and a nil next is a removed one.
func (s *Service) evaluateSource(ctx context.Context, projectID uuid.UUID, base, next *repo.DeploymentsOpenapiv3Asset) (*gen.OpenAPIv3Evaluation, error) {
	source := next
	if source == nil {
		source = base
	}

	docInfo := &types.OpenAPIv3DeploymentAsset{
		ID:      source.ID.String(),
		AssetID: source.AssetID.String(),
		Name:    source.Name,
		Slug:    types.Slug(source.Slug),
	}

	var baseBytes, nextBytes []byte
	var err error
	if base != nil {
		baseBytes, err = s.readOpenAPIv3Asset(ctx, projectID, base.AssetID)
		if err != nil {
			return nil, err
		}
	}
	if next != nil {
		nextBytes, err = s.readOpenAPIv3Asset(ctx, projectID, next.AssetID)
		if err != nil {
			return nil, err
		}
	}

	result := &gen.OpenAPIv3Evaluation{
		Slug:             source.Slug,
		Name:             source.Name,
		Status:           "",
		AssetID:          nil,
		BaseAssetID:      nil,
		Base:             nil,
		Next:             nil,
		Changes:          nil,
		BreakingCount:    0,
		WarningCount:     0,
		InfoCount:        0,
		AffectedToolsets: nil,
	}
	if base != nil {
		result.BaseAssetID = new(base.AssetID.String())
	}
	if next != nil {
		result.AssetID = new(next.AssetID.String())
	}

	var changes []openapi.EvaluationChange
	if next == nil {
		// Extract the deployed document as if it were new to summarize what
		// the removal takes away.
		eval, err := openapi.EvaluateDocumentChange(ctx, s.tracer, docInfo, nil, baseBytes)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error reading deployed openapi document %q", source.Slug).LogError(ctx, s.logger)
		}

		result.Status = string(evaluationStatusRemoved)
		result.Base = toSnapshotView(&eval.Next)
	} else {
		eval, err := openapi.EvaluateDocumentChange(ctx, s.tracer, docInfo, baseBytes, nextBytes)
		switch {
		case errors.Is(err, openapi.ErrDeployedDocumentUnreadable):
			return nil, oops.E(oops.CodeUnexpected, err, "error reading deployed openapi document %q", source.Slug).LogError(ctx, s.logger)
		case err != nil:
			return nil, oops.E(oops.CodeInvalid, err, "error evaluating openapi document %q: %s", source.Slug, err.Error()).LogWarn(ctx, s.logger)
		}

		changes = eval.Changes
		result.Next = toSnapshotView(&eval.Next)
		if eval.Base != nil {
			result.Base = toSnapshotView(eval.Base)
		}

		switch {
		case base == nil:
			result.Status = string(evaluationStatusNew)
		case len(changes) == 0:
			result.Status = string(evaluationStatusUnchanged)
		default:
			result.Status = string(evaluationStatusChanged)
		}
	}

	toolsets, err := s.repo.ListToolsetsUsingOpenAPIv3Source(ctx, repo.ListToolsetsUsingOpenAPIv3SourceParams{
		ToolUrnPrefix: urn.NewTool(urn.ToolKindHTTP, source.Slug, "").String(),
		ProjectID:     projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing toolsets using openapi source").LogError(ctx, s.logger)
	}

	if next == nil {
		var removedTools []string
		for _, ts := range toolsets {
			for _, u := range ts.SourceToolUrns {
				if tool, err := urn.ParseTool(u); err == nil {
					removedTools = append(removedTools, tool.Name)
				}
			}
		}
		slices.Sort(removedTools)
		removedTools = slices.Compact(removedTools)

		title := fmt.Sprintf("Source removed: %d tools are no longer available", result.Base.ToolCount)
		if result.Base.ToolCount == 1 {
			title = "Source removed: 1 tool is no longer available"
		}

		changes = []openapi.EvaluationChange{{
			Severity: openapi.EvaluationSeverityBreaking,
			Category: openapi.EvaluationCategoryTools,
			Code:     codeSourceRemoved,
			Title:    title,
			Details:  nil,
			Tools:    removedTools,
		}}
	}

	affectedTools := make(map[string]struct{})
	result.Changes = make([]*gen.OpenAPIv3EvaluationChange, 0, len(changes))
	for _, c := range changes {
		switch c.Severity {
		case openapi.EvaluationSeverityBreaking:
			result.BreakingCount++
		case openapi.EvaluationSeverityWarning:
			result.WarningCount++
		case openapi.EvaluationSeverityInfo:
			result.InfoCount++
		}

		if c.Severity != openapi.EvaluationSeverityInfo {
			for _, tool := range c.Tools {
				affectedTools[tool] = struct{}{}
			}
		}

		result.Changes = append(result.Changes, &gen.OpenAPIv3EvaluationChange{
			Severity: gen.OpenAPIv3EvaluationSeverity(c.Severity),
			Category: gen.OpenAPIv3EvaluationCategory(c.Category),
			Code:     c.Code,
			Title:    c.Title,
			Details:  conv.DefaultSlice(c.Details, []string{}),
			Tools:    conv.DefaultSlice(c.Tools, []string{}),
		})
	}

	// A changed server URL names no tools because every tool from the source
	// calls it.
	affectsAllTools := slices.ContainsFunc(changes, func(c openapi.EvaluationChange) bool {
		return c.Severity != openapi.EvaluationSeverityInfo && c.Category == openapi.EvaluationCategoryServer
	})

	result.AffectedToolsets = make([]*gen.OpenAPIv3AffectedToolset, 0, len(toolsets))
	for _, ts := range toolsets {
		affected := 0
		for _, u := range ts.SourceToolUrns {
			tool, err := urn.ParseTool(u)
			if err != nil {
				continue
			}
			if _, ok := affectedTools[tool.Name]; ok || affectsAllTools {
				affected++
			}
		}

		result.AffectedToolsets = append(result.AffectedToolsets, &gen.OpenAPIv3AffectedToolset{
			ID:                ts.ID.String(),
			Name:              ts.Name,
			Slug:              ts.Slug,
			McpSlug:           conv.FromPGText[string](ts.McpSlug),
			McpEnabled:        ts.McpEnabled,
			OauthConfigured:   ts.OauthConfigured,
			ToolCount:         len(ts.SourceToolUrns),
			AffectedToolCount: affected,
		})
	}

	return result, nil
}

// readOpenAPIv3Asset loads the bytes of an OpenAPI document uploaded to the
// project.
func (s *Service) readOpenAPIv3Asset(ctx context.Context, projectID uuid.UUID, assetID uuid.UUID) ([]byte, error) {
	asset, err := s.assets.GetProjectAsset(ctx, assetsRepo.GetProjectAssetParams{ProjectID: projectID, ID: assetID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "openapi document asset not found").LogWarn(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error getting openapi document asset").LogError(ctx, s.logger)
	}

	if asset.Kind != openAPIv3AssetKind {
		return nil, oops.E(oops.CodeBadRequest, nil, "asset is not an openapi document").LogWarn(ctx, s.logger)
	}

	assetURL, err := url.Parse(asset.Url)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error parsing openapi document url").LogError(ctx, s.logger)
	}

	rc, err := s.assetStorage.Read(ctx, assetURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching openapi document").LogError(ctx, s.logger)
	}
	defer o11y.LogDefer(ctx, s.logger, "close openapi document reader", func() error { return rc.Close() })

	// Read one byte past the upload limit to tell a document at the limit
	// from one over it.
	data, err := io.ReadAll(io.LimitReader(rc, assets.MaxFileSizeOpenAPI+1))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading openapi document").LogError(ctx, s.logger)
	}
	if len(data) > assets.MaxFileSizeOpenAPI {
		return nil, oops.E(oops.CodeRequestTooLarge, nil, "openapi document is larger than the %d MiB upload limit", assets.MaxFileSizeOpenAPI>>20).LogWarn(ctx, s.logger)
	}

	return data, nil
}

func toSnapshotView(snap *openapi.DocumentSnapshot) *gen.OpenAPIv3DocumentSnapshot {
	schemes := make([]*gen.OpenAPIv3SecuritySchemeSummary, 0, len(snap.SecuritySchemes))
	for _, scheme := range snap.SecuritySchemes {
		schemes = append(schemes, &gen.OpenAPIv3SecuritySchemeSummary{
			Key:          scheme.Key,
			Type:         scheme.Type,
			Summary:      scheme.Summary,
			EnvVariables: conv.DefaultSlice(scheme.EnvVariables, []string{}),
		})
	}

	return &gen.OpenAPIv3DocumentSnapshot{
		ToolCount:             snap.ToolCount,
		SkippedOperationCount: snap.SkippedOperationCount,
		SecuritySchemes:       schemes,
		DefaultSecurity:       snap.DefaultSecurity,
		ServerURL:             snap.ServerURL,
	}
}
