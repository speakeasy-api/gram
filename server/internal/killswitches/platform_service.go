package killswitches

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/platform_killswitches/server"
	gen "github.com/speakeasy-api/gram/server/gen/platform_killswitches"
	"github.com/speakeasy-api/gram/server/internal/audit"
	gramauth "github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const maxPlatformKillswitchRequestBodyBytes = 2 << 20

// PlatformService is the separately authorized main-server break-glass adapter.
// It deliberately has no evaluator dependency. A nil generic service keeps the
// transport mounted but fail-closed until production definitions and their
// authoritative validators are configured.
type PlatformService struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	auth     *gramauth.Auth
	sessions gramauth.PlatformAdminEntitlementReader
	generic  GenericService
}

var _ gen.Service = (*PlatformService)(nil)
var _ gen.Auther = (*PlatformService)(nil)

func NewPlatformService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	generic GenericService,
) *PlatformService {
	return &PlatformService{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/killswitches/platform"),
		logger:   logger,
		auth:     gramauth.New(logger, db, sessionManager, authzEngine),
		sessions: sessionManager,
		generic:  generic,
	}
}

func AttachPlatformService(mux goahttp.Muxer, service *PlatformService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, platformKillswitchRequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func platformKillswitchRequestDecoder(r *http.Request) goahttp.Decoder {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(nil, r.Body, maxPlatformKillswitchRequestBodyBytes)
	}
	return goahttp.RequestDecoder(r)
}

func (s *PlatformService) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *PlatformService) ListDefinitions(ctx context.Context, _ *gen.ListDefinitionsPayload) (*gen.ListDefinitionsResult, error) {
	ctx, _, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	generic, err := s.configuredGeneric()
	if err != nil {
		return nil, err
	}
	definitions, err := generic.ListDefinitions(ctx)
	if err != nil {
		return nil, mapPlatformLifecycleError(err)
	}
	result := make([]*gen.PlatformKillswitchDefinition, len(definitions))
	for i, definition := range definitions {
		result[i] = platformDefinition(definition)
	}
	return &gen.ListDefinitionsResult{Definitions: result}, nil
}

func (s *PlatformService) ActivatePrescription(ctx context.Context, payload *gen.ActivatePrescriptionPayload) (*gen.PlatformKillswitchMutationResult, error) {
	ctx, actor, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	generic, err := s.configuredGeneric()
	if err != nil {
		return nil, err
	}
	operationID, err := uuid.Parse(payload.OperationID)
	if err != nil {
		return nil, badPlatformRequest(err)
	}
	desired, err := platformDesired(payload.ResourceScope, payload.SelectedResourceInputs, payload.StartMode, payload.StartsAt, payload.ExpiresAt, payload.InternalNote, payload.ExternalNote)
	if err != nil {
		return nil, err
	}
	var prescriptionID *PrescriptionID
	if payload.PrescriptionID != nil {
		value := PrescriptionID(*payload.PrescriptionID)
		prescriptionID = &value
	}
	result, err := generic.ActivatePrescription(ctx, ActivatePrescriptionInput{
		MutationContext: MutationContext{OrganizationID: OrganizationID(payload.OrganizationID), ActorUserID: actor.userID, ActorDisplayName: actor.email, OperationID: operationID},
		PrescriptionID:  prescriptionID, ExpectedVersion: payload.ExpectedVersion,
		Definition: DefinitionKey(conv.PtrValOrEmpty(payload.Definition, "")), PrincipalKind: PrincipalKind(conv.PtrValOrEmpty(payload.PrincipalKind, "")), PrincipalInput: conv.PtrValOrEmpty(payload.PrincipalInput, ""),
		ResourceKind: ResourceKind(conv.PtrValOrEmpty(payload.ResourceKind, "")), Desired: desired,
	})
	if err != nil {
		return nil, mapPlatformLifecycleError(err)
	}
	return platformMutationResult(result), nil
}

