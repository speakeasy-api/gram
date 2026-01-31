package mv

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/templates/repo"
)

func DescribePromptTemplate(
	ctx context.Context,
	logger *slog.Logger,
	tx DBTX,
	projectID ProjectID,
	id PromptTemplateID,
	name PromptTemplateName,
) (*types.PromptTemplate, error) {
	pid := uuid.UUID(projectID)
	ptid := uuid.NullUUID(id)
	var pname *string = name

	r := repo.New(tx)

	var row repo.PromptTemplate
	var err error
	if ptid.Valid {
		row, err = r.GetTemplateByID(ctx, repo.GetTemplateByIDParams{
			ProjectID: pid,
			ID:        ptid.UUID,
		})
	} else if pname != nil && *pname != "" {
		row, err = r.GetTemplateByName(ctx, repo.GetTemplateByNameParams{
			ProjectID: pid,
			Name:      *pname,
		})
	} else {
		return nil, oops.E(oops.CodeBadRequest, err, "id or name is required to lookup template").Log(ctx, logger)
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "template not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get template").Log(ctx, logger)
	}

	pt := fromPromptTemplateRow(row)
	err = ApplyVariations(ctx, logger, tx, pid, []*types.Tool{{PromptTemplate: pt}})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to apply variations to prompt template").Log(ctx, logger)
	}

	return pt, nil
}

func DescribePromptTemplates(
	ctx context.Context,
	logger *slog.Logger,
	tx DBTX,
	projectID ProjectID,
) ([]*types.PromptTemplate, error) {
	pid := uuid.UUID(projectID)

	if err := inv.Check(
		"describe prompt template inputs",
		"project id is set", pid != uuid.Nil,
	); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "not enough information to get prompt template").Log(ctx, logger)
	}

	r := repo.New(tx)
	rows, err := r.ListTemplates(ctx, pid)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get template").Log(ctx, logger)
	}

	templates := make([]*types.PromptTemplate, 0, len(rows))
	for _, row := range rows {
		pt := fromPromptTemplateRow(row)
		err = ApplyVariations(ctx, logger, tx, pid, []*types.Tool{{PromptTemplate: pt}})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to apply variations to prompt template").Log(ctx, logger)
		}
		templates = append(templates, pt)
	}

	return templates, nil
}

func fromPromptTemplateRow(row repo.PromptTemplate) *types.PromptTemplate {
	var tools []string
	if len(row.ToolsHint) > 0 {
		tools = row.ToolsHint
	}
	var toolUrns []string
	if len(row.ToolUrnsHint) > 0 {
		toolUrns = row.ToolUrnsHint
	}

	return &types.PromptTemplate{
		ID:            row.ID.String(),
		ToolUrn:       row.ToolUrn.String(),
		HistoryID:     row.HistoryID.String(),
		PredecessorID: conv.FromNullableUUID(row.PredecessorID),
		Name:          row.Name,
		Prompt:        row.Prompt,
		Description:   conv.PtrValOrEmpty(conv.FromPGText[string](row.Description), ""),
		Schema:        conv.Default(string(row.Arguments), constants.DefaultEmptyToolSchema),
		SchemaVersion: nil,
		Engine:        conv.PtrValOr(conv.FromPGText[string](row.Engine), ""),
		Kind:          conv.PtrValOrEmpty(conv.FromPGText[string](row.Kind), "prompt"),
		ToolsHint:     tools,
		ToolUrnsHint:  toolUrns,
		CreatedAt:     row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     row.UpdatedAt.Time.Format(time.RFC3339),
		ProjectID:     row.ProjectID.String(),
		CanonicalName: row.Name,
		Confirm:       nil,
		ConfirmPrompt: nil,
		Summarizer:    nil,
		Canonical:     nil,
		Variation:     nil,
		Annotations:   nil,
	}
}
