package admin

import (
	"context"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) ListPlatformMcpFeedback(ctx context.Context, p *gen.ListPlatformMcpFeedbackPayload) (*gen.ListPlatformMcpFeedbackResult, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	rows, err := repo.New(s.db).AdminListPlatformMCPFeedback(ctx, int32(p.Limit)) //nolint:gosec // The design bounds limit to 1 through 500.
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list platform mcp feedback").LogError(ctx, s.logger)
	}
	result := &gen.ListPlatformMcpFeedbackResult{Feedback: make([]*gen.AdminPlatformMCPFeedback, 0, len(rows))}
	for _, row := range rows {
		var rating *int
		if row.Rating.Valid {
			value := int(row.Rating.Int32)
			rating = &value
		}
		result.Feedback = append(result.Feedback, &gen.AdminPlatformMCPFeedback{
			ID:               row.ID.String(),
			OrganizationID:   row.OrganizationID,
			OrganizationName: conv.FromPGText[string](row.OrganizationName),
			OrganizationSlug: conv.FromPGText[string](row.OrganizationSlug),
			SubjectUrn:       row.SubjectUrn,
			UserEmail:        conv.FromPGText[string](row.UserEmail),
			Category:         row.Category,
			Rating:           rating,
			Success:          conv.FromPGBool[bool](row.Success),
			ToolName:         conv.FromPGText[string](row.ToolName),
			FailureCategory:  conv.FromPGText[string](row.FailureCategory),
			Note:             conv.FromPGText[string](row.Note),
			DeliveryState:    row.DeliveryState,
			CreatedAt:        row.CreatedAt.Time.UTC().Format(time.RFC3339),
			ExpiresAt:        row.ExpiresAt.Time.UTC().Format(time.RFC3339),
		})
	}
	return result, nil
}
