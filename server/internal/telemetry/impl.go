package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	telem_srv "github.com/speakeasy-api/gram/server/gen/http/telemetry/server"
	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgsRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
	"golang.org/x/sync/errgroup"
)

type Service struct {
	auth                  *auth.Auth
	db                    *pgxpool.Pool
	chatRepo              *chatRepo.Queries
	hooksRepo             *hooksRepo.Queries
	orgsRepo              *orgsRepo.Queries
	projectsRepo          *projectsRepo.Queries
	chConn                clickhouse.Conn
	chRepo                *repo.Queries
	logger                *slog.Logger
	tracer                trace.Tracer
	posthog               PosthogClient
	chatSessions          *chatsessions.Manager
	logsEnabled           FeatureChecker
	sessionCaptureEnabled FeatureChecker
	authz                 *authz.Engine
}

var _ telem_gen.Service = (*Service)(nil)
var _ telem_gen.Auther = (*Service)(nil)

// NewService creates a telemetry service.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	chConn clickhouse.Conn,
	sessions *sessions.Manager,
	chatSessions *chatsessions.Manager,
	logsEnabled FeatureChecker,
	sessionCaptureEnabled FeatureChecker,
	posthogClient PosthogClient,
	authzEngine *authz.Engine,
) *Service {
	logger = logger.With(attr.SlogComponent("telemetry"))
	chRepo := repo.New(chConn)

	// The sessions and chatSessions parameters may be nil for callers that only need
	// telemetry emission (e.g., Temporal workers using CreateLog). When nil, the HTTP
	// API auth methods (APIKeyAuth, JWTAuth) will return unauthorized errors.
	var a *auth.Auth
	if sessions != nil {
		a = auth.New(logger, db, sessions, authzEngine)
	}

	return &Service{
		auth:                  a,
		db:                    db,
		chatRepo:              chatRepo.New(db),
		hooksRepo:             hooksRepo.New(db),
		orgsRepo:              orgsRepo.New(db),
		projectsRepo:          projectsRepo.New(db),
		chConn:                chConn,
		chRepo:                chRepo,
		logger:                logger,
		logsEnabled:           logsEnabled,
		sessionCaptureEnabled: sessionCaptureEnabled,
		tracer:                tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry"),
		posthog:               posthogClient,
		chatSessions:          chatSessions,
		authz:                 authzEngine,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := telem_gen.NewEndpoints(service)

	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))

	telem_srv.Mount(
		mux,
		telem_srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	if s.auth == nil {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "auth not configured")
	}
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) JWTAuth(ctx context.Context, token string, schema *security.JWTScheme) (context.Context, error) {
	if s.chatSessions == nil {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "chat sessions not configured")
	}
	return s.chatSessions.Authorize(ctx, token)
}

// CheckLogsEnabled returns whether logs are enabled for the given organization.
// Returns an error suitable for returning from API endpoints if logs are disabled.
func (s *Service) CheckLogsEnabled(ctx context.Context, organizationID string) error {
	enabled, err := s.logsEnabled(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !enabled {
		return oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}
	return nil
}

// SearchLogs retrieves unified telemetry logs with pagination.
func (s *Service) SearchLogs(ctx context.Context, payload *telem_gen.SearchLogsPayload) (res *telem_gen.SearchLogsResult, err error) {
	// Prefer top-level from/to; fall back to deprecated filter fields.
	from := payload.From
	to := payload.To
	if from == nil && payload.Filter != nil {
		from = payload.Filter.From
	}
	if to == nil && payload.Filter != nil {
		to = payload.Filter.To
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, from, to)
	if err != nil {
		return nil, err
	}

	// Extract SearchLogs-specific filter fields (deprecated filter path).
	var traceID, deploymentID, functionID, severityText, httpRoute, httpMethod, serviceName, gramChatID, userID, externalUserID, eventSource string
	var httpStatusCode int32
	var gramURNs []string
	if payload.Filter != nil {
		// Handle both gram_urn (single) and gram_urns (array) for backwards compatibility
		gramURNs = resolveGramURNs(payload.Filter.GramUrn, payload.Filter.GramUrns)
		traceID = conv.PtrValOr(payload.Filter.TraceID, "")
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		functionID = conv.PtrValOr(payload.Filter.FunctionID, "")
		severityText = conv.PtrValOr(payload.Filter.SeverityText, "")
		httpStatusCode = conv.PtrValOr(payload.Filter.HTTPStatusCode, 0)
		httpRoute = conv.PtrValOr(payload.Filter.HTTPRoute, "")
		httpMethod = conv.PtrValOr(payload.Filter.HTTPMethod, "")
		serviceName = conv.PtrValOr(payload.Filter.ServiceName, "")
		gramChatID = conv.PtrValOr(payload.Filter.GramChatID, "")
		userID = conv.PtrValOr(payload.Filter.UserID, "")
		externalUserID = conv.PtrValOr(payload.Filter.ExternalUserID, "")
		eventSource = conv.PtrValOr(payload.Filter.EventSource, "")
	}

	// New top-level filters supersede filter.attribute_filters.
	attributeFilters := toRepoAttributeFilters(payload.Filters)

	// Query with limit+1 to detect if there are more results
	items, err := s.chRepo.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          params.projectID,
		TimeStart:              params.timeStart,
		TimeEnd:                params.timeEnd,
		GramURNs:               gramURNs,
		TraceID:                traceID,
		GramDeploymentID:       deploymentID,
		GramFunctionID:         functionID,
		SeverityText:           severityText,
		HTTPResponseStatusCode: httpStatusCode,
		HTTPRoute:              httpRoute,
		HTTPRequestMethod:      httpMethod,
		ServiceName:            serviceName,
		GramChatID:             gramChatID,
		UserID:                 userID,
		ExternalUserID:         externalUserID,
		EventSource:            eventSource,
		AttributeFilters:       attributeFilters,
		SortOrder:              params.sortOrder,
		Cursor:                 params.cursor,
		Limit:                  params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing telemetry logs")
	}

	// Compute next cursor using limit+1 pattern
	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = new(items[params.limit-1].ID)
		items = items[:params.limit]
	}

	// Convert repo models to Goa types
	telemetryLogs := make([]*telem_gen.TelemetryLogRecord, len(items))
	for i, log := range items {
		record, err := toTelemetryLogPayload(log)
		if err != nil {
			return nil, err
		}
		telemetryLogs[i] = record
	}

	return &telem_gen.SearchLogsResult{
		Logs:       telemetryLogs,
		NextCursor: nextCursor,
	}, nil
}

// SearchToolCalls retrieves tool call summaries with pagination.
func (s *Service) SearchToolCalls(ctx context.Context, payload *telem_gen.SearchToolCallsPayload) (res *telem_gen.SearchToolCallsResult, err error) {
	var from, to *string
	if payload.Filter != nil {
		from, to = payload.Filter.From, payload.Filter.To
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, from, to)
	if err != nil {
		return nil, err
	}

	// Extract SearchToolCalls-specific filter fields
	var deploymentID, functionID, gramURN, eventSource string
	if payload.Filter != nil {
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		functionID = conv.PtrValOr(payload.Filter.FunctionID, "")
		gramURN = conv.PtrValOr(payload.Filter.GramUrn, "")
		eventSource = conv.PtrValOr(payload.Filter.EventSource, "")
	}

	// Query with limit+1 to detect if there are more results
	items, err := s.chRepo.ListToolTraces(ctx, repo.ListToolTracesParams{
		GramProjectID:    params.projectID,
		TimeStart:        params.timeStart,
		TimeEnd:          params.timeEnd,
		GramDeploymentID: deploymentID,
		GramFunctionID:   functionID,
		GramURN:          gramURN,
		EventSource:      eventSource,
		SortOrder:        params.sortOrder,
		Cursor:           params.cursor,
		Limit:            params.limit + 1,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "error listing tool traces", attr.SlogError(err))
		return nil, oops.E(oops.CodeUnexpected, err, "error listing traces")
	}

	// Compute next cursor using limit+1 pattern
	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = &items[params.limit-1].TraceID
		items = items[:params.limit]
	}

	// Convert repo models to Goa types
	toolCalls := make([]*telem_gen.ToolCallSummary, len(items))
	for i, item := range items {
		toolCalls[i] = &telem_gen.ToolCallSummary{
			TraceID:           item.TraceID,
			StartTimeUnixNano: strconv.FormatInt(item.StartTimeUnixNano, 10),
			LogCount:          item.LogCount,
			HTTPStatusCode:    item.HTTPStatusCode,
			GramUrn:           item.GramURN,
			ToolName:          item.ToolName,
			ToolSource:        item.ToolSource,
			EventSource:       item.EventSource,
		}
	}

	return &telem_gen.SearchToolCallsResult{
		ToolCalls:  toolCalls,
		NextCursor: nextCursor,
	}, nil
}

// SearchChats retrieves chat session summaries with pagination.
func (s *Service) SearchChats(ctx context.Context, payload *telem_gen.SearchChatsPayload) (res *telem_gen.SearchChatsResult, err error) {
	var from, to *string
	if payload.Filter != nil {
		from, to = payload.Filter.From, payload.Filter.To
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, from, to)
	if err != nil {
		return nil, err
	}

	var deploymentID, gramURN, userID, externalUserID string
	if payload.Filter != nil {
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		gramURN = conv.PtrValOr(payload.Filter.GramUrn, "")
		userID = conv.PtrValOr(payload.Filter.UserID, "")
		externalUserID = conv.PtrValOr(payload.Filter.ExternalUserID, "")
	}

	items, err := s.chRepo.ListChats(ctx, repo.ListChatsParams{
		GramProjectID:    params.projectID,
		TimeStart:        params.timeStart,
		TimeEnd:          params.timeEnd,
		GramDeploymentID: deploymentID,
		GramURN:          gramURN,
		UserID:           userID,
		ExternalUserID:   externalUserID,
		SortOrder:        params.sortOrder,
		Cursor:           params.cursor,
		Limit:            params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing chats")
	}

	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = &items[params.limit-1].GramChatID
		items = items[:params.limit]
	}

	chats := make([]*telem_gen.ChatSummary, len(items))
	for i, item := range items {
		chats[i] = &telem_gen.ChatSummary{
			GramChatID:        item.GramChatID,
			StartTimeUnixNano: strconv.FormatInt(item.StartTimeUnixNano, 10),
			EndTimeUnixNano:   strconv.FormatInt(item.EndTimeUnixNano, 10),
			LogCount:          item.LogCount,
			ToolCallCount:     item.ToolCallCount,
			MessageCount:      item.MessageCount,
			DurationSeconds:   sanitizeFloat64(item.DurationSeconds),
			Status:            item.Status,
			UserID:            item.UserID,
			Model:             item.Model,
			TotalInputTokens:  item.TotalInputTokens,
			TotalOutputTokens: item.TotalOutputTokens,
			TotalTokens:       item.TotalTokens,
		}
	}

	return &telem_gen.SearchChatsResult{
		Chats:      chats,
		NextCursor: nextCursor,
	}, nil
}

// SearchUsers retrieves user usage summaries grouped by user_id or external_user_id.
// When group_by=role, it aggregates per-user costs by RBAC role.
func (s *Service) SearchUsers(ctx context.Context, payload *telem_gen.SearchUsersPayload) (res *telem_gen.SearchUsersResult, err error) {
	if payload.GroupBy == "role" {
		return s.searchUsersByRole(ctx, payload)
	}
	return s.searchUsersByEmployee(ctx, payload)
}

