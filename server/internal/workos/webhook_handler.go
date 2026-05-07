package workossvc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"

	"github.com/workos/workos-go/v6/pkg/webhooks"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	goapkg "goa.design/goa/v3/pkg"

	srv "github.com/speakeasy-api/gram/server/gen/http/workos/server"
	gen "github.com/speakeasy-api/gram/server/gen/workos"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// WebhookHandler is the Goa service implementation for the WorkOS webhook endpoint.
type WebhookHandler struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	webhooksClient *webhooks.Client
	temporalEnv    *tenv.Environment
}

func NewWebhookHandler(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	webhooksClient *webhooks.Client,
	temporalEnv *tenv.Environment,
) *WebhookHandler {
	return &WebhookHandler{
		logger:         logger,
		tracer:         tracerProvider.Tracer("workos"),
		webhooksClient: webhooksClient,
		temporalEnv:    temporalEnv,
	}
}

var _ gen.Service = (*WebhookHandler)(nil)

// oopsFormatter restores the correct HTTP status code on errors that passed
// through MapErrors(). MapErrors converts ShareableError → goa.ServiceError
// with fault=false/temporary=false, which Goa would otherwise default to 400.
// The ServiceError.Name is the oops.Code string, so we remap via StatusCodes.
func oopsFormatter(ctx context.Context, err error) goahttp.Statuser {
	var se *goapkg.ServiceError
	if errors.As(err, &se) {
		if status, ok := oops.StatusCodes[oops.Code(se.Name)]; ok {
			return &staticStatus{status}
		}
	}
	return nil
}

type staticStatus struct{ code int }

func (s *staticStatus) StatusCode() int { return s.code }

func AttachWebhookHandler(mux goahttp.Muxer, h *WebhookHandler) {
	endpoints := gen.NewEndpoints(h)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(h.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, oopsFormatter))
}

type webhookEventData struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
}

type webhookEvent struct {
	Event string           `json:"event"`
	Data  webhookEventData `json:"data"`
}

func (h *WebhookHandler) ReceiveWorkOSWebhook(ctx context.Context, payload *gen.ReceiveWorkOSWebhookPayload, body io.ReadCloser) error {
	defer o11y.NoLogDefer(func() error { return body.Close() })

	if payload.WorkosSignature == nil || *payload.WorkosSignature == "" {
		return oops.E(oops.CodeUnauthorized, errors.New("missing WorkOS signature"), "missing WorkOS-Signature header")
	}

	if h.webhooksClient == nil {
		return oops.E(oops.CodeUnauthorized, errors.New("webhooks not configured"), "webhook processing not available")
	}

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, h.logger)
	}

	if _, err := h.webhooksClient.ValidatePayload(*payload.WorkosSignature, string(bodyBytes)); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "invalid WorkOS webhook signature")
	}

	var event webhookEvent
	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid webhook payload").Log(ctx, h.logger)
	}

	switch workos.EventKind(event.Event) {
	case workos.EventKindOrganizationMembershipCreated,
		workos.EventKindOrganizationMembershipUpdated,
		workos.EventKindOrganizationMembershipDeleted:
		if _, err := background.ExecuteProcessWorkOSMembershipEventsWorkflowDebounced(ctx, h.temporalEnv); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to enqueue membership sync").Log(ctx, h.logger)
		}
		return nil

	case workos.EventKindOrganizationCreated,
		workos.EventKindOrganizationUpdated,
		workos.EventKindOrganizationDeleted:
		if event.Data.ID == "" {
			return oops.E(oops.CodeBadRequest, errors.New("missing organization id"), "invalid organization event payload").Log(ctx, h.logger)
		}
		if _, err := background.ExecuteProcessWorkOSOrganizationEventsWorkflowDebounced(ctx, h.temporalEnv, background.ProcessWorkOSEventsParams{
			WorkOSOrganizationID: event.Data.ID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to enqueue org event sync").Log(ctx, h.logger)
		}
		return nil

	case workos.EventKindOrganizationRoleCreated,
		workos.EventKindOrganizationRoleUpdated,
		workos.EventKindOrganizationRoleDeleted:
		if event.Data.OrganizationID == "" {
			return oops.E(oops.CodeBadRequest, errors.New("missing organization id"), "invalid organization role event payload").Log(ctx, h.logger)
		}
		if _, err := background.ExecuteProcessWorkOSOrganizationEventsWorkflowDebounced(ctx, h.temporalEnv, background.ProcessWorkOSEventsParams{
			WorkOSOrganizationID: event.Data.OrganizationID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to enqueue org role sync").Log(ctx, h.logger)
		}
		return nil

	case workos.EventKindRoleCreated,
		workos.EventKindRoleUpdated,
		workos.EventKindRoleDeleted:
		if _, err := background.ExecuteProcessWorkOSGlobalRoleEventsWorkflowDebounced(ctx, h.temporalEnv); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to enqueue global role sync").Log(ctx, h.logger)
		}
		return nil

	default:
		h.logger.InfoContext(ctx, "WorkOS webhook event type not handled, skipping",
			attr.SlogWorkOSEventType(event.Event),
		)
		return nil
	}
}
