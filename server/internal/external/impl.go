package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/webhooks"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/api/serviceerror"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/external"
	srv "github.com/speakeasy-api/gram/server/gen/http/external/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/temporal"
)

const maxWebhookBodySize = 1 << 20 // 1MB

type Service struct {
	tracer        trace.Tracer
	logger        *slog.Logger
	db            *pgxpool.Pool
	webhookClient *webhooks.Client
	temporalEnv   *temporal.Environment
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, webhookClient *webhooks.Client, temporalEnv *temporal.Environment) *Service {
	return &Service{
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/external"),
		logger:        logger.With(attr.SlogComponent("external")),
		db:            db,
		webhookClient: webhookClient,
		temporalEnv:   temporalEnv,
	}
}

// rawWorkOSSignatureKey stores the WorkOS-Signature header value in context before
// Goa's APIKey Bearer-stripping mangles it.
type rawWorkOSSignatureKey struct{}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))

	goaServer := srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)

	// WorkOS sends "t=<ts>, v1=<sig>" with a space after the comma. Goa's APIKey
	// Bearer-stripping removes everything before the first space, leaving "v1=<sig>"
	// which fails parseSignatureHeader. Capture the raw header in context before Goa
	// processes it so ReceiveWorkOSWebhook can use it directly.
	orig := goaServer.ReceiveWorkOSWebhook
	goaServer.ReceiveWorkOSWebhook = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.Header.Get(constants.WorkOSSignatureHeader)
		r = r.WithContext(context.WithValue(r.Context(), rawWorkOSSignatureKey{}, sig))
		orig.ServeHTTP(w, r)
	})

	srv.Mount(mux, goaServer)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	logger := s.logger

	switch scheme.Name {
	case constants.WorkOSSignatureSecurityScheme:
		// Validated within the service method because we need the request body.
		return ctx, nil
	default:
		err := fmt.Errorf("unexpected security scheme: %s", scheme.Name)
		return ctx, oops.E(oops.CodeUnexpected, err, "unable to authorize request").Log(ctx, logger)
	}
}

// ReceiveWorkOSWebhook implements [external.Service]. For now this PR only
// routes organization.* events into the per-org sync workflow. Membership and
// role events are accepted but ignored — they will be wired in their own slices.
func (s *Service) ReceiveWorkOSWebhook(ctx context.Context, payload *gen.ReceiveWorkOSWebhookPayload, body io.ReadCloser) (err error) {
	logger := s.logger
	defer o11y.NoLogDefer(func() error { return body.Close() })

	signature, _ := ctx.Value(rawWorkOSSignatureKey{}).(string)
	if signature == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	bs, err := io.ReadAll(io.LimitReader(body, maxWebhookBodySize))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "unable to read request body").Log(ctx, logger)
	}

	validated, err := s.webhookClient.ValidatePayload(signature, string(bs))
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "invalid WorkOS webhook signature").Log(ctx, logger)
	}

	var event struct {
		ID   string `json:"id"`
		Type string `json:"event"`
		Data struct {
			ID             string `json:"id"`
			Object         string `json:"object"`
			OrganizationID string `json:"organization_id"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(validated), &event); err != nil {
		return oops.E(oops.CodeBadRequest, err, "unable to parse workos webhook payload").Log(ctx, logger)
	}

	// Only organization.* events are routed in this slice. Other event types are
	// accepted (so WorkOS gets a 2xx and won't retry) but ignored.
	if event.Data.Object != "organization" {
		return nil
	}

	orgID := conv.Ternary(event.Data.Object == "organization", event.Data.ID, event.Data.OrganizationID)
	if orgID == "" {
		return nil
	}

	_, err = background.ExecuteProcessWorkOSOrganizationEventsWorkflowDebounced(ctx, s.temporalEnv, background.ProcessWorkOSEventsParams{
		WorkOSOrganizationID: orgID,
	})
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	switch {
	case errors.As(err, &alreadyStarted):
		// Already running, signal collapsed via debounce.
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to start workos events workflow").Log(ctx, logger, attr.SlogWorkOSOrganizationID(orgID))
	}

	return nil
}