func (s *Service) searchUsersByEmployee(ctx context.Context, payload *telem_gen.SearchUsersPayload) (*telem_gen.SearchUsersResult, error) {
	// Filter is required by the Goa design, but direct callers (e.g. platform
	// tools) bypass transport validation and may pass a nil filter. Normalize to
	// an empty filter to avoid a nil pointer dereference.
	filter := payload.Filter
	if filter == nil {
		filter = &telem_gen.SearchUsersFilter{
			From:          "",
			To:            "",
			DeploymentID:  nil,
			UserIds:       nil,
			EventSource:   nil,
			HookSource:    nil,
			AccountType:   nil,
			ExternalOrgID: nil,
		}
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, &filter.From, &filter.To)
	if err != nil {
		return nil, err
	}

	deploymentID := conv.PtrValOr(filter.DeploymentID, "")

	groupBy := "user_id"
	if payload.UserType == "external" {
		groupBy = "external_user_id"
	}

	items, err := s.chRepo.SearchUsers(ctx, repo.SearchUsersParams{
		GramProjectID:    params.projectID,
		TimeStart:        params.timeStart,
		TimeEnd:          params.timeEnd,
		GramDeploymentID: deploymentID,
		EventSource:      conv.PtrValOr(filter.EventSource, ""),
		HookSource:       conv.PtrValOr(filter.HookSource, ""),
		AccountType:      conv.PtrValOr(filter.AccountType, ""),
		ExternalOrgID:    conv.PtrValOr(filter.ExternalOrgID, ""),
		GroupBy:          groupBy,
		UserIDs:          filter.UserIds,
		SortOrder:        params.sortOrder,
		Cursor:           params.cursor,
		Limit:            params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error searching users")
	}

	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = &items[params.limit-1].UserID
		items = items[:params.limit]
	}

	users := make([]*telem_gen.UserSummary, len(items))
	rawUserIDsByKey := make(map[string][]string, len(items))
	for i, item := range items {
		rawUserIDsByKey[item.UserID] = item.RawUserIDs
		// Build per-tool breakdown from the 3 maps
		tools := make([]*telem_gen.ToolUsage, 0, len(item.ToolCounts))
		for urn, count := range item.ToolCounts {
			tools = append(tools, &telem_gen.ToolUsage{
				Urn:          urn,
				Count:        int64(count),                       //nolint:gosec // Bounded count
				SuccessCount: int64(item.ToolSuccessCounts[urn]), //nolint:gosec // Bounded count
				FailureCount: int64(item.ToolFailureCounts[urn]), //nolint:gosec // Bounded count
			})
		}

		hookSources := make([]*telem_gen.HookSourceUsage, 0, len(item.HookSourceCounts))
		for hookSource, count := range item.HookSourceCounts {
			hookSources = append(hookSources, &telem_gen.HookSourceUsage{
				Source:     hookSource,
				EventCount: int64(count), //nolint:gosec // Bounded count
			})
		}
		sort.Slice(hookSources, func(i, j int) bool {
			return hookSources[i].Source < hookSources[j].Source
		})

		//nolint:gosec // Values are bounded counts that won't overflow int64
		users[i] = &telem_gen.UserSummary{
			UserID:                   item.UserID,
			UserEmail:                item.UserEmail,
			FirstSeenUnixNano:        strconv.FormatInt(item.FirstSeenUnixNano, 10),
			LastSeenUnixNano:         strconv.FormatInt(item.LastSeenUnixNano, 10),
			TotalChats:               int64(item.TotalChats),
			TotalChatRequests:        int64(item.TotalChatRequests),
			TotalInputTokens:         item.TotalInputTokens,
			TotalOutputTokens:        item.TotalOutputTokens,
			TotalTokens:              item.TotalTokens,
			CacheReadInputTokens:     item.CacheReadInputTokens,
			CacheCreationInputTokens: item.CacheCreationInputTokens,
			AvgTokensPerRequest:      sanitizeFloat64(item.AvgTokensPerReq),
			TotalCost:                sanitizeFloat64(item.TotalCost),
			TotalToolCalls:           int64(item.TotalToolCalls),
			ToolCallSuccess:          int64(item.ToolCallSuccess),
			ToolCallFailure:          int64(item.ToolCallFailure),
			Tools:                    tools,
			HookSources:              hookSources,
			AccountTypes:             item.AccountTypes,
			Accounts:                 nil,
		}
	}

	// Attach each user's linked AI accounts (team + personal, across providers)
	// from the user_accounts directory. Only meaningful when grouping by internal
	// user_id — external ids don't map to a directory owner.
	if payload.UserType != "external" {
		if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
			s.attachUserAccounts(ctx, authCtx.ActiveOrganizationID, users, rawUserIDsByKey)
		}
	}

	return &telem_gen.SearchUsersResult{
		Users:      users,
		Roles:      nil,
		NextCursor: nextCursor,
	}, nil
}

// resolveSummaryOwnerIDs maps summary group keys to the Gram user id each key
// authoritatively identifies. SearchUsers keys internal summaries email-first,
// so an email-shaped key is resolved through the org's user directory; a
// non-email key is already a raw gram user id and identifies itself. Keys that
// resolve to no connected user are absent from the result. Best-effort: on a
// directory lookup failure only the id-shaped keys are returned.
func (s *Service) resolveSummaryOwnerIDs(ctx context.Context, orgID string, keys []string) map[string]string {
	ownerByKey := make(map[string]string, len(keys))
	emails := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if !strings.Contains(key, "@") {
			ownerByKey[key] = key
			continue
		}
		email := conv.NormalizeEmail(key)
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}
	if len(emails) == 0 {
		return ownerByKey
	}

	rows, err := usersRepo.New(s.db).GetConnectedUsersByEmails(ctx, usersRepo.GetConnectedUsersByEmailsParams{
		Emails:         emails,
		OrganizationID: orgID,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to resolve summary emails to org users", attr.SlogError(err))
		return ownerByKey
	}
	idByEmail := make(map[string]string, len(rows))
	for _, row := range rows {
		idByEmail[conv.NormalizeEmail(row.Email)] = row.ID
	}
	for _, key := range keys {
		if !strings.Contains(key, "@") {
			continue
		}
		if id, ok := idByEmail[conv.NormalizeEmail(key)]; ok {
			ownerByKey[key] = id
		}
	}
	return ownerByKey
}

// attachUserAccounts populates UserSummary.Accounts from the user_accounts
// directory. Ownership comes from the directory itself, never from telemetry
// row identity: an account row attaches to the summary whose resolved owner
// (email group key resolved through the user directory, or an id-shaped group
// key) matches the row's user_id, else to the summary keyed by the account's
// own email (its own usage rows, e.g. an unprovisioned member or a personal
// identity). The raw telemetry user_ids folded into a summary (rawUserIDsByKey)
// only widen the candidate fetch — attaching through them would let a stray row
// pairing one person's email with another person's user id hand the second
// person's accounts to the first summary (DNO-509). Best-effort: a lookup
// failure leaves accounts empty rather than failing the listing.
func (s *Service) attachUserAccounts(ctx context.Context, orgID string, users []*telem_gen.UserSummary, rawUserIDsByKey map[string][]string) {
	if len(users) == 0 {
		return
	}

	keys := make([]string, 0, len(users))
	for _, u := range users {
		keys = append(keys, u.UserID)
	}
	ownerByKey := s.resolveSummaryOwnerIDs(ctx, orgID, keys)

	summaryByOwner := make(map[string]*telem_gen.UserSummary, len(users))
	summaryByEmailKey := make(map[string]*telem_gen.UserSummary, len(users))
	userIDs := make([]string, 0, len(users))
	seenIDs := make(map[string]struct{}, len(users))
	for _, u := range users {
		// Group keys are distinct, but two case-variant email keys can resolve to
		// the same user; the earlier summary in list order keeps the claim.
		if owner := ownerByKey[u.UserID]; owner != "" {
			if _, claimed := summaryByOwner[owner]; !claimed {
				summaryByOwner[owner] = u
			}
		}
		if strings.Contains(u.UserID, "@") {
			emailKey := conv.NormalizeEmail(u.UserID)
			if _, claimed := summaryByEmailKey[emailKey]; !claimed {
				summaryByEmailKey[emailKey] = u
			}
		}
		for _, id := range append([]string{ownerByKey[u.UserID]}, rawUserIDsByKey[u.UserID]...) {
			if id == "" {
				continue
			}
			if _, ok := seenIDs[id]; ok {
				continue
			}
			seenIDs[id] = struct{}{}
			userIDs = append(userIDs, id)
		}
	}
	if len(userIDs) == 0 {
		return
	}

	rows, err := s.hooksRepo.ListUserAccountsByUsers(ctx, hooksRepo.ListUserAccountsByUsersParams{
		OrganizationID: orgID,
		UserIds:        userIDs,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to load user accounts for employees list", attr.SlogError(err))
		return
	}

	for _, row := range rows {
		if !row.UserID.Valid || row.UserID.String == "" {
			continue
		}
		summary := summaryByOwner[row.UserID.String]
		if summary == nil {
			if email := conv.NormalizeEmail(conv.FromPGTextOrEmpty[string](row.Email)); email != "" {
				summary = summaryByEmailKey[email]
			}
		}
		if summary == nil {
			continue
		}
		var lastSeen *string
		if row.LastSeenAt.Valid {
			ns := strconv.FormatInt(row.LastSeenAt.Time.UnixNano(), 10)
			lastSeen = &ns
		}
		idStr := row.ID.String()
		summary.Accounts = append(summary.Accounts, &telem_gen.UserAccount{
			ID:               &idStr,
			Provider:         row.Provider,
			Email:            conv.FromPGText[string](row.Email),
			AccountType:      conv.FromPGText[string](row.AccountType),
			ExternalOrgID:    conv.FromPGText[string](row.ExternalOrgID),
			LastSeenUnixNano: lastSeen,
		})
	}
}

// searchUsersByRole fetches all per-user costs from ClickHouse, joins with role
// assignments from Postgres, and returns aggregates grouped by role.
func (s *Service) searchUsersByRole(ctx context.Context, payload *telem_gen.SearchUsersPayload) (*telem_gen.SearchUsersResult, error) {
	// Filter is required by the Goa design, but direct callers (e.g. platform
	// tools) bypass transport validation and may pass a nil filter. Normalize to
	// an empty filter to avoid a nil pointer dereference.
	filter := payload.Filter
	if filter == nil {
		filter = &telem_gen.SearchUsersFilter{
			From:          "",
			To:            "",
			DeploymentID:  nil,
			UserIds:       nil,
			EventSource:   nil,
			HookSource:    nil,
			AccountType:   nil,
			ExternalOrgID: nil,
		}
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, &filter.From, &filter.To)
	if err != nil {
		return nil, err
	}

	deploymentID := conv.PtrValOr(filter.DeploymentID, "")

	// Fetch per-user costs from ClickHouse and role assignments from Postgres
	// concurrently — the two queries are independent.
	var items []repo.UserSummary
	var assignments []orgsRepo.ListActiveRoleAssignmentsByOrganizationRow

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var fetchErr error
		items, fetchErr = s.chRepo.SearchUsers(egCtx, repo.SearchUsersParams{
			GramProjectID:    params.projectID,
			TimeStart:        params.timeStart,
			TimeEnd:          params.timeEnd,
			GramDeploymentID: deploymentID,
			EventSource:      conv.PtrValOr(filter.EventSource, ""),
			HookSource:       conv.PtrValOr(filter.HookSource, ""),
			AccountType:      conv.PtrValOr(filter.AccountType, ""),
			ExternalOrgID:    conv.PtrValOr(filter.ExternalOrgID, ""),
			GroupBy:          "user_id",
			UserIDs:          filter.UserIds,
			SortOrder:        "desc",
			Cursor:           "",
			Limit:            10001, // Upper bound; orgs rarely have >10k users
		})
		if fetchErr != nil {
			return oops.E(oops.CodeUnexpected, fetchErr, "error searching users for role aggregation")
		}
		return nil
	})
	eg.Go(func() error {
		var fetchErr error
		assignments, fetchErr = s.orgsRepo.ListActiveRoleAssignmentsByOrganization(egCtx, params.organizationID)
		if fetchErr != nil {
			return oops.E(oops.CodeUnexpected, fetchErr, "error fetching role assignments")
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching role usage data")
	}

	// Build user_id → (role_id, role_name) map. A user may have multiple role
	// assignments; we take the first one encountered.
	type roleInfo struct {
		id   string
		name string
	}
	userToRole := make(map[string]roleInfo, len(assignments))
	for _, a := range assignments {
		if a.UserID.Valid {
			if _, exists := userToRole[a.UserID.String]; !exists {
				userToRole[a.UserID.String] = roleInfo{
					id:   roleIDFromURN(a.RoleUrn),
					name: a.RoleName,
				}
			}
		}
	}

	// Role assignments are keyed by raw gram user id while summaries are keyed
	// email-first, so resolve each summary's owner through the user directory.
	// The raw ids folded into a summary are only a fallback for keys that do not
	// resolve: a stray row pairing one person's email with another person's user
	// id would otherwise bucket the summary under the wrong role (DNO-509).
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.UserID)
	}
	ownerByKey := s.resolveSummaryOwnerIDs(ctx, params.organizationID, keys)

	// Single pass: aggregate per-user costs by role and build the response.
	type roleAgg struct {
		summary *telem_gen.RoleSummary
	}
	aggByRole := make(map[string]*roleAgg, len(userToRole))

	const unassignedRoleID = "unassigned"
	for _, item := range items {
		ri := roleInfo{id: unassignedRoleID, name: "Unassigned"}
		if owner, ok := ownerByKey[item.UserID]; ok {
			// A resolved member without an assignment stays Unassigned rather than
			// borrowing a role through raw telemetry ids.
			if r, ok := userToRole[owner]; ok {
				ri = r
			}
		} else {
			for _, rawID := range item.RawUserIDs {
				if r, ok := userToRole[rawID]; ok {
					ri = r
					break
				}
			}
		}
		agg, exists := aggByRole[ri.id]
		if !exists {
			agg = &roleAgg{summary: &telem_gen.RoleSummary{
				RoleID:            ri.id,
				RoleName:          ri.name,
				UserCount:         0,
				TotalCost:         0,
				CostPerUser:       0,
				TotalInputTokens:  0,
				TotalOutputTokens: 0,
				TotalTokens:       0,
				TotalChats:        0,
			}}
			aggByRole[ri.id] = agg
		}
		s := agg.summary
		s.UserCount++
		s.TotalCost += item.TotalCost
		s.TotalInputTokens += item.TotalInputTokens
		s.TotalOutputTokens += item.TotalOutputTokens
		s.TotalTokens += item.TotalTokens
		s.TotalChats += int64(item.TotalChats) //nolint:gosec // Bounded count
	}

	roles := make([]*telem_gen.RoleSummary, 0, len(aggByRole))
	for _, agg := range aggByRole {
		rs := agg.summary
		if rs.UserCount > 0 {
			rs.CostPerUser = sanitizeFloat64(rs.TotalCost / float64(rs.UserCount))
		}
		rs.TotalCost = sanitizeFloat64(rs.TotalCost)
		roles = append(roles, rs)
	}

	// Sort by total cost descending.
	sort.Slice(roles, func(i, j int) bool {
		return roles[i].TotalCost > roles[j].TotalCost
	})

	return &telem_gen.SearchUsersResult{
		Users:      []*telem_gen.UserSummary{},
		Roles:      roles,
		NextCursor: nil,
	}, nil
}

