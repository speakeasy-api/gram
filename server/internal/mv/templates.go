package mv

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/inv"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/templates/repo"
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

	return fromPromptTemplateRow(row), nil
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

	pt := make([]*types.PromptTemplate, 0, len(rows))
	for _, row := range rows {
		pt = append(pt, fromPromptTemplateRow(row))
	}

	return pt, nil
}

func fromPromptTemplateRow(row repo.PromptTemplate) *types.PromptTemplate {
	var args *string
	if len(row.Arguments) == 0 {
		args = nil
	} else {
		args = conv.Ptr(string(row.Arguments))
	}

	var tools []string
	if len(row.ToolsHint) > 0 {
		tools = row.ToolsHint
	}

	return &types.PromptTemplate{
		ID:            row.ID.String(),
		HistoryID:     row.HistoryID.String(),
		PredecessorID: conv.FromNullableUUID(row.PredecessorID),
		Name:          types.Slug(row.Name),
		Prompt:        row.Prompt,
		Description:   conv.FromPGText[string](row.Description),
		Arguments:     args,
		Engine:        conv.PtrValOrEmpty(conv.FromPGText[string](row.Engine), "mustache"),
		Kind:          conv.PtrValOrEmpty(conv.FromPGText[string](row.Kind), "prompt"),
		ToolsHint:     tools,
		CreatedAt:     row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
