//nolint:exhaustruct // Optional transport and nullable database fields intentionally use documented zero values.
package killswitchapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/killswitches/server"
	gen "github.com/speakeasy-api/gram/server/gen/killswitches"
	"github.com/speakeasy-api/gram/server/internal/audit"
	gramauth "github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	killswitchrepo "github.com/speakeasy-api/gram/server/internal/killswitches/repo"
	"github.com/speakeasy-api/gram/server/internal/management/readmodel"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	CapabilityMCPToolCalls = "mcp_tool_calls"
	capabilityLabel        = "MCP tool calls"
	maxHistoryEvents       = int32(100)
	maxBatchUsers          = 100
	maxRequestBodyBytes    = 2 << 20
)

type Service struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	auth       *gramauth.Auth
	db         *pgxpool.Pool
	authorized *killswitches.AuthorizedService
	generic    killswitches.GenericService
	user       killswitches.PrincipalAdapter
	server     killswitches.ResourceAdapter
	features   feature.Provider
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine, auditLogger *audit.Logger, features feature.Provider) (*Service, error) {
	registry, err := mcptoolexecution.NewRegistry(db)
	if err != nil {
		return nil, fmt.Errorf("build MCP tool-call killswitch registry: %w", err)
	}
	lifecycle, err := killswitches.NewLifecycleService(
		db, registry, mcptoolexecution.NewCustomerLifecycleValidator(), killswitches.NewAuditBeforeCommitHook(auditLogger),
		killswitches.WithBeforeApplyHook(rolloutBeforeApply(features)),
	)
	if err != nil {
		return nil, fmt.Errorf("build killswitch lifecycle service: %w", err)
	}
	user, ok := registry.PrincipalAdapter(mcptoolexecution.PrincipalKindUser)
	if !ok {
		return nil, errors.New("MCP tool-call killswitch registry has no user adapter")
	}
	server, ok := registry.ResourceAdapter(mcptoolexecution.ResourceKindMCPServer)
	if !ok {
		return nil, errors.New("MCP tool-call killswitch registry has no server adapter")
	}
	facade, err := killswitches.NewFacade(lifecycle)
	if err != nil {
		return nil, fmt.Errorf("build killswitch facade: %w", err)
	}
	authorized, err := killswitches.NewAuthorizedService(facade, authzEngine)
	if err != nil {
		return nil, fmt.Errorf("build authorized killswitch service: %w", err)
	}
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/killswitchapi"), logger: logger,
		auth: gramauth.New(logger, db, sessionManager, authzEngine), db: db, authorized: authorized, generic: facade,
		user: user, server: server, features: features,
	}, nil
}

func (s *Service) GenericService() killswitches.GenericService {
	return s.generic
}