// roleIDFromURN extracts the role ID from a role URN like "role:organization:<id>"
// or "role:global:<id>".
func roleIDFromURN(urn string) string {
	parts := strings.SplitN(urn, ":", 3)
	if len(parts) == 3 {
		return parts[2]
	}
	return urn
}

// GetProjectMetricsSummary retrieves aggregated metrics for an entire project.
func (s *Service) GetProjectMetricsSummary(ctx context.Context, payload *telem_gen.GetProjectMetricsSummaryPayload) (res *telem_gen.GetMetricsSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	metrics, err := s.chRepo.GetMetricsSummary(ctx, repo.GetMetricsSummaryParams{
		GramProjectID: authCtx.ProjectID.String(),
		TimeStart:     timeStart,
		TimeEnd:       timeEnd,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving project metrics")
	}

	return buildMetricsSummaryResult(*metrics), nil
}

// buildMetricsSummaryResult converts repo metrics to the API response format.
func buildMetricsSummaryResult(metrics repo.MetricsSummaryRow) *telem_gen.GetMetricsSummaryResult {
	// Convert models map to ModelUsage slice
	models := make([]*telem_gen.ModelUsage, 0, len(metrics.Models))
	for name, count := range metrics.Models {
		models = append(models, &telem_gen.ModelUsage{
			Name:  name,
			Count: int64(count), //nolint:gosec // Bounded count
		})
	}

	// Convert tool maps to ToolUsage slice
	tools := make([]*telem_gen.ToolUsage, 0, len(metrics.ToolCounts))
	for urn, count := range metrics.ToolCounts {
		tools = append(tools, &telem_gen.ToolUsage{
			Urn:          urn,
			Count:        int64(count),                          //nolint:gosec // Bounded count
			SuccessCount: int64(metrics.ToolSuccessCounts[urn]), //nolint:gosec // Bounded count
			FailureCount: int64(metrics.ToolFailureCounts[urn]), //nolint:gosec // Bounded count
		})
	}

	//nolint:gosec // Values are bounded counts that won't overflow int64
	return &telem_gen.GetMetricsSummaryResult{
		Metrics: &telem_gen.ProjectSummary{
			FirstSeenUnixNano:        strconv.FormatInt(metrics.FirstSeenUnixNano, 10),
			LastSeenUnixNano:         strconv.FormatInt(metrics.LastSeenUnixNano, 10),
			TotalInputTokens:         metrics.TotalInputTokens,
			TotalOutputTokens:        metrics.TotalOutputTokens,
			TotalTokens:              metrics.TotalTokens,
			CacheReadInputTokens:     metrics.CacheReadInputTokens,
			CacheCreationInputTokens: metrics.CacheCreationInputTokens,
			AvgTokensPerRequest:      sanitizeFloat64(metrics.AvgTokensPerReq),
			TotalCost:                sanitizeFloat64(metrics.TotalCost),
			TotalChatRequests:        int64(metrics.TotalChatRequests),
			AvgChatDurationMs:        sanitizeFloat64(metrics.AvgChatDurationMs),
			FinishReasonStop:         int64(metrics.FinishReasonStop),
			FinishReasonToolCalls:    int64(metrics.FinishReasonToolCalls),
			TotalToolCalls:           int64(metrics.TotalToolCalls),
			ToolCallSuccess:          int64(metrics.ToolCallSuccess),
			ToolCallFailure:          int64(metrics.ToolCallFailure),
			AvgToolDurationMs:        sanitizeFloat64(metrics.AvgToolDurationMs),
			TotalChats:               int64(metrics.TotalChats),
			DistinctModels:           int64(metrics.DistinctModels),
			DistinctProviders:        int64(metrics.DistinctProviders),
			Models:                   models,
			Tools:                    tools,
			ChatResolutionSuccess:    int64(metrics.ChatResolutionSuccess),
			ChatResolutionFailure:    int64(metrics.ChatResolutionFailure),
			ChatResolutionPartial:    int64(metrics.ChatResolutionPartial),
			ChatResolutionAbandoned:  int64(metrics.ChatResolutionAbandoned),
			AvgChatResolutionScore:   sanitizeFloat64(metrics.AvgChatResolutionScore),
		},
	}
}

// GetUserMetricsSummary retrieves aggregated metrics for a specific user.
func (s *Service) GetUserMetricsSummary(ctx context.Context, payload *telem_gen.GetUserMetricsSummaryPayload) (res *telem_gen.GetUserMetricsSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	// Validate that exactly one of user_id or external_user_id is provided
	userID := conv.PtrValOr(payload.UserID, "")
	externalUserID := conv.PtrValOr(payload.ExternalUserID, "")
	if userID == "" && externalUserID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "either user_id or external_user_id is required")
	}
	if userID != "" && externalUserID != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "only one of user_id or external_user_id can be provided")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	metrics, err := s.chRepo.GetUserMetricsSummary(ctx, repo.GetUserMetricsSummaryParams{
		GramProjectID:  authCtx.ProjectID.String(),
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		UserID:         userID,
		ExternalUserID: externalUserID,
		EventSource:    conv.PtrValOr(payload.EventSource, ""),
		HookSource:     conv.PtrValOr(payload.HookSource, ""),
		AccountType:    conv.PtrValOr(payload.AccountType, ""),
		ExternalOrgID:  conv.PtrValOr(payload.ExternalOrgID, ""),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving user metrics")
	}

	// Reuse the same helper as project metrics since the response format is identical
	projectResult := buildMetricsSummaryResult(*metrics)
	return &telem_gen.GetUserMetricsSummaryResult{
		Metrics: projectResult.Metrics,
	}, nil
}

// GetEmployeeDataFlowGraph retrieves an employee's MCP data-flow graph.
func (s *Service) GetEmployeeDataFlowGraph(ctx context.Context, payload *telem_gen.GetEmployeeDataFlowGraphPayload) (res *telem_gen.GetEmployeeDataFlowGraphResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	userID := conv.PtrValOr(payload.UserID, "")
	externalUserID := conv.PtrValOr(payload.ExternalUserID, "")
	if userID == "" && externalUserID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "either user_id or external_user_id is required")
	}
	if userID != "" && externalUserID != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "only one of user_id or external_user_id can be provided")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	rows, err := s.chRepo.GetEmployeeDataFlowGraph(ctx, repo.GetEmployeeDataFlowGraphParams{
		GramProjectID:  authCtx.ProjectID.String(),
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		UserID:         userID,
		ExternalUserID: externalUserID,
		AccountType:    conv.PtrValOr(payload.AccountType, ""),
		ExternalOrgID:  conv.PtrValOr(payload.ExternalOrgID, ""),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving employee data flow graph")
	}

	nodes, edges := buildEmployeeDataFlowGraph(rows)
	return &telem_gen.GetEmployeeDataFlowGraphResult{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

type employeeGraphTupleNode struct {
	tier        string
	label       string
	serverClass string
}

type employeeGraphNodeAccumulator struct {
	node       *telem_gen.EmployeeDataFlowNode
	totalCalls uint64
}

type employeeGraphEdgeAccumulator struct {
	edge         *telem_gen.EmployeeDataFlowEdge
	callCount    uint64
	successCount uint64
	failureCount uint64
}

type employeeGraphEdgeKey struct {
	sourceID string
	targetID string
}

var employeeDataFlowTierOrder = map[string]int{
	"origin": 0,
	"client": 1,
	"server": 2,
	"tool":   3,
}

func buildEmployeeDataFlowGraph(rows []repo.EmployeeDataFlowRow) ([]*telem_gen.EmployeeDataFlowNode, []*telem_gen.EmployeeDataFlowEdge) {
	nodeAccs := make(map[string]*employeeGraphNodeAccumulator)
	edgeAccs := make(map[employeeGraphEdgeKey]*employeeGraphEdgeAccumulator)

	for _, row := range rows {
		callCount := row.CallCount
		if callCount == 0 {
			continue
		}

		path := employeeDataFlowPath(row)
		for _, pathNode := range path {
			nodeID := employeeDataFlowNodeID(pathNode)
			acc, ok := nodeAccs[nodeID]
			if !ok {
				acc = &employeeGraphNodeAccumulator{
					node: &telem_gen.EmployeeDataFlowNode{
						ID:          nodeID,
						Tier:        pathNode.tier,
						Label:       pathNode.label,
						TotalCalls:  0,
						ServerClass: nil,
					},
					totalCalls: 0,
				}
				if pathNode.tier == "server" && pathNode.serverClass != "" {
					serverClass := pathNode.serverClass
					acc.node.ServerClass = &serverClass
				}
				nodeAccs[nodeID] = acc
			}
			acc.totalCalls += callCount
		}

		for i := 0; i < len(path)-1; i++ {
			sourceID := employeeDataFlowNodeID(path[i])
			targetID := employeeDataFlowNodeID(path[i+1])
			edgeKey := employeeGraphEdgeKey{sourceID: sourceID, targetID: targetID}
			acc, ok := edgeAccs[edgeKey]
			if !ok {
				acc = &employeeGraphEdgeAccumulator{
					edge: &telem_gen.EmployeeDataFlowEdge{
						ID:           employeeDataFlowEdgeID(sourceID, targetID),
						Source:       sourceID,
						Target:       targetID,
						CallCount:    0,
						SuccessCount: 0,
						FailureCount: 0,
					},
					callCount:    0,
					successCount: 0,
					failureCount: 0,
				}
				edgeAccs[edgeKey] = acc
			}
			acc.callCount += row.CallCount
			acc.successCount += row.SuccessCount
			acc.failureCount += row.FailureCount
		}
	}

	nodes := make([]*telem_gen.EmployeeDataFlowNode, 0, len(nodeAccs))
	for _, acc := range nodeAccs {
		acc.node.TotalCalls = uint64ToInt64(acc.totalCalls)
		nodes = append(nodes, acc.node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		leftTier := employeeDataFlowTierOrder[nodes[i].Tier]
		rightTier := employeeDataFlowTierOrder[nodes[j].Tier]
		if leftTier != rightTier {
			return leftTier < rightTier
		}
		if nodes[i].TotalCalls != nodes[j].TotalCalls {
			return nodes[i].TotalCalls > nodes[j].TotalCalls
		}
		return nodes[i].Label < nodes[j].Label
	})

	edges := make([]*telem_gen.EmployeeDataFlowEdge, 0, len(edgeAccs))
	for _, acc := range edgeAccs {
		acc.edge.CallCount = uint64ToInt64(acc.callCount)
		acc.edge.SuccessCount = uint64ToInt64(acc.successCount)
		acc.edge.FailureCount = uint64ToInt64(acc.failureCount)
		edges = append(edges, acc.edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].CallCount != edges[j].CallCount {
			return edges[i].CallCount > edges[j].CallCount
		}
		return edges[i].ID < edges[j].ID
	})

	return pruneUnreachableEmployeeDataFlowGraph(nodes, edges)
}

// pruneUnreachableEmployeeDataFlowGraph keeps only the nodes reachable forward
// from an origin-tier node (and the edges between them). This drops dangling
// nodes such as an MCP client with no inbound connection, and anything that
// hangs off them. When there are no origin nodes the result is empty.
func pruneUnreachableEmployeeDataFlowGraph(nodes []*telem_gen.EmployeeDataFlowNode, edges []*telem_gen.EmployeeDataFlowEdge) ([]*telem_gen.EmployeeDataFlowNode, []*telem_gen.EmployeeDataFlowEdge) {
	adjacency := make(map[string][]string)
	for _, edge := range edges {
		adjacency[edge.Source] = append(adjacency[edge.Source], edge.Target)
	}

	reachable := make(map[string]struct{})
	queue := make([]string, 0)
	for _, node := range nodes {
		if node.Tier == "origin" {
			reachable[node.ID] = struct{}{}
			queue = append(queue, node.ID)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, target := range adjacency[id] {
			if _, ok := reachable[target]; !ok {
				reachable[target] = struct{}{}
				queue = append(queue, target)
			}
		}
	}

	prunedNodes := make([]*telem_gen.EmployeeDataFlowNode, 0, len(nodes))
	for _, node := range nodes {
		if _, ok := reachable[node.ID]; ok {
			prunedNodes = append(prunedNodes, node)
		}
	}

	prunedEdges := make([]*telem_gen.EmployeeDataFlowEdge, 0, len(edges))
	for _, edge := range edges {
		_, sourceOK := reachable[edge.Source]
		_, targetOK := reachable[edge.Target]
		if sourceOK && targetOK {
			prunedEdges = append(prunedEdges, edge)
		}
	}

	return prunedNodes, prunedEdges
}

func employeeDataFlowPath(row repo.EmployeeDataFlowRow) []employeeGraphTupleNode {
	candidates := []employeeGraphTupleNode{
		{tier: "origin", label: strings.TrimSpace(row.Origin), serverClass: ""},
		{tier: "client", label: strings.TrimSpace(row.Client), serverClass: ""},
		{tier: "server", label: strings.TrimSpace(row.Server), serverClass: strings.TrimSpace(row.ServerClass)},
		{tier: "tool", label: strings.TrimSpace(row.Tool), serverClass: ""},
	}

	path := make([]employeeGraphTupleNode, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.label == "" {
			continue
		}
		path = append(path, candidate)
	}
	return path
}

func employeeDataFlowNodeID(node employeeGraphTupleNode) string {
	if node.tier == "server" && node.serverClass != "" {
		return node.tier + ":" + node.serverClass + ":" + node.label
	}
	return node.tier + ":" + node.label
}

func employeeDataFlowEdgeID(sourceID, targetID string) string {
	return strconv.Itoa(len(sourceID)) + ":" + sourceID + "->" + strconv.Itoa(len(targetID)) + ":" + targetID
}

func uint64ToInt64(v uint64) int64 {
	const maxInt64 = uint64(1<<63 - 1)
	if v > maxInt64 {
		return int64(maxInt64)
	}
	return int64(v)
}

// searchParams contains common validated parameters for telemetry search endpoints.
type searchParams struct {
	projectID      string
	organizationID string
	limit          int
	sortOrder      string
	cursor         string
	timeStart      int64
	timeEnd        int64
}

// prepareTelemetrySearch validates and prepares common search parameters.
func (s *Service) prepareTelemetrySearch(ctx context.Context, limit int, sort string, cursor *string, from, to *string) (*searchParams, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "checking if logs enabled")
	}
	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	if limit < 1 || limit > 1000 {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and 1000")
	}

	sortOrder := "desc"
	if sort != "" && sort != "desc" && sort != "asc" {
		return nil, oops.E(oops.CodeBadRequest, nil, "sort order must be one of 'asc' or 'desc'")
	}
	if sort != "" {
		sortOrder = sort
	}

	cursorVal := ""
	if cursor != nil {
		cursorVal = *cursor
	}

	timeStart, timeEnd, err := parseTimeRange(from, to)
	if err != nil {
		return nil, err
	}

	return &searchParams{
		projectID:      authCtx.ProjectID.String(),
		organizationID: authCtx.ActiveOrganizationID,
		limit:          limit,
		sortOrder:      sortOrder,
		cursor:         cursorVal,
		timeStart:      timeStart,
		timeEnd:        timeEnd,
	}, nil
}