func (s *PlatformService) ChangePrescription(ctx context.Context, payload *gen.ChangePrescriptionPayload) (*gen.PlatformKillswitchMutationResult, error) {
	ctx, actor, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	generic, err := s.configuredGeneric()
	if err != nil {
		return nil, err
	}
	operationID, err := uuid.Parse(payload.OperationID)
	if err != nil {
		return nil, badPlatformRequest(err)
	}
	desired, err := platformDesired(payload.ResourceScope, payload.SelectedResourceInputs, payload.StartMode, payload.StartsAt, payload.ExpiresAt, payload.InternalNote, payload.ExternalNote)
	if err != nil {
		return nil, err
	}
	result, err := generic.ChangePrescription(ctx, ChangePrescriptionRequest{
		MutationContext: MutationContext{OrganizationID: OrganizationID(payload.OrganizationID), ActorUserID: actor.userID, ActorDisplayName: actor.email, OperationID: operationID},
		PrescriptionID:  PrescriptionID(payload.PrescriptionID), ExpectedVersion: payload.ExpectedVersion, Desired: desired,
	})
	if err != nil {
		return nil, mapPlatformLifecycleError(err)
	}
	return platformMutationResult(result), nil
}

func (s *PlatformService) DeactivatePrescription(ctx context.Context, payload *gen.DeactivatePrescriptionPayload) (*gen.PlatformKillswitchMutationResult, error) {
	ctx, actor, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	generic, err := s.configuredGeneric()
	if err != nil {
		return nil, err
	}
	operationID, err := uuid.Parse(payload.OperationID)
	if err != nil {
		return nil, badPlatformRequest(err)
	}
	result, err := generic.DeactivatePrescription(ctx, DeactivatePrescriptionRequest{
		MutationContext: MutationContext{OrganizationID: OrganizationID(payload.OrganizationID), ActorUserID: actor.userID, ActorDisplayName: actor.email, OperationID: operationID},
		PrescriptionID:  PrescriptionID(payload.PrescriptionID), ExpectedVersion: payload.ExpectedVersion,
	})
	if err != nil {
		return nil, mapPlatformLifecycleError(err)
	}
	return platformMutationResult(result), nil
}

func (s *PlatformService) GetPrescription(ctx context.Context, payload *gen.GetPrescriptionPayload) (*gen.PlatformKillswitchPrescription, error) {
	ctx, _, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	generic, err := s.configuredGeneric()
	if err != nil {
		return nil, err
	}
	prescription, err := generic.GetPrescription(ctx, GetPrescriptionRequest{OrganizationID: OrganizationID(payload.OrganizationID), PrescriptionID: PrescriptionID(payload.PrescriptionID)})
	if err != nil {
		return nil, mapPlatformLifecycleError(err)
	}
	return platformPrescription(prescription), nil
}

func (s *PlatformService) ListPrescriptions(ctx context.Context, payload *gen.ListPrescriptionsPayload) (*gen.ListPrescriptionsResult, error) {
	ctx, _, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	generic, err := s.configuredGeneric()
	if err != nil {
		return nil, err
	}
	limit := int32(0)
	if payload.Limit != nil {
		limit = *payload.Limit
	}
	var afterID *PrescriptionID
	if payload.AfterID != nil {
		value := PrescriptionID(*payload.AfterID)
		afterID = &value
	}
	prescriptions, err := generic.ListPrescriptions(ctx, ListPrescriptionsRequest{OrganizationID: OrganizationID(payload.OrganizationID), Limit: limit, AfterID: afterID})
	if err != nil {
		return nil, mapPlatformLifecycleError(err)
	}
	result := make([]*gen.PlatformKillswitchPrescription, len(prescriptions.Prescriptions))
	for i, prescription := range prescriptions.Prescriptions {
		result[i] = platformPrescription(prescription)
	}
	var nextAfterID *string
	if prescriptions.NextAfterID != nil {
		value := string(*prescriptions.NextAfterID)
		nextAfterID = &value
	}
	return &gen.ListPrescriptionsResult{Prescriptions: result, NextAfterID: nextAfterID}, nil
}

type platformActor struct {
	userID string
	email  string
}

func (s *PlatformService) authorize(ctx context.Context) (context.Context, platformActor, error) {
	authCtx, _, err := gramauth.RequireFreshPlatformAdminSession(ctx, s.logger, s.sessions)
	if err != nil {
		return ctx, platformActor{}, fmt.Errorf("authorize platform killswitch request: %w", err)
	}
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfacePlatformBreakGlass))
	return ctx, platformActor{userID: authCtx.UserID, email: strings.TrimSpace(*authCtx.Email)}, nil
}

