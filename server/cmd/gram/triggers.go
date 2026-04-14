package gram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

type triggerDeliveryLogger struct {
	telemetry *telemetry.Logger
}

func (l *triggerDeliveryLogger) LogTriggerDelivery(
	instance triggerrepo.TriggerInstance,
	envelope bgtriggers.EventEnvelope,
	status bgtriggers.DeliveryStatus,
	reason string,
	err error,
) {
	if l == nil || l.telemetry == nil {
		return
	}

	body := fmt.Sprintf("trigger event %s", status)
	if reason != "" {
		body += ": " + reason
	}

	attributes := map[attr.Key]any{
		attr.EventSourceKey:           string(telemetry.EventSourceTrigger),
		attr.TriggerDefinitionSlugKey: instance.DefinitionSlug,
		attr.TriggerInstanceIDKey:     instance.ID.String(),
		attr.TriggerEventIDKey:        envelope.EventID,
		attr.TriggerCorrelationIDKey:  envelope.CorrelationID,
		attr.TriggerDeliveryStatusKey: string(status),
		attr.TriggerTargetKindKey:     instance.TargetKind,
		attr.TriggerTargetRefKey:      instance.TargetRef,
		attr.LogBodyKey:               body,
		attr.LogSeverityKey:           conv.Ternary(status == bgtriggers.DeliveryStatusFailed, "ERROR", "INFO"),
	}
	if reason != "" {
		attributes[attr.ReasonKey] = reason
	}
	if instance.EnvironmentID.Valid {
		attributes[attr.EnvironmentIDKey] = instance.EnvironmentID.UUID.String()
	}
	if err != nil {
		attributes[attr.ErrorMessageKey] = err.Error()
	}

	l.telemetry.Log(context.Background(), telemetry.LogParams{
		Timestamp: conv.Default(envelope.ReceivedAt, time.Now().UTC()),
		ToolInfo: telemetry.ToolInfo{
			ID:             instance.ID.String(),
			URN:            "urn:uuid:" + instance.ID.String(),
			Name:           "trigger:" + instance.DefinitionSlug,
			ProjectID:      instance.ProjectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: instance.OrganizationID,
		},
		Attributes: attributes,
	})
}

func newTriggersApp(
	logger *slog.Logger,
	db *pgxpool.Pool,
	enc *encryption.Client,
	temporalEnv *temporal.Environment,
	telemetryLogger *telemetry.Logger,
	serverURL *url.URL,
) *bgtriggers.App {
	envEntries := environments.NewEnvironmentEntries(logger, db, enc, nil)
	return bgtriggers.NewApp(
		logger,
		db,
		temporalEnv,
		envEntries,
		&triggerDeliveryLogger{telemetry: telemetryLogger},
		serverURL,
		bgtriggers.NewNoopDispatcher(logger),
	)
}