// parseTimeRange extracts and parses the time range from a telemetry filter.
// Returns Unix nanoseconds for start and end times.
// Defaults: start=0 (epoch), end=now
func parseTimeRange(from, to *string) (timeStart, timeEnd int64, err error) {
	timeStart = 0
	timeEnd = time.Now().UnixNano()

	if from != nil && *from != "" {
		fromTime, parseErr := time.Parse(time.RFC3339, *from)
		if parseErr != nil {
			return 0, 0, oops.E(oops.CodeBadRequest, parseErr, "invalid 'from' time format, expected ISO 8601 (e.g., '2025-12-19T10:00:00Z')")
		}
		timeStart = fromTime.UnixNano()
	}

	if to != nil && *to != "" {
		toTime, parseErr := time.Parse(time.RFC3339, *to)
		if parseErr != nil {
			return 0, 0, oops.E(oops.CodeBadRequest, parseErr, "invalid 'to' time format, expected ISO 8601 (e.g., '2025-12-19T11:00:00Z')")
		}
		timeEnd = toTime.UnixNano()
	}

	// Validate that from < to to prevent unsigned integer overflow in ClickHouse queries
	if timeStart >= timeEnd {
		return 0, 0, oops.E(oops.CodeBadRequest, nil, "'from' time must be before 'to' time")
	}

	return timeStart, timeEnd, nil
}

// resolveGramURNs handles backwards compatibility between gram_urn (single) and gram_urns (array).
// If gram_urns is provided, it takes precedence; otherwise gram_urn is used as a single-element array.
func resolveGramURNs(gramURN *string, gramURNs []string) []string {
	if len(gramURNs) > 0 {
		return gramURNs
	}
	if gramURN != nil && *gramURN != "" {
		return []string{*gramURN}
	}
	return nil
}

// toTelemetryLogPayload converts a ClickHouse telemetry log record to the API response format.
// It parses the JSON-encoded attributes and resource_attributes fields into proper JSON objects.
func toTelemetryLogPayload(log repo.TelemetryLog) (*telem_gen.TelemetryLogRecord, error) {
	// Parse JSON attributes into objects
	var attributes any
	var resourceAttributes any

	if err := json.Unmarshal([]byte(log.Attributes), &attributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse log attributes")
	}
	if err := json.Unmarshal([]byte(log.ResourceAttributes), &resourceAttributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse resource attributes")
	}

	return &telem_gen.TelemetryLogRecord{
		ID:                   log.ID,
		TimeUnixNano:         strconv.FormatInt(log.TimeUnixNano, 10),
		ObservedTimeUnixNano: strconv.FormatInt(log.ObservedTimeUnixNano, 10),
		SeverityText:         log.SeverityText,
		Body:                 log.Body,
		TraceID:              log.TraceID,
		SpanID:               log.SpanID,
		Attributes:           attributes,
		ResourceAttributes:   resourceAttributes,
		Service: &telem_gen.ServiceInfo{
			Name:    log.ServiceName,
			Version: log.ServiceVersion,
		},
	}, nil
}

// toRepoAttributeFilters converts generated Goa log filters to repo types.
func toRepoAttributeFilters(filters []*telem_gen.LogFilter) []repo.AttributeFilter {
	if len(filters) == 0 {
		return nil
	}
	result := make([]repo.AttributeFilter, 0, len(filters))
	for _, f := range filters {
		if f == nil {
			continue
		}
		result = append(result, repo.AttributeFilter{
			Path:   f.Path,
			Op:     f.Operator,
			Values: f.Values,
		})
	}
	return result
}

// sanitizeFloat64 returns 0 for NaN or Inf values which cannot be JSON-encoded.
func sanitizeFloat64(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// CaptureEvent captures a telemetry event and forwards it to PostHog.
func (s *Service) CaptureEvent(ctx context.Context, payload *telem_gen.CaptureEventPayload) (res *telem_gen.CaptureEventResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Use provided distinct_id or default to organization ID
	distinctID := authCtx.ActiveOrganizationID
	if payload.DistinctID != nil && *payload.DistinctID != "" {
		distinctID = *payload.DistinctID
	}

	// Build event properties
	properties := make(map[string]any)
	if payload.Properties != nil {
		properties = payload.Properties
	}

	if authCtx.Email != nil {
		properties["email"] = *authCtx.Email
	}
	if authCtx.ProjectSlug != nil {
		properties["project_slug"] = *authCtx.ProjectSlug
	}
	properties["organization_slug"] = authCtx.OrganizationSlug
	properties["user_id"] = authCtx.UserID
	properties["external_user_id"] = authCtx.ExternalUserID

	// Capture event in PostHog
	if err := s.posthog.CaptureEvent(ctx, payload.Event, distinctID, properties); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to capture event").
			LogError(ctx, s.logger,
				attr.SlogEvent(payload.Event),
			)
	}

	s.logger.DebugContext(ctx, "captured telemetry event",
		attr.SlogEvent(payload.Event),
	)

	return &telem_gen.CaptureEventResult{
		Success: true,
	}, nil
}

