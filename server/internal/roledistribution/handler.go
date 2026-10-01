package roledistribution

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

const (
	messageIDKey               = "message_id"
	eventKey                   = "event"
	roleURNKey                 = "role_urn"
	globalRoleIDKey            = "global_role_id"
	organizationIDKey          = "organization_id"
	bootstrapOrganizationIDKey = "bootstrap_organization_id"
	reasonKey                  = "reason"
	errorKey                   = "error"
)

// Processors supplies the durable processors used by the single-message consumer.
// Setup returning false without an error includes disabled or obsolete work.
type Processors struct {
	Setup                 func(context.Context, string, string) (bool, error)
	GlobalFanout          func(context.Context, uuid.UUID, string) error
	OrganizationBootstrap func(context.Context, string, string) error
}

type Handler struct {
	logger     *slog.Logger
	processors Processors
}

func NewHandler(logger *slog.Logger, processors Processors) *Handler {
	return &Handler{logger: logger, processors: processors}
}

// HandleRoleDistributionSetupRequested acknowledges poison payloads and skipped
// work. Processing failures use the subscription's best-effort retry/DLQ policy.
func (h *Handler) HandleRoleDistributionSetupRequested(ctx context.Context, event *roledistributionv1.RoleDistributionSetupRequestedV1, metadata gcp.MessageMetadata) error {
	setup, global, bootstrap := event.GetRoleUrn(), event.GetGlobalRoleId(), event.GetBootstrapOrganizationId()
	logger := h.logger.With(
		slog.Attr{Key: messageIDKey, Value: slog.StringValue(metadata.ID)},
		slog.Attr{Key: eventKey, Value: slog.StringValue("role_distribution.setup_requested_v1")},
		slog.Attr{Key: roleURNKey, Value: slog.StringValue(setup)},
		slog.Attr{Key: globalRoleIDKey, Value: slog.StringValue(global)},
		slog.Attr{Key: organizationIDKey, Value: slog.StringValue(event.GetOrganizationId())},
		slog.Attr{Key: bootstrapOrganizationIDKey, Value: slog.StringValue(bootstrap)},
	)
	targets := 0
	for _, target := range []string{setup, global, bootstrap} {
		if target != "" {
			targets++
		}
	}
	invalid := func(reason string) error {
		logger.LogAttrs(ctx, slog.LevelWarn, "acknowledging invalid role distribution request", slog.Attr{Key: reasonKey, Value: slog.StringValue(reason)})
		return nil
	}
	if targets != 1 || (setup == "" && event.GetOrganizationId() != "") {
		return invalid("exactly one target is required")
	}
	var err error
	switch {
	case setup != "":
		parts := strings.Split(setup, ":")
		if len(parts) != 3 || parts[0] != "role" || (parts[1] != "global" && parts[1] != "organization") {
			return invalid("setup requires a valid role URN")
		}
		id, parseErr := uuid.Parse(parts[2])
		if parseErr != nil || id == uuid.Nil || strings.TrimSpace(event.GetOrganizationId()) == "" || event.GetCursor() != "" {
			return invalid("setup requires a valid role URN, organization, and no cursor")
		}
		if h.processors.Setup == nil {
			err = fmt.Errorf("setup processor is not configured")
		} else {
			_, err = h.processors.Setup(ctx, setup, event.GetOrganizationId())
		}
	case global != "":
		id, parseErr := uuid.Parse(global)
		if parseErr != nil || id == uuid.Nil {
			return invalid("global role requires a valid UUID")
		}
		if h.processors.GlobalFanout == nil {
			err = fmt.Errorf("global fanout processor is not configured")
		} else {
			err = h.processors.GlobalFanout(ctx, id, event.GetCursor())
		}
	case bootstrap != "":
		if strings.TrimSpace(bootstrap) == "" {
			return invalid("bootstrap requires an organization")
		}
		if h.processors.OrganizationBootstrap == nil {
			err = fmt.Errorf("organization bootstrap processor is not configured")
		} else {
			err = h.processors.OrganizationBootstrap(ctx, bootstrap, event.GetCursor())
		}
	}
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "role distribution processing failed; requesting retry", slog.Attr{Key: errorKey, Value: slog.AnyValue(err)})
		return fmt.Errorf("process role distribution request: %w", err)
	}
	return nil
}