func rolloutBeforeApply(features feature.Provider) killswitches.BeforeApplyHook {
	return func(ctx context.Context, mutation killswitches.MutationContext, operation killswitches.MutationOperation) error {
		if operation == killswitches.MutationOperationDeactivate {
			return nil
		}
		mode, err := mcptoolexecution.ResolveRolloutMode(ctx, features, string(mutation.OrganizationID))
		if err != nil {
			return fmt.Errorf("%w: resolve MCP killswitch rollout mode: %w", killswitches.ErrOperationUnavailable, err)
		}
		if mode != mcptoolexecution.RolloutModeEnforce {
			return fmt.Errorf("%w: killswitch activation is not enabled for this organization", killswitches.ErrOperationUnavailable)
		}
		return nil
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, requestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func requestDecoder(r *http.Request) goahttp.Decoder {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(nil, r.Body, maxRequestBodyBytes)
	}
	return goahttp.RequestDecoder(r)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListCapabilities(ctx context.Context, _ *gen.ListCapabilitiesPayload) (*gen.KillswitchListCapabilitiesResult, error) {
	if _, err := s.organization(ctx); err != nil {
		return nil, err
	}
	return &gen.KillswitchListCapabilitiesResult{
		Capabilities: []*gen.KillswitchCapability{{Key: CapabilityMCPToolCalls, Label: capabilityLabel}},
		ComingSoon:   []*gen.KillswitchComingSoonCapability{{Label: "AI access"}},
	}, nil
}

func (s *Service) ListMCPServers(ctx context.Context, _ *gen.ListMCPServersPayload) (*gen.KillswitchListMCPServersResult, error) {
	organizationID, err := s.organization(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := readmodel.New(s.db).ListMCPServersForOrganization(ctx, string(organizationID))
	if err != nil {
		return nil, mapError(fmt.Errorf("list organization MCP servers: %w", err))
	}
	servers := make([]*gen.KillswitchMCPServer, len(rows))
	for i, row := range rows {
		servers[i] = &gen.KillswitchMCPServer{ID: row.ID.String(), Name: row.Name.String, ProjectID: row.ProjectID.String()}
	}
	return &gen.KillswitchListMCPServersResult{Servers: servers}, nil
}

type listCursor struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
	AsOf      string `json:"as_of"`
	Watermark int64  `json:"watermark"`
	Filter    string `json:"filter"`
}

func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.KillswitchListResult, error) {
	organizationID, err := s.organization(ctx)
	if err != nil {
		return nil, err
	}
	if payload.CapabilityKey != nil && *payload.CapabilityKey != CapabilityMCPToolCalls {
		return nil, badRequest(errors.New("capability is not available"))
	}
	limit := int32(constants.DefaultPageLimit)
	if payload.Limit != nil {
		limit = *payload.Limit
	}
	if limit < 1 || limit > int32(constants.MaxPageLimit) {
		return nil, badRequest(errors.New("limit must be between 1 and 100"))
	}

	var principalKey *killswitches.PrincipalKey
	if payload.UserID != nil {
		canonical, err := s.canonicalUser(organizationID, *payload.UserID)
		if err != nil {
			return nil, err
		}
		key := killswitches.PrincipalKey(canonical)
		principalKey = &key
	}
	var status *killswitches.CustomerStatus
	if payload.Status != nil {
		value := killswitches.CustomerStatus(*payload.Status)
		status = &value
	}
	filter := listFilter(principalKey, status)

	var cursor *killswitches.CustomerListCursor
	var asOf time.Time
	var snapshotWatermark int64
	if payload.Cursor != nil {
		decoded, decodeErr := decodeCursor(*payload.Cursor, filter)
		if decodeErr != nil {
			return nil, badRequest(decodeErr)
		}
		createdAt, parseErr := time.Parse(time.RFC3339Nano, decoded.CreatedAt)
		if parseErr != nil {
			return nil, badRequest(errors.New("cursor is invalid"))
		}
		id, parseErr := uuid.Parse(decoded.ID)
		if parseErr != nil || id.String() != decoded.ID {
			return nil, badRequest(errors.New("cursor is invalid"))
		}
		asOf, parseErr = time.Parse(time.RFC3339Nano, decoded.AsOf)
		if parseErr != nil {
			return nil, badRequest(errors.New("cursor is invalid"))
		}
		cursor = &killswitches.CustomerListCursor{CreatedAt: createdAt, ID: killswitches.PrescriptionID(id.String())}
		snapshotWatermark = decoded.Watermark
	} else {
		snapshot, queryErr := killswitchrepo.New(s.db).CaptureKillswitchCustomerListWatermark(ctx, killswitchrepo.CaptureKillswitchCustomerListWatermarkParams{
			OrganizationID: string(organizationID), DefinitionKey: string(mcptoolexecution.DefinitionKeyMCPToolExecution),
			PrincipalKind: string(mcptoolexecution.PrincipalKindUser), ResourceKind: string(mcptoolexecution.ResourceKindMCPServer),
		})
		if queryErr != nil {
			return nil, mapError(fmt.Errorf("capture killswitch customer list watermark: %w", queryErr))
		}
		if !snapshot.StatusAsOf.Valid {
			return nil, mapError(errors.New("captured killswitch customer list watermark has no status time"))
		}
		asOf = snapshot.StatusAsOf.Time
		snapshotWatermark = snapshot.Watermark
	}

	result, err := s.authorized.ListCustomerPrescriptions(ctx, killswitches.AuthorizedListCustomerPrescriptionsRequest{
		Definition: mcptoolexecution.DefinitionKeyMCPToolExecution, PrincipalKind: mcptoolexecution.PrincipalKindUser,
		ResourceKind: mcptoolexecution.ResourceKindMCPServer, PrincipalKey: principalKey, Status: status,
		Limit: limit, Cursor: cursor, StatusAsOf: asOf, SnapshotWatermark: snapshotWatermark,
	})
	if err != nil {
		return nil, mapError(err)
	}
	items := make([]*gen.KillswitchSummary, len(result.Items))
	for i, item := range result.Items {
		items[i] = summary(string(item.ID), string(item.PrincipalKey), item.Version, string(item.Status), string(item.StartMode), string(item.ResourceScope), stringsFrom(item.SelectedResourceKeys), item.StartsAt, item.ExpiresAt)
	}
	var next *string
	if result.NextCursor != nil {
		encoded, encodeErr := encodeCursor(listCursor{Version: 2, CreatedAt: result.NextCursor.CreatedAt.Format(time.RFC3339Nano), ID: string(result.NextCursor.ID), AsOf: asOf.Format(time.RFC3339Nano), Watermark: snapshotWatermark, Filter: filter})
		if encodeErr != nil {
			return nil, mapError(encodeErr)
		}
		next = &encoded
	}
	return &gen.KillswitchListResult{Items: items, NextCursor: next}, nil
}

func (s *Service) Get(ctx context.Context, payload *gen.GetPayload) (*gen.KillswitchDetail, error) {
	prescription, organizationID, err := s.getCurated(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	dbNow, err := killswitchrepo.New(s.db).GetKillswitchDatabaseTime(ctx)
	if err != nil || !dbNow.Valid {
		return nil, mapError(fmt.Errorf("read database time: %w", err))
	}
	status := currentStatus(prescription, dbNow.Time)
	start := "now"
	if prescription.StartMode == killswitches.StartModeAt {
		start = "scheduled"
	}
	base := summary(string(prescription.ID), string(prescription.PrincipalKey), prescription.Version, status, start, string(prescription.ResourceScope), stringsFrom(prescription.SelectedResourceKeys), prescription.StartsAt, prescription.ExpiresAt)
	id, _ := uuid.Parse(payload.ID)
	rows, err := killswitchrepo.New(s.db).ListCustomerKillswitchHistory(ctx, killswitchrepo.ListCustomerKillswitchHistoryParams{PrescriptionID: id, OrganizationID: string(organizationID), ResultLimit: maxHistoryEvents + 1})
	if err != nil {
		return nil, mapError(fmt.Errorf("list killswitch history: %w", err))
	}
	truncated := len(rows) > int(maxHistoryEvents)
	if truncated {
		rows = rows[:maxHistoryEvents]
	}
	history := make([]*gen.KillswitchHistoryEvent, len(rows))
	for i, row := range rows {
		history[i], err = historyEvent(row)
		if err != nil {
			return nil, mapError(err)
		}
	}
	return &gen.KillswitchDetail{
		ID: base.ID, CapabilityKey: base.CapabilityKey, CapabilityLabel: base.CapabilityLabel, UserID: base.UserID, Version: base.Version, Status: base.Status, Scope: base.Scope, Schedule: base.Schedule,
		ExternalNote: prescription.ExternalNote, InternalNote: prescription.InternalNote, History: history, HistoryTruncated: truncated,
	}, nil
}

func (s *Service) Create(ctx context.Context, payload *gen.CreatePayload) (*gen.KillswitchMutationReceipt, error) {
	if payload.CapabilityKey != CapabilityMCPToolCalls {
		return nil, badRequest(errors.New("capability is not available"))
	}
	desired, err := desired(payload.Scope, payload.Schedule, payload.InternalNote, payload.ExternalNote)
	if err != nil {
		return nil, err
	}
	operationID, err := parseUUID(payload.OperationID)
	if err != nil {
		return nil, err
	}
	result, err := s.authorized.ActivatePrescription(ctx, killswitches.AuthorizedActivatePrescriptionRequest{
		OperationID: operationID, Definition: mcptoolexecution.DefinitionKeyMCPToolExecution, PrincipalKind: mcptoolexecution.PrincipalKindUser,
		PrincipalInput: payload.UserID, ResourceKind: mcptoolexecution.ResourceKindMCPServer, Desired: desired,
	})
	if err != nil {
		return nil, mapError(err)
	}
	return mutationResult(result), nil
}

func (s *Service) Edit(ctx context.Context, payload *gen.EditPayload) (*gen.KillswitchMutationReceipt, error) {
	desiredVersion, err := desired(payload.Scope, payload.Schedule, payload.InternalNote, payload.ExternalNote)
	if err != nil {
		return nil, err
	}
	operationID, err := parseUUID(payload.OperationID)
	if err != nil {
		return nil, err
	}
	if _, _, err = s.getCurated(ctx, payload.ID); err != nil {
		return nil, err
	}
	result, err := s.authorized.ChangePrescription(ctx, killswitches.AuthorizedChangePrescriptionRequest{
		OperationID: operationID, PrescriptionID: killswitches.PrescriptionID(payload.ID), ExpectedVersion: payload.ExpectedVersion, Desired: desiredVersion,
	})
	if err != nil {
		return nil, mapError(err)
	}
	return mutationResult(result), nil
}

func (s *Service) Lift(ctx context.Context, payload *gen.LiftPayload) (*gen.KillswitchLiftResult, error) {
	if _, _, err := s.getCurated(ctx, payload.ID); err != nil {
		return nil, err
	}
	operationID, err := parseUUID(payload.OperationID)
	if err != nil {
		return nil, err
	}
	result, err := s.authorized.DeactivatePrescription(ctx, killswitches.AuthorizedDeactivatePrescriptionRequest{OperationID: operationID, PrescriptionID: killswitches.PrescriptionID(payload.ID), ExpectedVersion: payload.ExpectedVersion})
	if err != nil {
		return nil, mapError(err)
	}
	current, organizationID, err := s.getCurated(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	overlaps, truncated, err := s.overlaps(ctx, organizationID, string(current.PrincipalKey), current.ResourceScope, stringsFrom(current.SelectedResourceKeys), current.StartsAt, current.ExpiresAt, current.ID)
	if err != nil {
		return nil, err
	}
	return &gen.KillswitchLiftResult{Result: mutationResult(result), RemainingOverlaps: overlaps, Truncated: truncated}, nil
}

func (s *Service) PreviewOverlaps(ctx context.Context, payload *gen.PreviewOverlapsPayload) (*gen.KillswitchPreviewOverlapsResult, error) {
	organizationID, err := s.organization(ctx)
	if err != nil {
		return nil, err
	}
	if payload.CapabilityKey != CapabilityMCPToolCalls {
		return nil, badRequest(errors.New("capability is not available"))
	}
	userID, err := s.canonicalUser(organizationID, payload.UserID)
	if err != nil {
		return nil, err
	}
	valid, err := s.user.ValidateCurrentOrganization(ctx, organizationID, killswitches.PrincipalKey(userID))
	if err != nil {
		return nil, mapError(err)
	}
	if !valid {
		return nil, badRequest(errors.New("user is not available"))
	}
	scope, selected, err := s.previewScope(ctx, organizationID, payload.Scope)
	if err != nil {
		return nil, err
	}
	startsAt, endsAt, err := s.previewSchedule(ctx, payload.Schedule)
	if err != nil {
		return nil, err
	}
	exclude := killswitches.PrescriptionID("")
	if payload.ID != nil {
		current, _, getErr := s.getCurated(ctx, *payload.ID)
		if getErr != nil {
			return nil, getErr
		}
		if string(current.PrincipalKey) != userID {
			return nil, badRequest(errors.New("killswitch identity cannot be changed"))
		}
		exclude = current.ID
	}
	overlaps, truncated, err := s.overlaps(ctx, organizationID, userID, scope, selected, startsAt, endsAt, exclude)
	if err != nil {
		return nil, err
	}
	return &gen.KillswitchPreviewOverlapsResult{Overlaps: overlaps, Truncated: truncated}, nil
}

func (s *Service) BatchUserBadges(ctx context.Context, payload *gen.BatchUserBadgesPayload) (*gen.KillswitchBatchUserBadgesResult, error) {
	organizationID, err := s.organization(ctx)
	if err != nil {
		return nil, err
	}
	if len(payload.UserIds) < 1 || len(payload.UserIds) > maxBatchUsers {
		return nil, badRequest(errors.New("user_ids must contain between 1 and 100 items"))
	}
	users := make([]string, 0, len(payload.UserIds))
	for _, input := range payload.UserIds {
		key, canonicalErr := s.canonicalUser(organizationID, input)
		if canonicalErr != nil {
			return nil, canonicalErr
		}
		users = append(users, key)
	}
	slices.Sort(users)
	users = slices.Compact(users)
	rows, err := killswitchrepo.New(s.db).BatchCustomerKillswitchUserBadges(ctx, killswitchrepo.BatchCustomerKillswitchUserBadgesParams{
		OrganizationID: string(organizationID), DefinitionKey: string(mcptoolexecution.DefinitionKeyMCPToolExecution),
		PrincipalKind: string(mcptoolexecution.PrincipalKindUser), ResourceKind: string(mcptoolexecution.ResourceKindMCPServer), UserIds: users,
	})
	if err != nil {
		return nil, mapError(fmt.Errorf("read killswitch user badges: %w", err))
	}
	badges := make([]*gen.KillswitchUserBadge, len(rows))
	for i, row := range rows {
		badges[i] = &gen.KillswitchUserBadge{UserID: row.UserID, Affected: row.AffectedNow || row.Scheduled, AffectedNow: row.AffectedNow, Scheduled: row.Scheduled}
	}
	return &gen.KillswitchBatchUserBadgesResult{Badges: badges}, nil
}

func (s *Service) organization(ctx context.Context) (killswitches.OrganizationID, error) {
	organizationID, err := s.authorized.CustomerOrganization(ctx)
	if err != nil {
		return "", mapError(err)
	}
	return organizationID, nil
}

func (s *Service) getCurated(ctx context.Context, id string) (killswitches.CurrentPrescription, killswitches.OrganizationID, error) {
	if _, err := parseUUID(id); err != nil {
		return killswitches.CurrentPrescription{}, "", err
	}
	prescription, err := s.authorized.GetPrescription(ctx, killswitches.AuthorizedGetPrescriptionRequest{PrescriptionID: killswitches.PrescriptionID(id)})
	if err != nil {
		return killswitches.CurrentPrescription{}, "", mapError(err)
	}
	if prescription.Definition != mcptoolexecution.DefinitionKeyMCPToolExecution || prescription.PrincipalKind != mcptoolexecution.PrincipalKindUser || prescription.ResourceKind != mcptoolexecution.ResourceKindMCPServer {
		return killswitches.CurrentPrescription{}, "", oops.C(oops.CodeNotFound)
	}
	return prescription, prescription.OrganizationID, nil
}

func (s *Service) canonicalUser(organizationID killswitches.OrganizationID, input string) (string, error) {
	result, err := s.user.Canonicalize(organizationID, input)
	if err != nil {
		return "", mapError(err)
	}
	key, supported, err := result.Key()
	if err != nil || !supported {
		return "", badRequest(errors.New("user is not available"))
	}
	return string(key), nil
}

func desired(scope *gen.KillswitchScope, schedule *gen.KillswitchSchedule, internalNote, externalNote string) (killswitches.DesiredVersionInput, error) {
	resourceScope, selected, err := parseScope(scope)
	if err != nil {
		return killswitches.DesiredVersionInput{}, err
	}
	startMode, startsAt, endsAt, err := parseSchedule(schedule)
	if err != nil {
		return killswitches.DesiredVersionInput{}, err
	}
	return killswitches.DesiredVersionInput{ResourceScope: resourceScope, SelectedResourceInputs: selected, StartMode: startMode, StartsAt: startsAt, ExpiresAt: endsAt, InternalNote: internalNote, ExternalNote: externalNote}, nil
}

func parseScope(scope *gen.KillswitchScope) (killswitches.ResourceScope, []string, error) {
	if scope == nil {
		return "", nil, badRequest(errors.New("scope is required"))
	}
	switch scope.Type {
	case "all_servers":
		if len(scope.ServerIds) != 0 {
			return "", nil, badRequest(errors.New("all_servers cannot include server_ids"))
		}
		return killswitches.ResourceScopeAll, nil, nil
	case "selected_servers":
		if len(scope.ServerIds) == 0 {
			return "", nil, badRequest(errors.New("selected_servers requires at least one server_id"))
		}
		return killswitches.ResourceScopeSelected, slices.Clone(scope.ServerIds), nil
	default:
		return "", nil, badRequest(errors.New("scope type is invalid"))
	}
}

func parseSchedule(schedule *gen.KillswitchSchedule) (killswitches.StartMode, *time.Time, *time.Time, error) {
	if schedule == nil {
		return "", nil, nil, badRequest(errors.New("schedule is required"))
	}
	var startMode killswitches.StartMode
	var startsAt *time.Time
	switch schedule.Start {
	case "now":
		if schedule.StartsAt != nil {
			return "", nil, nil, badRequest(errors.New("now cannot include starts_at"))
		}
		startMode = killswitches.StartModeNow
	case "scheduled":
		if schedule.StartsAt == nil {
			return "", nil, nil, badRequest(errors.New("scheduled requires starts_at"))
		}
		parsed, err := time.Parse(time.RFC3339Nano, *schedule.StartsAt)
		if err != nil {
			return "", nil, nil, badRequest(errors.New("starts_at must be an RFC 3339 timestamp"))
		}
		startsAt = &parsed
		startMode = killswitches.StartModeAt
	default:
		return "", nil, nil, badRequest(errors.New("schedule start is invalid"))
	}
	var endsAt *time.Time
	switch schedule.End {
	case "until_lifted":
		if schedule.EndsAt != nil {
			return "", nil, nil, badRequest(errors.New("until_lifted cannot include ends_at"))
		}
	case "bounded":
		if schedule.EndsAt == nil {
			return "", nil, nil, badRequest(errors.New("bounded requires ends_at"))
		}
		parsed, err := time.Parse(time.RFC3339Nano, *schedule.EndsAt)
		if err != nil {
			return "", nil, nil, badRequest(errors.New("ends_at must be an RFC 3339 timestamp"))
		}
		endsAt = &parsed
	default:
		return "", nil, nil, badRequest(errors.New("schedule end is invalid"))
	}
	return startMode, startsAt, endsAt, nil
}

func (s *Service) previewScope(ctx context.Context, organizationID killswitches.OrganizationID, scopeInput *gen.KillswitchScope) (killswitches.ResourceScope, []string, error) {
	scope, selected, err := parseScope(scopeInput)
	if err != nil || scope == killswitches.ResourceScopeAll {
		return scope, selected, err
	}
	canonical := make([]string, 0, len(selected))
	for _, input := range selected {
		result, canonicalErr := s.server.Canonicalize(organizationID, input)
		if canonicalErr != nil {
			return "", nil, mapError(canonicalErr)
		}
		key, supported, keyErr := result.Key()
		if keyErr != nil || !supported {
			return "", nil, badRequest(errors.New("one or more servers are not available"))
		}
		canonical = append(canonical, string(key))
	}
	slices.Sort(canonical)
	canonical = slices.Compact(canonical)
	keys := make([]killswitches.ResourceKey, len(canonical))
	for i, key := range canonical {
		keys[i] = killswitches.ResourceKey(key)
	}
	valid, validateErr := mcptoolexecution.ValidateLiveMCPServersInOrganization(ctx, s.db, organizationID, keys)
	if validateErr != nil {
		return "", nil, mapError(validateErr)
	}
	if !valid {
		return "", nil, badRequest(errors.New("one or more servers are not available"))
	}
	return scope, canonical, nil
}

func (s *Service) previewSchedule(ctx context.Context, schedule *gen.KillswitchSchedule) (time.Time, *time.Time, error) {
	mode, startsAt, endsAt, err := parseSchedule(schedule)
	if err != nil {
		return time.Time{}, nil, err
	}
	dbNow, err := killswitchrepo.New(s.db).GetKillswitchDatabaseTime(ctx)
	if err != nil || !dbNow.Valid {
		return time.Time{}, nil, mapError(fmt.Errorf("read database time: %w", err))
	}
	start := dbNow.Time
	if mode == killswitches.StartModeAt {
		start = *startsAt
		if !start.After(dbNow.Time) {
			return time.Time{}, nil, badRequest(errors.New("scheduled starts_at must be in the future"))
		}
	}
	if endsAt != nil && !endsAt.After(start) {
		return time.Time{}, nil, badRequest(errors.New("ends_at must be after the start"))
	}
	return start, endsAt, nil
}

func (s *Service) overlaps(ctx context.Context, organizationID killswitches.OrganizationID, userID string, scope killswitches.ResourceScope, selected []string, startsAt time.Time, endsAt *time.Time, exclude killswitches.PrescriptionID) ([]*gen.KillswitchOverlap, bool, error) {
	excludeID := uuid.NullUUID{}
	if exclude != "" {
		id, err := uuid.Parse(string(exclude))
		if err != nil {
			return nil, false, badRequest(errors.New("killswitch id is invalid"))
		}
		excludeID = uuid.NullUUID{UUID: id, Valid: true}
	}
	rows, err := killswitchrepo.New(s.db).ListCustomerKillswitchOverlaps(ctx, killswitchrepo.ListCustomerKillswitchOverlapsParams{
		OrganizationID: string(organizationID), DefinitionKey: string(mcptoolexecution.DefinitionKeyMCPToolExecution), PrincipalKind: string(mcptoolexecution.PrincipalKindUser), PrincipalKey: userID, ResourceKind: string(mcptoolexecution.ResourceKindMCPServer),
		ExcludeID: excludeID, DraftStartsAt: conv.ToPGTimestamptz(startsAt), DraftEndsAt: conv.PtrToPGTimestamptz(endsAt), DraftScope: string(scope), DraftSelectedResourceKeys: selected,
	})
	if err != nil {
		return nil, false, mapError(fmt.Errorf("list killswitch overlaps: %w", err))
	}
	truncated := len(rows) > 100
	if truncated {
		rows = rows[:100]
	}
	result := make([]*gen.KillswitchOverlap, len(rows))
	for i, row := range rows {
		result[i] = &gen.KillswitchOverlap{ID: row.ID.String(), Status: gen.KillswitchOverlapStatus(row.CustomerStatus), Scope: outputScope(row.ResourceScope, row.SelectedResourceKeys), Schedule: outputSchedule(row.StartsAt.Time, optionalTime(row.ExpiresAt), row.CustomerStart)}
	}
	return result, truncated, nil
}

func summary(id, userID string, version int64, status, start, scope string, selected []string, startsAt time.Time, endsAt *time.Time) *gen.KillswitchSummary {
	return &gen.KillswitchSummary{ID: id, CapabilityKey: CapabilityMCPToolCalls, CapabilityLabel: capabilityLabel, UserID: userID, Version: version, Status: gen.KillswitchStatus(status), Scope: outputScope(scope, selected), Schedule: outputSchedule(startsAt, endsAt, start)}
}

func outputScope(scope string, selected []string) *gen.KillswitchScope {
	if scope == string(killswitches.ResourceScopeAll) {
		return &gen.KillswitchScope{Type: "all_servers", ServerIds: []string{}}
	}
	return &gen.KillswitchScope{Type: "selected_servers", ServerIds: slices.Clone(selected)}
}

func outputSchedule(startsAt time.Time, endsAt *time.Time, start string) *gen.KillswitchSchedule {
	result := &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}
	if start == "scheduled" {
		formatted := startsAt.Format(time.RFC3339Nano)
		result.Start = "scheduled"
		result.StartsAt = &formatted
	}
	if endsAt != nil {
		formatted := endsAt.Format(time.RFC3339Nano)
		result.End = "bounded"
		result.EndsAt = &formatted
	}
	return result
}

func currentStatus(p killswitches.CurrentPrescription, now time.Time) string {
	if p.State == killswitches.PrescriptionStateInactive {
		return "lifted"
	}
	if p.StartsAt.After(now) {
		return "scheduled"
	}
	if p.ExpiresAt != nil && !now.Before(*p.ExpiresAt) {
		return "expired"
	}
	return "active"
}

func historyEvent(row killswitchrepo.ListCustomerKillswitchHistoryRow) (*gen.KillswitchHistoryEvent, error) {
	var action gen.KillswitchHistoryAction
	switch audit.Action(row.Action) {
	case audit.ActionKillswitchActivate:
		action = "created"
		if row.Operation == string(killswitches.MutationOperationReactivate) {
			action = "restored"
		}
	case audit.ActionKillswitchChange:
		action = "edited"
	case audit.ActionKillswitchDeactivate:
		action = "lifted"
	case audit.ActionKillswitchExpire:
		action = "expired"
	default:
		return nil, fmt.Errorf("unknown killswitch history action %q", row.Action)
	}
	var actorDisplayName *string
	if row.ActorDisplayName.Valid {
		actorDisplayName = &row.ActorDisplayName.String
	}
	var actorUserID *string
	if row.ActorType == "user" {
		actorID := row.ActorID
		actorUserID = &actorID
	}
	return &gen.KillswitchHistoryEvent{Sequence: row.Seq, Version: row.Version, Action: action, Status: gen.KillswitchStatus(row.CustomerStatus), Scope: outputScope(row.ResourceScope, row.SelectedResourceKeys), Schedule: outputSchedule(row.StartsAt.Time, optionalTime(row.ExpiresAt), row.CustomerStart), ExternalNote: row.ExternalNote, InternalNote: row.InternalNote, ActorUserID: actorUserID, ActorDisplayName: actorDisplayName, ChangedAt: row.CreatedAt.Time.Format(time.RFC3339Nano)}, nil
}

func mutationResult(result killswitches.MutationResult) *gen.KillswitchMutationReceipt {
	return &gen.KillswitchMutationReceipt{ID: string(result.PrescriptionID), Version: result.Version, Replayed: result.Replayed}
}

func parseUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, badRequest(errors.New("id must be a canonical UUID"))
	}
	return id, nil
}

