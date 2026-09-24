package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func newPICascade(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, client openrouter.CompletionClient, policy *guardian.Policy, provisioner openrouter.Provisioner, flags feature.Provider, db repo.DBTX, limiter *ratelimit.Limiter) *piopenrouter.Cascade {
	jev := typesafe.New(policy.PooledClient(), func(ctx context.Context, orgID string) (string, error) {
		key, err := provisioner.ProvisionAPIKey(ctx, orgID, openrouter.KeyTypeInternal)
		if err != nil {
			return "", fmt.Errorf("provision Jev internal key: %w", err)
		}
		return key, nil
	})
	enabled := func(ctx context.Context, orgID, projectID string) bool {
		id, err := uuid.Parse(projectID)
		if err != nil {
			return false
		}
		groups, err := repo.New(db).GetProjectFlagGroups(ctx, id)
		if err != nil {
			logger.WarnContext(ctx, "resolve PI cascade rollout groups", attr.SlogError(err))
			return false
		}
		on, err := flags.IsFlagEnabledLocal(ctx, feature.FlagRiskPromptInjectionCascade, orgID, feature.OrgProjectGroups(groups.OrganizationSlug, groups.ProjectSlug), nil)
		if err != nil {
			logger.WarnContext(ctx, "evaluate PI cascade rollout", attr.SlogError(err))
			return false
		}
		return on
	}
	return piopenrouter.NewCascade(logger, tracerProvider, meterProvider, client, limiter, jev, enabled, judgemessage.NewWindowLoader(db).Load)
}
