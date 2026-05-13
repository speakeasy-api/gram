package external

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/workos/workos-go/v6/pkg/webhooks"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	goapkg "goa.design/goa/v3/pkg"

	gen "github.com/speakeasy-api/gram/server/gen/external"
	srv "github.com/speakeasy-api/gram/server/gen/http/external/server"
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

// webhookEventData is the relevant subset of the `data` field on a WorkOS
// webhook payload. ID and OrganizationID are populated differently per event
// type; see workOSOrganizationIDFromWebhook for the mapping.
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

	logger := h.logger.With(attr.SlogWorkOSEventType(event.Event))

	return h.dispatch(ctx, logger, event)
}

// dispatch routes a verified webhook to the per-domain Temporal workflow that
// owns the relevant cursor. Webhooks only nudge sync; each workflow's activity
// fetches authoritative pages from the Events API using its own cursor.
func (h *WebhookHandler) dispatch(ctx context.Context, logger *slog.Logger, event webhookEvent) error {
	switch event.Event {
	case string(workos.EventKindOrganizationCreated),
		string(workos.EventKindOrganizationUpdated),
		string(workos.EventKindOrganizationDeleted),
		string(workos.EventKindOrganizationRoleCreated),
		string(workos.EventKindOrganizationRoleUpdated),
		string(workos.EventKindOrganizationRoleDeleted),
		string(workos.EventKindOrganizationMembershipCreated),
		string(workos.EventKindOrganizationMembershipUpdated),
		string(workos.EventKindOrganizationMembershipDeleted):

		orgID := parseOrganizationID(event)
		if _, err := background.ExecuteProcessWorkOSOrganizationEventsWorkflowDebounced(ctx, h.temporalEnv, background.ProcessWorkOSEventsParams{
			WorkOSOrganizationID: orgID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to enqueue WorkOS organization sync").Log(ctx, logger)
		}
		return nil

	case string(workos.EventKindRoleCreated),
		string(workos.EventKindRoleUpdated),
		string(workos.EventKindRoleDeleted):
		if _, err := background.ExecuteProcessWorkOSGlobalRoleEventsWorkflowDebounced(ctx, h.temporalEnv); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to enqueue WorkOS global role sync").Log(ctx, logger)
		}
		return nil

	default:
		// user.*, dsync.*, and any new event types are accepted (so WorkOS
		// stops retrying) but not yet processed. Add a workflow before
		// enabling those subscriptions in the WorkOS dashboard.
		return nil
	}
}

// parseOrganizationID returns the WorkOS organization ID associated
// with the event, or "" if the event does not carry one (e.g. role.* and
// user.* are environment-scoped).
//
// `organization.*` events carry the org id on `data.id`; everything else org-
// scoped carries it on `data.organization_id`.
func parseOrganizationID(event webhookEvent) string {
	if strings.HasPrefix(event.Event, "organization.") {
		return event.Data.ID
	}
	return event.Data.OrganizationID
}