func listFilter(userID *killswitches.PrincipalKey, status *killswitches.CustomerStatus) string {
	var userValue, statusValue string
	if userID != nil {
		userValue = string(*userID)
	}
	if status != nil {
		statusValue = string(*status)
	}
	return fmt.Sprintf("%t:%s|%t:%s", userID != nil, userValue, status != nil, statusValue)
}

func encodeCursor(cursor listCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode killswitch cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeCursor(value, filter string) (listCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) > 1024 {
		return listCursor{}, errors.New("cursor is invalid")
	}
	var cursor listCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Version != 2 || cursor.Watermark < 0 || cursor.Filter != filter || cursor.CreatedAt == "" || cursor.ID == "" || cursor.AsOf == "" {
		return listCursor{}, errors.New("cursor is invalid")
	}
	return cursor, nil
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func stringsFrom[T ~string](values []T) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*oops.ShareableError](err); ok {
		return err
	}
	switch {
	case errors.Is(err, killswitches.ErrInvalidArgument), errors.Is(err, killswitches.ErrInvalidReference), errors.Is(err, killswitches.ErrInvalidTransition):
		return badRequest(err)
	case errors.Is(err, killswitches.ErrPrescriptionNotFound):
		return oops.E(oops.CodeNotFound, err, "resource not found")
	case errors.Is(err, killswitches.ErrOperationConflict):
		return &gen.KillswitchConflict{Name: "operation_conflict", Message: "the operation ID was already used for a different request"}
	case errors.Is(err, killswitches.ErrVersionConflict):
		return &gen.KillswitchConflict{Name: "version_conflict", Message: "the killswitch changed after the supplied version"}
	case errors.Is(err, killswitches.ErrOperationUnavailable):
		return oops.E(oops.CodeUnavailable, err, "service is temporarily unavailable")
	default:
		return oops.E(oops.CodeUnexpected, err, "unexpected error occurred")
	}
}

func badRequest(err error) error {
	return oops.E(oops.CodeBadRequest, err, "request is invalid")
}