// GetObservabilityOverview retrieves aggregated observability metrics for the overview dashboard.
func (s *Service) GetObservabilityOverview(ctx context.Context, payload *telem_gen.GetObservabilityOverviewPayload) (res *telem_gen.GetObservabilityOverviewResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	projectID := authCtx.ProjectID.String()
	userID := conv.PtrValOr(payload.UserID, "")
	externalUserID := conv.PtrValOr(payload.ExternalUserID, "")
	apiKeyID := conv.PtrValOr(payload.APIKeyID, "")
	toolsetSlug := conv.PtrValOr(payload.ToolsetSlug, "")
	remoteMCPServerID := conv.PtrValOr(payload.RemoteMcpServerID, "")
	mcpServerID := conv.PtrValOr(payload.McpServerID, "")
	eventSource := conv.PtrValOr(payload.EventSource, "")
	hookSource := conv.PtrValOr(payload.HookSource, "")
	accountType := conv.PtrValOr(payload.AccountType, "")
	externalOrgID := conv.PtrValOr(payload.ExternalOrgID, "")

	if userID != "" && externalUserID != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "only one of user_id or external_user_id can be provided")
	}

	// Auto-calculate interval based on time range
	intervalSeconds := calculateInterval(timeStart, timeEnd)

	// Calculate comparison period (same duration, immediately before)
	duration := timeEnd - timeStart
	comparisonStart := timeStart - duration
	comparisonEnd := timeStart

	// Fetch all data sequentially to avoid ClickHouse concurrent query limits
	summary, err := s.chRepo.GetOverviewSummary(ctx, repo.GetOverviewSummaryParams{
		GramProjectID:     projectID,
		TimeStart:         timeStart,
		TimeEnd:           timeEnd,
		UserID:            userID,
		ExternalUserID:    externalUserID,
		APIKeyID:          apiKeyID,
		ToolsetSlug:       toolsetSlug,
		RemoteMCPServerID: remoteMCPServerID,
		MCPServerID:       mcpServerID,
		EventSource:       eventSource,
		HookSource:        hookSource,
		AccountType:       accountType,
		ExternalOrgID:     externalOrgID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving overview summary")
	}

	comparison, err := s.chRepo.GetOverviewSummary(ctx, repo.GetOverviewSummaryParams{
		GramProjectID:     projectID,
		TimeStart:         comparisonStart,
		TimeEnd:           comparisonEnd,
		UserID:            userID,
		ExternalUserID:    externalUserID,
		APIKeyID:          apiKeyID,
		ToolsetSlug:       toolsetSlug,
		RemoteMCPServerID: remoteMCPServerID,
		MCPServerID:       mcpServerID,
		EventSource:       eventSource,
		HookSource:        hookSource,
		AccountType:       accountType,
		ExternalOrgID:     externalOrgID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving comparison summary")
	}

	var timeSeries []repo.TimeSeriesBucket
	if payload.IncludeTimeSeries {
		timeSeries, err = s.chRepo.GetTimeSeriesMetrics(ctx, repo.GetTimeSeriesMetricsParams{
			GramProjectID:     projectID,
			TimeStart:         timeStart,
			TimeEnd:           timeEnd,
			IntervalSeconds:   intervalSeconds,
			UserID:            userID,
			ExternalUserID:    externalUserID,
			APIKeyID:          apiKeyID,
			ToolsetSlug:       toolsetSlug,
			RemoteMCPServerID: remoteMCPServerID,
			MCPServerID:       mcpServerID,
			EventSource:       eventSource,
			HookSource:        hookSource,
			AccountType:       accountType,
			ExternalOrgID:     externalOrgID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error retrieving time series")
		}
	}

	toolsByCount, err := s.chRepo.GetToolMetricsBreakdown(ctx, repo.GetToolMetricsBreakdownParams{
		GramProjectID:     projectID,
		TimeStart:         timeStart,
		TimeEnd:           timeEnd,
		UserID:            userID,
		ExternalUserID:    externalUserID,
		APIKeyID:          apiKeyID,
		ToolsetSlug:       toolsetSlug,
		RemoteMCPServerID: remoteMCPServerID,
		MCPServerID:       mcpServerID,
		EventSource:       eventSource,
		HookSource:        hookSource,
		AccountType:       accountType,
		ExternalOrgID:     externalOrgID,
		Limit:             10,
		SortBy:            "count",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving tools by count")
	}

	toolsByFailure, err := s.chRepo.GetToolMetricsBreakdown(ctx, repo.GetToolMetricsBreakdownParams{
		GramProjectID:     projectID,
		TimeStart:         timeStart,
		TimeEnd:           timeEnd,
		UserID:            userID,
		ExternalUserID:    externalUserID,
		APIKeyID:          apiKeyID,
		ToolsetSlug:       toolsetSlug,
		RemoteMCPServerID: remoteMCPServerID,
		MCPServerID:       mcpServerID,
		EventSource:       eventSource,
		HookSource:        hookSource,
		AccountType:       accountType,
		ExternalOrgID:     externalOrgID,
		Limit:             10,
		SortBy:            "failure_rate",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving tools by failure rate")
	}

	// Convert to API types
	return &telem_gen.GetObservabilityOverviewResult{
		Summary:               toObservabilitySummary(summary),
		Comparison:            toObservabilitySummary(comparison),
		TimeSeries:            toTimeSeriesBuckets(timeSeries),
		TopToolsByCount:       toToolMetrics(toolsByCount),
		TopToolsByFailureRate: toToolMetrics(toolsByFailure),
		IntervalSeconds:       intervalSeconds,
	}, nil
}

// GetProjectOverview retrieves project-level overview metrics including total chats, tool calls,
// active servers/users, and top lists. This endpoint does not support filtering.
func (s *Service) GetProjectOverview(ctx context.Context, payload *telem_gen.GetProjectOverviewPayload) (res *telem_gen.GetProjectOverviewResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	projectID := authCtx.ProjectID.String()

	// Calculate comparison period (same duration as current period, ending at current period start)
	duration := timeEnd - timeStart
	comparisonStart := timeStart - duration
	comparisonEnd := timeStart

	// Convert timestamps for PostgreSQL queries
	timeStartPG := conv.ToPGTimestamptz(time.Unix(0, timeStart))
	timeEndPG := conv.ToPGTimestamptz(time.Unix(0, timeEnd))
	comparisonStartPG := conv.ToPGTimestamptz(time.Unix(0, comparisonStart))
	comparisonEndPG := conv.ToPGTimestamptz(time.Unix(0, comparisonEnd))

	// Determine metrics mode: Check if session capture is enabled
	sessionCaptureEnabled, err := s.sessionCaptureEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if session capture is enabled")
	}

	sessionMode := sessionCaptureEnabled
	metricsMode := "tool_call"
	if sessionMode {
		metricsMode = "session"
	}

	// These queries hit two database pools. Each pool is safe for concurrent use,
	// so independent queries run concurrently. The ClickHouse helper keeps its
	// orchestration and query-level tracing together.
	var (
		chatMetrics           chatRepo.GetChatMetricsSummaryRow
		chatMetricsComparison chatRepo.GetChatMetricsSummaryRow
		clickHouseResult      projectOverviewClickHouseResult
		serverNameOverrides   []hooksRepo.ListHooksServerNameOverridesRow

		// Session-mode (PostgreSQL) results; only populated when sessionMode is true.
		activeUsersCountPG int64
		topUsersPG         []chatRepo.GetTopUsersByMessagesRow
		llmClientsPG       []chatRepo.GetLLMClientBreakdownByMessagesRow
	)

	eg, egCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		var fetchErr error
		clickHouseResult, fetchErr = fetchProjectOverviewClickHouse(egCtx, s.tracer, s.chRepo, projectOverviewClickHouseParams{
			projectID:       projectID,
			timeStart:       timeStart,
			timeEnd:         timeEnd,
			comparisonStart: comparisonStart,
			comparisonEnd:   comparisonEnd,
			sessionMode:     sessionMode,
		})
		return fetchErr
	})

	// PostgreSQL lanes: the pgxpool is safe for concurrent use, so fan these out.

	// Chat metrics: current and comparison periods.
	eg.Go(func() error {
		var fetchErr error
		chatMetrics, fetchErr = s.chatRepo.GetChatMetricsSummary(egCtx, chatRepo.GetChatMetricsSummaryParams{
			ProjectID: *authCtx.ProjectID,
			TimeStart: timeStartPG,
			TimeEnd:   timeEndPG,
		})
		if fetchErr != nil {
			return oops.E(oops.CodeUnexpected, fetchErr, "error retrieving chat metrics summary")
		}
		return nil
	})
	eg.Go(func() error {
		var fetchErr error
		chatMetricsComparison, fetchErr = s.chatRepo.GetChatMetricsSummary(egCtx, chatRepo.GetChatMetricsSummaryParams{
			ProjectID: *authCtx.ProjectID,
			TimeStart: comparisonStartPG,
			TimeEnd:   comparisonEndPG,
		})
		if fetchErr != nil {
			return oops.E(oops.CodeUnexpected, fetchErr, "error retrieving comparison chat metrics")
		}
		return nil
	})

	// Server name overrides.
	eg.Go(func() error {
		var fetchErr error
		serverNameOverrides, fetchErr = s.hooksRepo.ListHooksServerNameOverrides(egCtx, *authCtx.ProjectID)
		if fetchErr != nil {
			return oops.E(oops.CodeUnexpected, fetchErr, "error retrieving server name overrides")
		}
		return nil
	})

	if sessionMode {
		// Session-based metrics come from PostgreSQL.
		eg.Go(func() error {
			var fetchErr error
			activeUsersCountPG, fetchErr = s.chatRepo.GetActiveUserCountByMessages(egCtx, chatRepo.GetActiveUserCountByMessagesParams{
				ProjectID: *authCtx.ProjectID,
				TimeStart: timeStartPG,
				TimeEnd:   timeEndPG,
			})
			if fetchErr != nil {
				return oops.E(oops.CodeUnexpected, fetchErr, "error retrieving active user count from PG")
			}
			return nil
		})
		eg.Go(func() error {
			var fetchErr error
			topUsersPG, fetchErr = s.chatRepo.GetTopUsersByMessages(egCtx, chatRepo.GetTopUsersByMessagesParams{
				ProjectID:   *authCtx.ProjectID,
				TimeStart:   timeStartPG,
				TimeEnd:     timeEndPG,
				ResultLimit: 10,
			})
			if fetchErr != nil {
				return oops.E(oops.CodeUnexpected, fetchErr, "error retrieving top users from PG")
			}
			return nil
		})
		eg.Go(func() error {
			var fetchErr error
			llmClientsPG, fetchErr = s.chatRepo.GetLLMClientBreakdownByMessages(egCtx, chatRepo.GetLLMClientBreakdownByMessagesParams{
				ProjectID: *authCtx.ProjectID,
				TimeStart: timeStartPG,
				TimeEnd:   timeEndPG,
			})
			if fetchErr != nil {
				return oops.E(oops.CodeUnexpected, fetchErr, "error retrieving LLM client breakdown from PG")
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving project overview")
	}

	// Resolve active counts and top lists now that every query has returned.
	activeServersCount := int64(clickHouseResult.activeCounts.ActiveServersCount) //nolint:gosec // Bounded count that won't overflow int64
	var activeUsersCount int64
	var topUsers []*telem_gen.TopUser
	var llmClientBreakdown []*telem_gen.LLMClientUsage
	if sessionMode {
		activeUsersCount = activeUsersCountPG
		topUsers = toTopUsersFromPG(topUsersPG)
		llmClientBreakdown = toLLMClientUsageFromPG(llmClientsPG)
	} else {
		activeUsersCount = int64(clickHouseResult.activeCounts.ActiveUsersCount) //nolint:gosec // Bounded count that won't overflow int64
		topUsers = toTopUsers(clickHouseResult.topUsers)
		llmClientBreakdown = toLLMClientUsage(clickHouseResult.llmClients)
	}

	// Build a map for quick lookup: raw_server_name -> display_name
	overrideMap := make(map[string]string, len(serverNameOverrides))
	for _, override := range serverNameOverrides {
		overrideMap[override.RawServerName] = override.DisplayName
	}

	// Apply overrides to top servers
	topServersWithOverrides := applyServerNameOverrides(clickHouseResult.topServers, overrideMap)

	// Convert to API types - build summaries with nested fields
	return &telem_gen.GetProjectOverviewResult{
		Summary: buildProjectOverviewSummary(
			chatMetrics,
			clickHouseResult.toolMetrics,
			activeServersCount,
			activeUsersCount,
			topUsers,
			toTopServers(topServersWithOverrides),
			llmClientBreakdown,
		),
		Comparison: buildProjectOverviewSummary(
			chatMetricsComparison,
			clickHouseResult.toolMetricsComparison,
			0, // Don't need active counts for comparison
			0,
			nil, // Don't need top lists for comparison
			nil,
			nil,
		),
		MetricsMode: metricsMode,
	}, nil
}

// calculateInterval determines the appropriate time bucket interval based on the time range.
// Returns interval in seconds.
func calculateInterval(timeStart, timeEnd int64) int64 {
	durationNanos := timeEnd - timeStart
	durationHours := durationNanos / (int64(time.Hour))

	switch {
	case durationHours <= 1:
		return 60 // 1 minute buckets
	case durationHours <= 24:
		return 900 // 15 minute buckets
	case durationHours <= 168: // 7 days
		return 3600 // 1 hour buckets
	case durationHours <= 720: // 30 days
		return 21600 // 6 hour buckets
	default:
		return 86400 // 1 day buckets for 90+ days
	}
}

// buildObservabilitySummary builds an ObservabilitySummary from multiple data sources.
// Chat metrics come from PostgreSQL, tool metrics from ClickHouse, and additional data from both.
// toObservabilitySummary converts repo summary to API type.
func toObservabilitySummary(summary *repo.OverviewSummary) *telem_gen.ObservabilitySummary {
	if summary == nil {
		return &telem_gen.ObservabilitySummary{
			TotalChats:               0,
			ResolvedChats:            0,
			FailedChats:              0,
			AvgSessionDurationMs:     0,
			AvgResolutionTimeMs:      0,
			TotalInputTokens:         0,
			TotalOutputTokens:        0,
			TotalTokens:              0,
			CacheReadInputTokens:     0,
			CacheCreationInputTokens: 0,
			TotalCost:                0,
			TotalToolCalls:           0,
			FailedToolCalls:          0,
			AvgLatencyMs:             0,
		}
	}
	//nolint:gosec // Values are bounded counts that won't overflow int64
	return &telem_gen.ObservabilitySummary{
		TotalChats:               int64(summary.TotalChats),
		ResolvedChats:            int64(summary.ResolvedChats),
		FailedChats:              int64(summary.FailedChats),
		AvgSessionDurationMs:     sanitizeFloat64(summary.AvgSessionDurationMs),
		AvgResolutionTimeMs:      sanitizeFloat64(summary.AvgResolutionTimeMs),
		TotalInputTokens:         summary.TotalInputTokens,
		TotalOutputTokens:        summary.TotalOutputTokens,
		TotalTokens:              summary.TotalTokens,
		CacheReadInputTokens:     summary.CacheReadInputTokens,
		CacheCreationInputTokens: summary.CacheCreationInputTokens,
		TotalCost:                sanitizeFloat64(summary.TotalCost),
		TotalToolCalls:           int64(summary.TotalToolCalls),
		FailedToolCalls:          int64(summary.FailedToolCalls),
		AvgLatencyMs:             sanitizeFloat64(summary.AvgLatencyMs),
	}
}

// buildProjectOverviewSummary builds a ProjectOverviewSummary from multiple data sources.
// Chat metrics come from PostgreSQL, tool metrics from ClickHouse, and additional data from both.
func buildProjectOverviewSummary(
	chatMetrics chatRepo.GetChatMetricsSummaryRow,
	toolMetrics *repo.OverviewSummary,
	activeServersCount int64,
	activeUsersCount int64,
	topUsers []*telem_gen.TopUser,
	topServers []*telem_gen.TopServer,
	llmClientBreakdown []*telem_gen.LLMClientUsage,
) *telem_gen.ProjectOverviewSummary {
	// Get tool metrics from ClickHouse (or defaults if nil)
	var totalToolCalls, failedToolCalls int64
	if toolMetrics != nil {
		totalToolCalls = int64(toolMetrics.TotalToolCalls)   //nolint:gosec // Bounded count that won't overflow int64
		failedToolCalls = int64(toolMetrics.FailedToolCalls) //nolint:gosec // Bounded count that won't overflow int64
	}

	// Ensure arrays are non-nil
	if topUsers == nil {
		topUsers = []*telem_gen.TopUser{}
	}
	if topServers == nil {
		topServers = []*telem_gen.TopServer{}
	}
	if llmClientBreakdown == nil {
		llmClientBreakdown = []*telem_gen.LLMClientUsage{}
	}

	return &telem_gen.ProjectOverviewSummary{
		// Chat metrics from PostgreSQL
		TotalChats:    chatMetrics.TotalChats,
		ResolvedChats: chatMetrics.ResolvedChats,
		FailedChats:   chatMetrics.FailedChats,
		// Tool metrics from ClickHouse
		TotalToolCalls:  totalToolCalls,
		FailedToolCalls: failedToolCalls,
		// Activity counts and top lists
		ActiveServersCount: activeServersCount,
		ActiveUsersCount:   activeUsersCount,
		TopUsers:           topUsers,
		TopServers:         topServers,
		LlmClientBreakdown: llmClientBreakdown,
	}
}

// toTimeSeriesBuckets converts repo buckets to API type.
func toTimeSeriesBuckets(buckets []repo.TimeSeriesBucket) []*telem_gen.TimeSeriesBucket {
	if buckets == nil {
		return []*telem_gen.TimeSeriesBucket{}
	}
	result := make([]*telem_gen.TimeSeriesBucket, len(buckets))
	for i, b := range buckets {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.TimeSeriesBucket{
			BucketTimeUnixNano:       strconv.FormatInt(b.BucketTimeUnixNano, 10),
			TotalChats:               int64(b.TotalChats),
			ResolvedChats:            int64(b.ResolvedChats),
			FailedChats:              int64(b.FailedChats),
			PartialChats:             int64(b.PartialChats),
			AbandonedChats:           int64(b.AbandonedChats),
			TotalInputTokens:         b.TotalInputTokens,
			TotalOutputTokens:        b.TotalOutputTokens,
			TotalTokens:              b.TotalTokens,
			CacheReadInputTokens:     b.CacheReadInputTokens,
			CacheCreationInputTokens: b.CacheCreationInputTokens,
			TotalCost:                sanitizeFloat64(b.TotalCost),
			TotalToolCalls:           int64(b.TotalToolCalls),
			FailedToolCalls:          int64(b.FailedToolCalls),
			AvgToolLatencyMs:         sanitizeFloat64(b.AvgToolLatencyMs),
			AvgSessionDurationMs:     sanitizeFloat64(b.AvgSessionDurationMs),
		}
	}
	return result
}

// toToolMetrics converts repo tool metrics to API type.
func toToolMetrics(tools []repo.ToolMetric) []*telem_gen.ToolMetric {
	if tools == nil {
		return []*telem_gen.ToolMetric{}
	}
	result := make([]*telem_gen.ToolMetric, len(tools))
	for i, t := range tools {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.ToolMetric{
			GramUrn:      t.GramURN,
			CallCount:    int64(t.CallCount),
			SuccessCount: int64(t.SuccessCount),
			FailureCount: int64(t.FailureCount),
			AvgLatencyMs: sanitizeFloat64(t.AvgLatencyMs),
			FailureRate:  sanitizeFloat64(t.FailureRate),
		}
	}
	return result
}

// toTopUsers converts repo top users to API type.
func toTopUsers(users []repo.TopUser) []*telem_gen.TopUser {
	if users == nil {
		return []*telem_gen.TopUser{}
	}
	result := make([]*telem_gen.TopUser, len(users))
	for i, u := range users {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.TopUser{
			UserID:        u.UserID,
			UserType:      u.UserType,
			ActivityCount: int64(u.ActivityCount),
		}
	}
	return result
}

// applyServerNameOverrides applies display name overrides to server names.
// Merges entries with the same display name after applying overrides and re-sorts by count.
func applyServerNameOverrides(servers []repo.TopServer, overrideMap map[string]string) []repo.TopServer {
	if len(overrideMap) == 0 {
		return servers
	}

	// Apply overrides and aggregate counts by display name
	counts := make(map[string]uint64)
	for _, s := range servers {
		displayName := s.ServerName
		if override, ok := overrideMap[s.ServerName]; ok {
			displayName = override
		}
		counts[displayName] += s.ToolCallCount
	}

	// Convert back to slice
	result := make([]repo.TopServer, 0, len(counts))
	for name, count := range counts {
		result = append(result, repo.TopServer{
			ServerName:    name,
			ToolCallCount: count,
		})
	}

	// Re-sort by ToolCallCount descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].ToolCallCount > result[j].ToolCallCount
	})

	return result
}

// toTopServers converts repo top servers to API type.
func toTopServers(servers []repo.TopServer) []*telem_gen.TopServer {
	if servers == nil {
		return []*telem_gen.TopServer{}
	}
	result := make([]*telem_gen.TopServer, len(servers))
	for i, s := range servers {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.TopServer{
			ServerName:    s.ServerName,
			ToolCallCount: int64(s.ToolCallCount),
		}
	}
	return result
}

// toLLMClientUsage converts repo LLM client usage to API type.
func toLLMClientUsage(clients []repo.LLMClientUsage) []*telem_gen.LLMClientUsage {
	if clients == nil {
		return []*telem_gen.LLMClientUsage{}
	}
	result := make([]*telem_gen.LLMClientUsage, len(clients))
	for i, c := range clients {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.LLMClientUsage{
			ClientName:    c.ClientName,
			ActivityCount: int64(c.ActivityCount),
		}
	}
	return result
}

// ListFilterOptions retrieves available filter options (API keys or users) for the observability dashboard.
func (s *Service) ListFilterOptions(ctx context.Context, payload *telem_gen.ListFilterOptionsPayload) (res *telem_gen.ListFilterOptionsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	options, err := s.chRepo.ListFilterOptions(ctx, repo.ListFilterOptionsParams{
		GramProjectID: authCtx.ProjectID.String(),
		TimeStart:     timeStart,
		TimeEnd:       timeEnd,
		FilterType:    payload.FilterType,
		EventSource:   conv.PtrValOr(payload.EventSource, ""),
		Limit:         100,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing filter options")
	}

	// Convert to API types
	result := make([]*telem_gen.FilterOption, len(options))
	for i, opt := range options {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.FilterOption{
			ID:    opt.ID,
			Label: opt.Label,
			Count: int64(opt.Count),
		}
	}

	return &telem_gen.ListFilterOptionsResult{
		Options: result,
	}, nil
}

// ListAttributeKeys retrieves distinct attribute keys from telemetry logs for the current project.
func (s *Service) ListAttributeKeys(ctx context.Context, payload *telem_gen.ListAttributeKeysPayload) (res *telem_gen.ListAttributeKeysResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	rawKeys, err := s.chRepo.ListAttributeKeys(ctx, repo.ListAttributeKeysParams{
		GramProjectID: authCtx.ProjectID.String(),
		TimeStart:     timeStart,
		TimeEnd:       timeEnd,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing attribute keys")
	}

	// Translate raw attribute paths to display keys:
	// "app.region" → "@region", everything else stays as-is.
	keys := make([]string, 0, len(rawKeys))
	for _, k := range rawKeys {
		if after, ok := strings.CutPrefix(k, "app."); ok {
			keys = append(keys, "@"+after)
			continue
		}

		keys = append(keys, k)
	}
	sort.Strings(keys)

	return &telem_gen.ListAttributeKeysResult{
		Keys: keys,
	}, nil
}

// GetHooksSummary returns aggregated hooks metrics grouped by server
func (s *Service) GetHooksSummary(ctx context.Context, payload *telem_gen.GetHooksSummaryPayload) (res *telem_gen.GetHooksSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	attributeFilters := toRepoAttributeFilters(payload.Filters)
	typesToInclude := payload.TypesToInclude

	// Compute time series bucket size: 5 min for ≤24h windows, 60 min otherwise
	const fiveMinNs = int64(5 * 60 * 1e9)
	const sixtyMinNs = int64(60 * 60 * 1e9)
	bucketSizeNs := sixtyMinNs
	if timeEnd-timeStart <= int64(24*60*60*1e9) {
		bucketSizeNs = fiveMinNs
	}

	// Run all eight independent ClickHouse queries in parallel
	var (
		serverRows            []repo.HooksServerSummaryRow
		userRows              []repo.HooksUserSummaryRow
		skillRows             []repo.SkillSummaryRow
		skillBreakdownRows    []repo.SkillBreakdownRow
		breakdownRows         []repo.HooksBreakdownRow
		timeSeriesPoints      []repo.HooksTimeSeriesPoint
		skillTimeSeriesPoints []repo.SkillTimeSeriesPoint
		sessionCount          int64
	)
	eg, egCtx := errgroup.WithContext(ctx)
	projectID := authCtx.ProjectID.String()

	eg.Go(func() error {
		var err error
		serverRows, err = s.chRepo.GetHooksSummary(egCtx, repo.GetHooksSummaryParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks server summary: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		userRows, err = s.chRepo.GetHooksUserSummary(egCtx, repo.GetHooksUserSummaryParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks user summary: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		skillRows, err = s.chRepo.GetSkillsSummary(egCtx, repo.GetSkillsSummaryParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get skills summary: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		skillBreakdownRows, err = s.chRepo.GetSkillBreakdown(egCtx, repo.GetSkillBreakdownParams{
			GramProjectID: projectID,
			TimeStart:     timeStart,
			TimeEnd:       timeEnd,
			Filters:       attributeFilters,
		})
		if err != nil {
			return fmt.Errorf("get skill breakdown: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		breakdownRows, err = s.chRepo.GetHooksBreakdown(egCtx, repo.GetHooksBreakdownParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks breakdown: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		timeSeriesPoints, err = s.chRepo.GetHooksTimeSeries(egCtx, repo.GetHooksTimeSeriesParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			BucketSizeNs:   bucketSizeNs,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks time series: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		skillTimeSeriesPoints, err = s.chRepo.GetSkillTimeSeries(egCtx, repo.GetSkillTimeSeriesParams{
			GramProjectID: projectID,
			TimeStart:     timeStart,
			TimeEnd:       timeEnd,
			BucketSizeNs:  bucketSizeNs,
			Filters:       attributeFilters,
		})
		if err != nil {
			return fmt.Errorf("get skill time series: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		sessionCount, err = s.chRepo.GetHooksSessionCount(egCtx, repo.GetHooksSessionCountParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks session count: %w", err)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching hooks summary data")
	}

	// Transform server rows into response
	servers := make([]*telem_gen.HooksServerSummary, 0, len(serverRows))
	var totalEvents, totalSessions int64
	for _, row := range serverRows {
		servers = append(servers, &telem_gen.HooksServerSummary{
			ServerName:   row.ServerName,
			EventCount:   int64(row.EventCount),   //nolint:gosec // Bounded count
			UniqueTools:  int64(row.UniqueTools),  //nolint:gosec // Bounded count
			SuccessCount: int64(row.SuccessCount), //nolint:gosec // Bounded count
			FailureCount: int64(row.FailureCount), //nolint:gosec // Bounded count
			FailureRate:  row.FailureRate,
		})
		totalEvents += int64(row.EventCount) //nolint:gosec // Bounded count
	}

	// Transform user rows into response
	users := make([]*telem_gen.HooksUserSummary, 0, len(userRows))
	for _, row := range userRows {
		users = append(users, &telem_gen.HooksUserSummary{
			UserEmail:    row.UserEmail,
			EventCount:   int64(row.EventCount),   //nolint:gosec // Bounded count
			UniqueTools:  int64(row.UniqueTools),  //nolint:gosec // Bounded count
			SuccessCount: int64(row.SuccessCount), //nolint:gosec // Bounded count
			FailureCount: int64(row.FailureCount), //nolint:gosec // Bounded count
			FailureRate:  row.FailureRate,
		})
	}

	// Transform skills rows into response
	skills := make([]*telem_gen.SkillSummary, 0, len(skillRows))
	for _, row := range skillRows {
		skills = append(skills, &telem_gen.SkillSummary{
			SkillName:   row.SkillName,
			UseCount:    int64(row.UseCount),    //nolint:gosec // Bounded count
			UniqueUsers: int64(row.UniqueUsers), //nolint:gosec // Bounded count
		})
	}

	// Transform skill breakdown rows into response
	skillBreakdown := make([]*telem_gen.SkillBreakdownRow, 0, len(skillBreakdownRows))
	for _, row := range skillBreakdownRows {
		skillBreakdown = append(skillBreakdown, &telem_gen.SkillBreakdownRow{
			SkillName: row.SkillName,
			UserEmail: row.UserEmail,
			UseCount:  int64(row.UseCount), //nolint:gosec // Bounded count
		})
	}

	// Transform breakdown rows into response
	breakdown := make([]*telem_gen.HooksBreakdownRow, 0, len(breakdownRows))
	for _, row := range breakdownRows {
		breakdown = append(breakdown, &telem_gen.HooksBreakdownRow{
			UserEmail:    row.UserEmail,
			ServerName:   row.ServerName,
			HookSource:   row.HookSource,
			ToolName:     row.ToolName,
			EventCount:   int64(row.EventCount),   //nolint:gosec // Bounded count
			FailureCount: int64(row.FailureCount), //nolint:gosec // Bounded count
		})
	}

	// Transform time series points into response
	timeSeries := make([]*telem_gen.HooksTimeSeriesPoint, 0, len(timeSeriesPoints))
	for _, pt := range timeSeriesPoints {
		timeSeries = append(timeSeries, &telem_gen.HooksTimeSeriesPoint{
			BucketStartNs: strconv.FormatInt(pt.BucketStartNs, 10),
			ServerName:    pt.ServerName,
			UserEmail:     pt.UserEmail,
			EventCount:    int64(pt.EventCount),   //nolint:gosec // Bounded count
			FailureCount:  int64(pt.FailureCount), //nolint:gosec // Bounded count
		})
	}

	skillTimeSeries := make([]*telem_gen.SkillTimeSeriesPoint, 0, len(skillTimeSeriesPoints))
	for _, pt := range skillTimeSeriesPoints {
		skillTimeSeries = append(skillTimeSeries, &telem_gen.SkillTimeSeriesPoint{
			BucketStartNs: strconv.FormatInt(pt.BucketStartNs, 10),
			SkillName:     pt.SkillName,
			EventCount:    int64(pt.EventCount), //nolint:gosec // Bounded count
		})
	}

	totalSessions = sessionCount

	return &telem_gen.GetHooksSummaryResult{
		Servers:         servers,
		Users:           users,
		Skills:          skills,
		SkillBreakdown:  skillBreakdown,
		TotalEvents:     totalEvents,
		TotalSessions:   totalSessions,
		Breakdown:       breakdown,
		TimeSeries:      timeSeries,
		SkillTimeSeries: skillTimeSeries,
	}, nil
}

// GetToolUsageSummary returns target-aware MCP and tool usage metrics.
func (s *Service) GetToolUsageSummary(ctx context.Context, payload *telem_gen.GetToolUsageSummaryPayload) (res *telem_gen.GetToolUsageSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	const fiveMinNs = int64(5 * 60 * 1e9)
	const sixtyMinNs = int64(60 * 60 * 1e9)
	bucketSizeNs := sixtyMinNs
	if timeEnd-timeStart <= int64(24*60*60*1e9) {
		bucketSizeNs = fiveMinNs
	}

	targetTypes := make([]string, 0, len(payload.TargetTypes))
	for _, targetType := range payload.TargetTypes {
		targetTypes = append(targetTypes, string(targetType))
	}

	userFilters := make([]repo.ToolUsageUserFilter, 0, len(payload.UserFilters))
	for _, filter := range payload.UserFilters {
		if filter == nil {
			continue
		}
		userFilters = append(userFilters, repo.ToolUsageUserFilter{
			Kind: string(filter.Kind),
			Key:  filter.Key,
		})
	}

	hostedMCPMatchers, err := s.toolUsageHostedMCPMatchers(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing hosted MCP servers")
	}
	mcpServerMatchers, err := s.toolUsageMCPServerMatchers(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing MCP servers")
	}

	summary, err := s.chRepo.GetToolUsageSummary(ctx, repo.GetToolUsageSummaryParams{
		GramProjectID:      authCtx.ProjectID.String(),
		TimeStart:          timeStart,
		TimeEnd:            timeEnd,
		BucketSizeNs:       bucketSizeNs,
		HostedMCPMatchers:  hostedMCPMatchers,
		MCPServerMatchers:  mcpServerMatchers,
		TargetTypes:        targetTypes,
		HostedToolsetSlugs: payload.HostedToolsetSlugs,
		ShadowServerNames:  payload.ShadowServerNames,
		UserFilters:        userFilters,
		HookSources:        payload.HookSources,
		AccountType:        conv.PtrValOr(payload.AccountType, ""),
		TargetLimit:        25,
		UserLimit:          25,
		UsersByTargetLimit: 100,
		TargetToolRowLimit: 100,
		TimeSeriesRowLimit: 10000,
		UserSeriesRowLimit: 10000,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching tool usage summary data")
	}

	return toToolUsageSummaryResult(summary), nil
}

func (s *Service) ListToolUsageTraces(ctx context.Context, payload *telem_gen.ListToolUsageTracesPayload) (res *telem_gen.ListToolUsageTracesResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, &payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	targetTypes := make([]string, 0, len(payload.TargetTypes))
	for _, targetType := range payload.TargetTypes {
		targetTypes = append(targetTypes, string(targetType))
	}

	statuses := make([]string, 0, len(payload.Statuses))
	for _, status := range payload.Statuses {
		statuses = append(statuses, string(status))
	}

	userFilters := make([]repo.ToolUsageUserFilter, 0, len(payload.UserFilters))
	for _, filter := range payload.UserFilters {
		if filter == nil {
			continue
		}
		userFilters = append(userFilters, repo.ToolUsageUserFilter{
			Kind: string(filter.Kind),
			Key:  filter.Key,
		})
	}

	cursorTimeUnixNano := int64(0)
	cursorID := ""
	if params.cursor != "" {
		cursorTimeUnixNano, cursorID, err = decodeToolUsageTraceCursor(params.cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
	}

	hostedMCPMatchers, err := s.toolUsageHostedMCPMatchers(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing hosted MCP servers").LogError(ctx, logger)
	}
	mcpServerMatchers, err := s.toolUsageMCPServerMatchers(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing MCP servers").LogError(ctx, logger)
	}

	rows, err := s.chRepo.ListToolUsageTraces(ctx, repo.ListToolUsageTracesParams{
		GramProjectID:      params.projectID,
		TimeStart:          params.timeStart,
		TimeEnd:            params.timeEnd,
		HostedMCPMatchers:  hostedMCPMatchers,
		MCPServerMatchers:  mcpServerMatchers,
		TargetTypes:        targetTypes,
		HostedToolsetSlugs: payload.HostedToolsetSlugs,
		ShadowServerNames:  payload.ShadowServerNames,
		UserFilters:        userFilters,
		HookSources:        payload.HookSources,
		AccountType:        conv.PtrValOr(payload.AccountType, ""),
		Statuses:           statuses,
		Query:              conv.PtrValOr(payload.Query, ""),
		Filters:            toRepoAttributeFilters(payload.Filters),
		SortOrder:          params.sortOrder,
		CursorTimeUnixNano: cursorTimeUnixNano,
		CursorID:           cursorID,
		Limit:              params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching tool usage traces").LogError(ctx, logger)
	}

	nextCursor := ""
	if len(rows) > params.limit {
		nextCursor = encodeToolUsageTraceCursor(rows[params.limit-1].StartTimeUnixNano, rows[params.limit-1].ID)
		rows = rows[:params.limit]
	}

	return toToolUsageTracesResult(rows, nextCursor), nil
}

// GetToolUsageFilterOptions returns selectable filter options for target-aware MCP and tool usage metrics.
func (s *Service) GetToolUsageFilterOptions(ctx context.Context, payload *telem_gen.GetToolUsageFilterOptionsPayload) (res *telem_gen.GetToolUsageFilterOptionsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	hostedMCPMatchers, err := s.toolUsageHostedMCPMatchers(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing hosted MCP servers")
	}
	mcpServerMatchers, err := s.toolUsageMCPServerMatchers(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing MCP servers")
	}

	options, err := s.chRepo.GetToolUsageFilterOptions(ctx, repo.GetToolUsageFilterOptionsParams{
		GramProjectID:     authCtx.ProjectID.String(),
		TimeStart:         timeStart,
		TimeEnd:           timeEnd,
		HostedMCPMatchers: hostedMCPMatchers,
		MCPServerMatchers: mcpServerMatchers,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching tool usage filter options")
	}

	return toToolUsageFilterOptionsResult(options, hostedMCPMatchers, payload.OptionTypes), nil
}

func (s *Service) toolUsageHostedMCPMatchers(ctx context.Context, projectID uuid.UUID) ([]repo.HostedMCPMatcher, error) {
	toolsets, err := toolsetsRepo.New(s.db).ListToolsetsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project toolsets: %w", err)
	}

	matchers := make([]repo.HostedMCPMatcher, 0, len(toolsets))
	for _, toolset := range toolsets {
		if !toolset.McpEnabled || !toolset.McpSlug.Valid || toolset.McpSlug.String == "" {
			continue
		}
		matchers = append(matchers, repo.HostedMCPMatcher{
			ToolsetSlug: toolset.Slug,
			ToolsetName: toolset.Name,
			McpSlug:     toolset.McpSlug.String,
		})
	}
	return matchers, nil
}

func (s *Service) toolUsageMCPServerMatchers(ctx context.Context, projectID uuid.UUID) ([]repo.MCPServerMatcher, error) {
	// Include soft-deleted servers: tool_source on telemetry rows is the
	// backend remote/tunneled server id, which outlives the mcp_servers row.
	// Matching against deleted servers keeps a deleted (or recreated) server's
	// historical calls classified as their true type instead of falling through
	// to shadow_mcp_server. The query returns live servers first so a source id
	// shared by a live and a deleted server resolves to the live one.
	servers, err := mcpserversRepo.New(s.db).ListMCPServersForTelemetryByProjectID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project MCP servers: %w", err)
	}

	matchers := make([]repo.MCPServerMatcher, 0, len(servers))
	seen := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		targetType := repo.ToolUsageTargetTypeHostedMCP
		sourceID := ""
		switch {
		case server.TunneledMcpServerID.Valid:
			targetType = repo.ToolUsageTargetTypeTunneledMCP
			sourceID = server.TunneledMcpServerID.UUID.String()
		case server.RemoteMcpServerID.Valid:
			sourceID = server.RemoteMcpServerID.UUID.String()
		default:
			continue
		}

		// Keep the first matcher for a source id (a live server, given the
		// query ordering); drop later duplicates from deleted/recreated rows.
		if _, ok := seen[sourceID]; ok {
			continue
		}
		seen[sourceID] = struct{}{}

		targetID := server.ID.String()
		if server.Slug.Valid && server.Slug.String != "" {
			targetID = server.Slug.String
		}
		targetLabel := targetID
		if server.Name.Valid && server.Name.String != "" {
			targetLabel = server.Name.String
		}

		matchers = append(matchers, repo.MCPServerMatcher{
			SourceID:    sourceID,
			TargetType:  targetType,
			TargetID:    targetID,
			TargetLabel: targetLabel,
		})
	}
	return matchers, nil
}

func encodeToolUsageTraceCursor(startTimeUnixNano int64, id string) string {
	payload := fmt.Sprintf("%d:%s", startTimeUnixNano, id)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeToolUsageTraceCursor(cursor string) (int64, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", fmt.Errorf("decode tool usage trace cursor: %w", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, "", fmt.Errorf("invalid tool usage trace cursor")
	}
	startTimeUnixNano, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("parse tool usage trace cursor timestamp: %w", err)
	}
	return startTimeUnixNano, parts[1], nil
}

func toToolUsageTracesResult(rows []repo.ToolUsageTraceSummary, nextCursor string) *telem_gen.ListToolUsageTracesResult {
	traces := make([]*telem_gen.ToolUsageTraceSummary, 0, len(rows))
	for _, row := range rows {
		trace := &telem_gen.ToolUsageTraceSummary{
			ID:      row.ID,
			TraceID: conv.PtrEmpty(row.TraceID),
			LogGroup: &telem_gen.ToolUsageTraceLogGroup{
				Kind:  telem_gen.ToolUsageTraceLogGroupKind(row.LogGroupKind),
				Value: row.LogGroupValue,
			},
			StartTimeUnixNano: strconv.FormatInt(row.StartTimeUnixNano, 10),
			LogCount:          row.LogCount,
			GramUrn:           row.GramURN,
			ToolName:          row.ToolName,
			TargetType:        telem_gen.ToolUsageTargetType(row.TargetType),
			TargetKind:        telem_gen.ToolUsageTargetKind(row.TargetKind),
			TargetID:          row.TargetID,
			TargetLabel:       row.TargetLabel,
			UserKey:           row.UserKey,
			UserLabel:         row.UserLabel,
			UserKind:          telem_gen.ToolUsageUserKind(row.UserKind),
			HookSource:        row.HookSource,
			EventSource:       row.EventSource,
			HTTPStatusCode:    row.HTTPStatusCode,
			HookStatus:        row.HookStatus,
			BlockReason:       row.BlockReason,
			AccountType:       row.AccountType,
		}
		traces = append(traces, trace)
	}

	return &telem_gen.ListToolUsageTracesResult{
		Traces:     traces,
		NextCursor: conv.PtrEmpty(nextCursor),
	}
}

func toToolUsageFilterOptionsResult(options *repo.ToolUsageFilterOptions, hostedMCPMatchers []repo.HostedMCPMatcher, optionTypes []telem_gen.ToolUsageFilterOptionType) *telem_gen.GetToolUsageFilterOptionsResult {
	includeHostedServers, includeShadowServers, includeUsers := toolUsageFilterOptionTypeSet(optionTypes)
	if options == nil {
		return &telem_gen.GetToolUsageFilterOptionsResult{
			HostedServers: []*telem_gen.ToolUsageHostedServerFilterOption{},
			ShadowServers: []*telem_gen.ToolUsageShadowServerFilterOption{},
			Users:         []*telem_gen.ToolUsageUserFilterOption{},
		}
	}

	hostedServers := make([]*telem_gen.ToolUsageHostedServerFilterOption, 0, len(options.HostedServers))
	if includeHostedServers {
		hostedCounts := make(map[string]int64, len(options.HostedServers))
		for _, row := range options.HostedServers {
			hostedCounts[row.ToolsetSlug] = uint64ToInt64(row.EventCount)
		}

		seen := make(map[string]struct{}, len(hostedMCPMatchers))
		for _, matcher := range hostedMCPMatchers {
			seen[matcher.ToolsetSlug] = struct{}{}
			hostedServers = append(hostedServers, &telem_gen.ToolUsageHostedServerFilterOption{
				ToolsetSlug: matcher.ToolsetSlug,
				ToolsetName: matcher.ToolsetName,
				EventCount:  hostedCounts[matcher.ToolsetSlug],
			})
		}

		for _, row := range options.HostedServers {
			if _, ok := seen[row.ToolsetSlug]; ok {
				continue
			}
			hostedServers = append(hostedServers, &telem_gen.ToolUsageHostedServerFilterOption{
				ToolsetSlug: row.ToolsetSlug,
				ToolsetName: row.ToolsetSlug,
				EventCount:  uint64ToInt64(row.EventCount),
			})
		}
	}

	shadowServers := make([]*telem_gen.ToolUsageShadowServerFilterOption, 0, len(options.ShadowServers))
	if includeShadowServers {
		for _, row := range options.ShadowServers {
			shadowServers = append(shadowServers, &telem_gen.ToolUsageShadowServerFilterOption{
				ServerName: row.ServerName,
				EventCount: uint64ToInt64(row.EventCount),
			})
		}
	}

	users := make([]*telem_gen.ToolUsageUserFilterOption, 0, len(options.Users))
	if includeUsers {
		for _, row := range options.Users {
			users = append(users, &telem_gen.ToolUsageUserFilterOption{
				UserKey:    row.UserKey,
				UserLabel:  row.UserLabel,
				UserKind:   telem_gen.ToolUsageUserKind(row.UserKind),
				EventCount: uint64ToInt64(row.EventCount),
			})
		}
	}

	return &telem_gen.GetToolUsageFilterOptionsResult{
		HostedServers: hostedServers,
		ShadowServers: shadowServers,
		Users:         users,
	}
}

func toolUsageFilterOptionTypeSet(optionTypes []telem_gen.ToolUsageFilterOptionType) (includeHostedServers bool, includeShadowServers bool, includeUsers bool) {
	if len(optionTypes) == 0 {
		return true, true, true
	}

	for _, optionType := range optionTypes {
		switch string(optionType) {
		case "hosted_servers":
			includeHostedServers = true
		case "shadow_servers":
			includeShadowServers = true
		case "users":
			includeUsers = true
		}
	}

	return includeHostedServers, includeShadowServers, includeUsers
}

func toToolUsageSummaryResult(summary *repo.ToolUsageSummary) *telem_gen.GetToolUsageSummaryResult {
	if summary == nil {
		return emptyToolUsageSummaryResult()
	}

	targets := make([]*telem_gen.ToolUsageTargetSummary, 0, len(summary.Targets))
	for _, row := range summary.Targets {
		targets = append(targets, &telem_gen.ToolUsageTargetSummary{
			TargetType:   telem_gen.ToolUsageTargetType(row.TargetType),
			TargetKind:   telem_gen.ToolUsageTargetKind(row.TargetKind),
			TargetID:     row.TargetID,
			TargetLabel:  row.TargetLabel,
			EventCount:   uint64ToInt64(row.EventCount),
			UniqueTools:  uint64ToInt64(row.UniqueTools),
			SuccessCount: uint64ToInt64(row.SuccessCount),
			FailureCount: uint64ToInt64(row.FailureCount),
			FailureRate:  row.FailureRate,
		})
	}

	users := make([]*telem_gen.ToolUsageUserSummary, 0, len(summary.Users))
	for _, row := range summary.Users {
		users = append(users, &telem_gen.ToolUsageUserSummary{
			UserKey:      row.UserKey,
			UserLabel:    row.UserLabel,
			UserKind:     telem_gen.ToolUsageUserKind(row.UserKind),
			EventCount:   uint64ToInt64(row.EventCount),
			UniqueTools:  uint64ToInt64(row.UniqueTools),
			SuccessCount: uint64ToInt64(row.SuccessCount),
			FailureCount: uint64ToInt64(row.FailureCount),
			FailureRate:  row.FailureRate,
		})
	}

	targetTimeSeries := make([]*telem_gen.ToolUsageTargetTimeSeriesPoint, 0, len(summary.TargetTimeSeries))
	for _, row := range summary.TargetTimeSeries {
		targetTimeSeries = append(targetTimeSeries, &telem_gen.ToolUsageTargetTimeSeriesPoint{
			BucketStartNs: strconv.FormatInt(row.BucketStartNs, 10),
			TargetType:    telem_gen.ToolUsageTargetType(row.TargetType),
			TargetKind:    telem_gen.ToolUsageTargetKind(row.TargetKind),
			TargetID:      row.TargetID,
			TargetLabel:   row.TargetLabel,
			EventCount:    uint64ToInt64(row.EventCount),
			FailureCount:  uint64ToInt64(row.FailureCount),
		})
	}

	userTimeSeries := make([]*telem_gen.ToolUsageUserTimeSeriesPoint, 0, len(summary.UserTimeSeries))
	for _, row := range summary.UserTimeSeries {
		userTimeSeries = append(userTimeSeries, &telem_gen.ToolUsageUserTimeSeriesPoint{
			BucketStartNs: strconv.FormatInt(row.BucketStartNs, 10),
			UserKey:       row.UserKey,
			UserLabel:     row.UserLabel,
			UserKind:      telem_gen.ToolUsageUserKind(row.UserKind),
			EventCount:    uint64ToInt64(row.EventCount),
			FailureCount:  uint64ToInt64(row.FailureCount),
		})
	}

	usersByTarget := make([]*telem_gen.ToolUsageUsersByTargetRow, 0, len(summary.UsersByTarget))
	for _, row := range summary.UsersByTarget {
		usersByTarget = append(usersByTarget, &telem_gen.ToolUsageUsersByTargetRow{
			TargetType:   telem_gen.ToolUsageTargetType(row.TargetType),
			TargetKind:   telem_gen.ToolUsageTargetKind(row.TargetKind),
			TargetID:     row.TargetID,
			TargetLabel:  row.TargetLabel,
			UserKey:      row.UserKey,
			UserLabel:    row.UserLabel,
			UserKind:     telem_gen.ToolUsageUserKind(row.UserKind),
			EventCount:   uint64ToInt64(row.EventCount),
			FailureCount: uint64ToInt64(row.FailureCount),
		})
	}

	targetToolBreakdown := make([]*telem_gen.ToolUsageTargetToolBreakdownRow, 0, len(summary.TargetToolBreakdown))
	for _, row := range summary.TargetToolBreakdown {
		targetToolBreakdown = append(targetToolBreakdown, &telem_gen.ToolUsageTargetToolBreakdownRow{
			TargetType:   telem_gen.ToolUsageTargetType(row.TargetType),
			TargetKind:   telem_gen.ToolUsageTargetKind(row.TargetKind),
			TargetID:     row.TargetID,
			TargetLabel:  row.TargetLabel,
			ToolName:     row.ToolName,
			EventCount:   uint64ToInt64(row.EventCount),
			SuccessCount: uint64ToInt64(row.SuccessCount),
			FailureCount: uint64ToInt64(row.FailureCount),
			FailureRate:  row.FailureRate,
		})
	}

	return &telem_gen.GetToolUsageSummaryResult{
		Totals: &telem_gen.ToolUsageTotals{
			EventCount:    uint64ToInt64(summary.Totals.EventCount),
			SuccessCount:  uint64ToInt64(summary.Totals.SuccessCount),
			FailureCount:  uint64ToInt64(summary.Totals.FailureCount),
			FailureRate:   summary.Totals.FailureRate,
			UniqueTools:   uint64ToInt64(summary.Totals.UniqueTools),
			UniqueUsers:   uint64ToInt64(summary.Totals.UniqueUsers),
			UniqueTargets: uint64ToInt64(summary.Totals.UniqueTargets),
		},
		Targets:             targets,
		Users:               users,
		TargetTimeSeries:    targetTimeSeries,
		UserTimeSeries:      userTimeSeries,
		UsersByTarget:       usersByTarget,
		TargetToolBreakdown: targetToolBreakdown,
	}
}

func emptyToolUsageSummaryResult() *telem_gen.GetToolUsageSummaryResult {
	return &telem_gen.GetToolUsageSummaryResult{
		Totals: &telem_gen.ToolUsageTotals{
			EventCount:    0,
			SuccessCount:  0,
			FailureCount:  0,
			FailureRate:   0,
			UniqueTools:   0,
			UniqueUsers:   0,
			UniqueTargets: 0,
		},
		Targets:             []*telem_gen.ToolUsageTargetSummary{},
		Users:               []*telem_gen.ToolUsageUserSummary{},
		TargetTimeSeries:    []*telem_gen.ToolUsageTargetTimeSeriesPoint{},
		UserTimeSeries:      []*telem_gen.ToolUsageUserTimeSeriesPoint{},
		UsersByTarget:       []*telem_gen.ToolUsageUsersByTargetRow{},
		TargetToolBreakdown: []*telem_gen.ToolUsageTargetToolBreakdownRow{},
	}
}

// ListHooksTraces retrieves hook trace summaries with pagination and filtering.
// Uses materialized columns for efficient querying while accessing user_email from JSON.
func (s *Service) ListHooksTraces(ctx context.Context, payload *telem_gen.ListHooksTracesPayload) (res *telem_gen.ListHooksTracesResult, err error) {
	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, &payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	// Convert attribute filters
	attributeFilters := toRepoAttributeFilters(payload.Filters)

	// Query with limit+1 to detect if there are more results
	items, err := s.chRepo.ListHooksTraces(ctx, repo.ListHooksTracesParams{
		GramProjectID:  params.projectID,
		TimeStart:      params.timeStart,
		TimeEnd:        params.timeEnd,
		Filters:        attributeFilters,
		TypesToInclude: payload.TypesToInclude,
		SortOrder:      params.sortOrder,
		Cursor:         params.cursor,
		Limit:          params.limit + 1,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "error listing hooks traces", attr.SlogError(err))
		return nil, oops.E(oops.CodeUnexpected, err, "error listing hooks traces")
	}

	// Compute next cursor using limit+1 pattern
	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = &items[params.limit-1].TraceID
		items = items[:params.limit]
	}

	// Convert repo models to Goa types
	traces := make([]*telem_gen.HookTraceSummary, len(items))
	for i, item := range items {
		traces[i] = &telem_gen.HookTraceSummary{
			TraceID:           item.TraceID,
			StartTimeUnixNano: strconv.FormatInt(item.StartTimeUnixNano, 10),
			LogCount:          item.LogCount,
			HookStatus:        item.HookStatus,
			BlockReason:       item.BlockReason,
			GramUrn:           item.GramURN,
			ToolName:          item.ToolName,
			ToolSource:        item.ToolSource,
			EventSource:       item.EventSource,
			UserEmail:         item.UserEmail,
			HookSource:        item.HookSource,
			SkillName:         item.SkillName,
		}
	}

	return &telem_gen.ListHooksTracesResult{
		Traces:     traces,
		NextCursor: nextCursor,
	}, nil
}

// GetChatMetricsByIDs retrieves token and cost metrics for specific chat IDs.
// This is used by the chat service to enrich chat overview data with metrics from ClickHouse.
func (s *Service) GetChatMetricsByIDs(ctx context.Context, projectID string, chatIDs []string) (map[string]repo.ChatMetricsRow, error) {
	if s.chRepo == nil {
		return make(map[string]repo.ChatMetricsRow), nil
	}

	result, err := s.chRepo.GetChatMetricsByIDs(ctx, repo.GetChatMetricsByIDsParams{
		GramProjectID: projectID,
		ChatIDs:       chatIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("get chat metrics by ids: %w", err)
	}
	return result, nil
}

// GetClaudeTurnUsageByChatIDs retrieves per-turn Claude Code usage for specific chat IDs.
// This is used by the chat service to enrich chat detail responses from ClickHouse.
func (s *Service) GetClaudeTurnUsageByChatIDs(ctx context.Context, projectID string, chatIDs []string) (map[string][]repo.ClaudeTurnUsageRow, error) {
	if s.chRepo == nil {
		usageByChatID := make(map[string][]repo.ClaudeTurnUsageRow, len(chatIDs))
		for _, chatID := range chatIDs {
			usageByChatID[chatID] = []repo.ClaudeTurnUsageRow{}
		}
		return usageByChatID, nil
	}

	result, err := s.chRepo.GetClaudeTurnUsageByChatIDs(ctx, repo.GetClaudeTurnUsageByChatIDsParams{
		GramProjectID: projectID,
		ChatIDs:       chatIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("get Claude turn usage by chat ids: %w", err)
	}
	return result, nil
}

// GetClaudeToolUsageByChatIDs retrieves per-tool Claude Code input/result byte sizes for specific chat IDs.
// This is used by the chat service to enrich chat detail rows from ClickHouse.
func (s *Service) GetClaudeToolUsageByChatIDs(ctx context.Context, projectID string, chatIDs []string) (map[string][]repo.ClaudeToolUsageRow, error) {
	if s.chRepo == nil {
		usageByChatID := make(map[string][]repo.ClaudeToolUsageRow, len(chatIDs))
		for _, chatID := range chatIDs {
			usageByChatID[chatID] = []repo.ClaudeToolUsageRow{}
		}
		return usageByChatID, nil
	}

	result, err := s.chRepo.GetClaudeToolUsageByChatIDs(ctx, repo.GetClaudeTurnUsageByChatIDsParams{
		GramProjectID: projectID,
		ChatIDs:       chatIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("get Claude tool usage by chat ids: %w", err)
	}
	return result, nil
}

// toTopUsersFromPG converts PostgreSQL top users to API type.
func toTopUsersFromPG(users []chatRepo.GetTopUsersByMessagesRow) []*telem_gen.TopUser {
	if users == nil {
		return []*telem_gen.TopUser{}
	}
	result := make([]*telem_gen.TopUser, len(users))
	for i, u := range users {
		userID := ""
		if u.UserID.Valid {
			userID = u.UserID.String
		}
		result[i] = &telem_gen.TopUser{
			UserID:        userID,
			UserType:      u.UserType,
			ActivityCount: u.MessageCount,
		}
	}
	return result
}

// toLLMClientUsageFromPG converts PostgreSQL LLM client breakdown to API type.
func toLLMClientUsageFromPG(clients []chatRepo.GetLLMClientBreakdownByMessagesRow) []*telem_gen.LLMClientUsage {
	if clients == nil {
		return []*telem_gen.LLMClientUsage{}
	}
	result := make([]*telem_gen.LLMClientUsage, len(clients))
	for i, c := range clients {
		result[i] = &telem_gen.LLMClientUsage{
			ClientName:    c.ClientName,
			ActivityCount: c.MessageCount,
		}
	}
	return result
}