func (s *PlatformService) configuredGeneric() (GenericService, error) {
	if isNilInterface(s.generic) {
		return nil, oops.E(oops.CodeUnavailable, ErrOperationUnavailable, "platform killswitch lifecycle is not configured")
	}
	return s.generic, nil
}

func mapPlatformLifecycleError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*oops.ShareableError](err); ok {
		return err
	}
	switch {
	case errors.Is(err, ErrInvalidArgument), errors.Is(err, ErrInvalidReference), errors.Is(err, ErrInvalidTransition):
		return oops.E(oops.CodeBadRequest, err, "request is invalid")
	case errors.Is(err, ErrPrescriptionNotFound):
		return oops.E(oops.CodeNotFound, err, "resource not found")
	case errors.Is(err, ErrOperationConflict):
		return gen.MakeOperationConflict(err)
	case errors.Is(err, ErrVersionConflict):
		return gen.MakeVersionConflict(err)
	case errors.Is(err, ErrOperationUnavailable):
		return oops.E(oops.CodeUnavailable, err, "service is temporarily unavailable")
	default:
		return oops.E(oops.CodeUnexpected, err, "unexpected error occurred")
	}
}

func badPlatformRequest(err error) error {
	return oops.E(oops.CodeBadRequest, err, "request is invalid")
}

func platformDesired(resourceScope string, selected []string, startMode string, startsAt, expiresAt *string, internalNote, externalNote string) (DesiredVersionInput, error) {
	start, err := parsePlatformTime(startsAt)
	if err != nil {
		return DesiredVersionInput{}, badPlatformRequest(fmt.Errorf("parse starts_at: %w", err))
	}
	expiry, err := parsePlatformTime(expiresAt)
	if err != nil {
		return DesiredVersionInput{}, badPlatformRequest(fmt.Errorf("parse expires_at: %w", err))
	}
	return DesiredVersionInput{ResourceScope: ResourceScope(resourceScope), SelectedResourceInputs: selected, StartMode: StartMode(startMode), StartsAt: start, ExpiresAt: expiry, InternalNote: internalNote, ExternalNote: externalNote}, nil
}

func parsePlatformTime(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return nil, fmt.Errorf("parse RFC 3339 timestamp: %w", err)
	}
	return &parsed, nil
}

func platformDefinition(definition Definition) *gen.PlatformKillswitchDefinition {
	return &gen.PlatformKillswitchDefinition{
		Key: string(definition.Key), PrincipalKinds: stringsFrom(definition.PrincipalKinds), ResourceKinds: stringsFrom(definition.ResourceKinds),
		FailurePolicy: string(definition.FailurePolicy), DefaultExternalNote: definition.DefaultExternalNote, EnforcementOwner: definition.EnforcementOwner,
		IdentityContract: string(definition.IdentityContract), Surfaces: stringsFrom(definition.Surfaces), TransportAdapters: stringsFrom(definition.TransportAdapters),
	}
}

func platformMutationResult(result MutationResult) *gen.PlatformKillswitchMutationResult {
	return &gen.PlatformKillswitchMutationResult{PrescriptionID: string(result.PrescriptionID), Version: result.Version, State: string(result.State), Replayed: result.Replayed}
}

func platformPrescription(prescription CurrentPrescription) *gen.PlatformKillswitchPrescription {
	return &gen.PlatformKillswitchPrescription{
		ID: string(prescription.ID), OrganizationID: string(prescription.OrganizationID), Definition: string(prescription.Definition),
		PrincipalKind: string(prescription.PrincipalKind), PrincipalKey: string(prescription.PrincipalKey), ResourceKind: string(prescription.ResourceKind),
		Version: prescription.Version, State: string(prescription.State), ResourceScope: string(prescription.ResourceScope),
		SelectedResourceKeys: stringsFrom(prescription.SelectedResourceKeys), StartsAt: prescription.StartsAt.Format(time.RFC3339Nano),
		ExpiresAt: formatPlatformTime(prescription.ExpiresAt), ActivatedAt: formatPlatformTime(prescription.ActivatedAt), SupersededAt: formatPlatformTime(prescription.SupersededAt),
		InternalNote: prescription.InternalNote, ExternalNote: prescription.ExternalNote,
	}
}

func formatPlatformTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339Nano)
	return &formatted
}

func stringsFrom[T ~string](values []T) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}
