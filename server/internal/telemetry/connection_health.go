package telemetry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	"github.com/speakeasy-api/gram/server/internal/authz"
	metamcpRepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgsRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsRepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	usersessionsRepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	// ConnectionHealthDefaultWindow is the window a health read covers when
	// the caller names none.
	ConnectionHealthDefaultWindow = 7 * 24 * time.Hour

	// ConnectionHealthMaxWindow bounds every health read.
	ConnectionHealthMaxWindow = 30 * 24 * time.Hour

	// ConnectionHotspotDefaultMinAttempts keeps one-off attempts out of the
	// ranking unless the caller lowers the floor.
	ConnectionHotspotDefaultMinAttempts = 5

	// ConnectionHotspotMaxLimit bounds the hotspots one read returns.
	ConnectionHotspotMaxLimit = 25

	// connectionHotspotMaxMinAttempts bounds the floor a caller may set.
	connectionHotspotMaxMinAttempts = 100000

	// connectionHotspotReasons is how many reasons each hotspot names.
	connectionHotspotReasons = 5
)

// A window must lie between the Unix epoch and 2100, inside what both the
// nanosecond event times and the rollup's second-resolution buckets can
// represent.
var (
	connectionHealthEarliest = time.Unix(0, 0).UTC()
	connectionHealthLatest   = time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
)

// Connection attempt stages a gateway records today.
const (
	ConnectionStageInboundAuthorize = "inbound_authorize"
	ConnectionStageInboundToken     = "inbound_token"
	ConnectionStageDiscoveryListing = "discovery_listing"
	ConnectionStageDiscoveryMember  = "discovery_member"
	ConnectionStageUpstreamCallback = "upstream_callback"
	ConnectionStageUpstreamRefresh  = "upstream_refresh"
)

// connectionStages are the recorded stages, in the order a connection meets
// them. Each counts its own attempts.
var connectionStages = []string{
	ConnectionStageInboundAuthorize, ConnectionStageInboundToken, ConnectionStageUpstreamCallback,
	ConnectionStageUpstreamRefresh, ConnectionStageDiscoveryListing, ConnectionStageDiscoveryMember,
}

func validConnectionStage(stage string) bool {
	return slices.Contains(connectionStages, stage)
}

var (
	// ErrConnectionHealthGatewayNotFound means the gateway is not in the
	// project.
	ErrConnectionHealthGatewayNotFound = errors.New("gateway not found in this project")

	// ErrConnectionHealthInvalid means the request is outside its bounds.
	ErrConnectionHealthInvalid = errors.New("invalid connection health request")

	// ErrConnectionHealthKeyNotFound means a key names a caller, provider, or
	// client the reader may not see, which is answered as if it had none.
	ErrConnectionHealthKeyNotFound = errors.New("no visible hotspot with this key")
)

// ConnectionHealthCoverage names a stage the gateway does not record, so a
// reader treats its absence as unknown rather than healthy.
type ConnectionHealthCoverage struct {
	Stage string
	Note  string
}

// connectionHealthNotRecorded lists what no producer writes yet. Its counts
// are unknown, never zero.
var connectionHealthNotRecorded = []ConnectionHealthCoverage{
	{Stage: "cimd_fetch", Note: "Client metadata document fetches are not recorded yet."},
	{Stage: "upstream_refresh_background", Note: "Scheduled and manual provider refreshes are not attributed to a gateway."},
	{Stage: "upstream_refresh_capture_limit", Note: "At most 64 provider refreshes are recorded per gateway request; any beyond that are not counted."},
	{Stage: "upstream_endpoint_contact", Note: "Requests to provider endpoints are not counted: a callback or refresh is one logical attempt, whether or not the provider was contacted."},
}

// connectionHealthScope says what the recorded stages observe.
const connectionHealthScope = "Sign-in and token requests this gateway answered are recorded, not whether a redirect arrived, sign-in finished, or a token was used. Provider sign-in callbacks are recorded when the state names this gateway; a stored credential is not proof the person returned to their client. Provider refreshes are recorded only when a request to this gateway caused them. Only Direct tools/list listings are recorded: Progressive discovery and organization gateways are not observed."

// ConnectionHealth reads one gateway's connection attempts and names their
// callers, providers, and clients only from rows the caller may see. The
// management API and the Platform MCP both read through it, so both apply
// one policy: project:read and log:read on the project, logs enabled, and
// mcp:read on a gateway in the project, all checked before any attempt is
// read.
type ConnectionHealth struct {
	db          *pgxpool.Pool
	ch          *repo.Queries
	logsEnabled FeatureChecker
	authz       *authz.Engine
	agents      *agentmanagement.Authorizer
}

// NewConnectionHealth builds the connection health reader.
func NewConnectionHealth(db *pgxpool.Pool, chConn clickhouse.Conn, logsEnabled FeatureChecker, engine *authz.Engine) *ConnectionHealth {
	return &ConnectionHealth{db: db, ch: repo.New(chConn), logsEnabled: logsEnabled, authz: engine, agents: agentmanagement.NewAuthorizer(engine)}
}

// ConnectionHealthTarget is the gateway a read covers, in the project the
// caller was authorized for.
type ConnectionHealthTarget struct {
	ProjectID      uuid.UUID
	OrganizationID string
	GatewayID      uuid.UUID
}

// ConnectionHealthWindow is exactly [Start, End).
type ConnectionHealthWindow struct {
	Start time.Time
	End   time.Time
}

// ParseConnectionHealthWindow reads RFC 3339 bounds, defaulting to the 7 days
// before now, and rejects a window longer than 30 days.
func ParseConnectionHealthWindow(from, to string, now time.Time) (ConnectionHealthWindow, error) {
	end := now.UTC()
	if to != "" {
		parsed, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return ConnectionHealthWindow{Start: time.Time{}, End: time.Time{}}, fmt.Errorf("%w: to must be RFC 3339", ErrConnectionHealthInvalid)
		}
		end = parsed.UTC()
	}
	start := end.Add(-ConnectionHealthDefaultWindow)
	if from != "" {
		parsed, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return ConnectionHealthWindow{Start: time.Time{}, End: time.Time{}}, fmt.Errorf("%w: from must be RFC 3339", ErrConnectionHealthInvalid)
		}
		start = parsed.UTC()
	}
	switch {
	case start.Before(connectionHealthEarliest) || end.After(connectionHealthLatest):
		return ConnectionHealthWindow{Start: time.Time{}, End: time.Time{}}, fmt.Errorf("%w: the window must lie between 1970 and 2100", ErrConnectionHealthInvalid)
	case !start.Before(end):
		return ConnectionHealthWindow{Start: time.Time{}, End: time.Time{}}, fmt.Errorf("%w: from must be before to", ErrConnectionHealthInvalid)
	case end.Sub(start) > ConnectionHealthMaxWindow:
		return ConnectionHealthWindow{Start: time.Time{}, End: time.Time{}}, fmt.Errorf("%w: the window may span at most 30 days; for a full 30 days, give both from and to", ErrConnectionHealthInvalid)
	}
	return ConnectionHealthWindow{Start: start, End: end}, nil
}

// ConnectionOutcomeCount is a stage's attempts with one outcome.
type ConnectionOutcomeCount struct {
	Outcome string
	Count   uint64
}

// ConnectionStageBucket is one timeline bucket's outcomes.
type ConnectionStageBucket struct {
	Start    time.Time
	Outcomes []ConnectionOutcomeCount
}

// ConnectionStageSummary is one recorded stage over the window. Attempts is
// the stage's denominator; units from different stages are never combined.
type ConnectionStageSummary struct {
	Stage     string
	Attempts  uint64
	Outcomes  []ConnectionOutcomeCount
	Retries   uint64
	Recovered uint64
	LastSeen  time.Time
	Buckets   []ConnectionStageBucket
	// LastCodeIssued is the latest authorization-code request's start whose
	// token response was written. Later refreshes do not change this time.
	LastCodeIssued time.Time
}

// ConnectionHealthSummary is a gateway's recorded stages over a window, with
// what is not recorded.
type ConnectionHealthSummary struct {
	Window          ConnectionHealthWindow
	IntervalSeconds int64
	Stages          []ConnectionStageSummary
	NotRecorded     []ConnectionHealthCoverage
	Scope           string
}

// ConnectionHealthKey narrows a summary to one hotspot: a stage's caller ×
// provider × client, as raw attempt values. An empty value matches attempts
// that recorded none, such as an anonymous caller or a hosted member.
type ConnectionHealthKey struct {
	Stage      string
	Subject    string
	ProviderID string
	ClientKey  string
}

// maxConnectionKeyLength bounds each raw key value a caller may filter by.
const maxConnectionKeyLength = 512

// validate accepts only values an attempt can record: a session subject URN,
// a provider ID, and a registered client ID or recorded client kind.
func (k ConnectionHealthKey) validate() error {
	if !validConnectionStage(k.Stage) {
		return fmt.Errorf("%w: key stage must be one of %s", ErrConnectionHealthInvalid, strings.Join(connectionStages, ", "))
	}
	if len(k.Subject) > maxConnectionKeyLength || len(k.ClientKey) > maxConnectionKeyLength {
		return fmt.Errorf("%w: key values are too long", ErrConnectionHealthInvalid)
	}
	if k.Subject != "" {
		if _, err := urn.ParseSessionSubject(k.Subject); err != nil {
			return fmt.Errorf("%w: key subject must be a caller URN or empty", ErrConnectionHealthInvalid)
		}
	}
	if k.ProviderID != "" {
		if _, err := uuid.Parse(k.ProviderID); err != nil {
			return fmt.Errorf("%w: key provider_id must be a UUID or empty", ErrConnectionHealthInvalid)
		}
	}
	if _, err := uuid.Parse(k.ClientKey); err != nil && !recordedClientKind(k.ClientKey) {
		return fmt.Errorf("%w: key client must be a client ID, a recorded client kind, or empty", ErrConnectionHealthInvalid)
	}
	return nil
}

// recordedClientKind reports a client key the gateway records when no
// registered client is known.
func recordedClientKind(raw string) bool {
	switch raw {
	case "", "anonymous", "unregistered_client", "unverified_client", "unknown":
		return true
	}
	kind, ok := strings.CutPrefix(raw, "credential:")
	return ok && credentialLabel(kind) != "Unknown client"
}

// Summary counts the gateway's attempts per recorded stage and outcome, with
// a timeline in UTC-aligned buckets. With a key, it counts only that
// hotspot's attempts, in its stage alone, so its denominator is its own.
func (h *ConnectionHealth) Summary(ctx context.Context, target ConnectionHealthTarget, window ConnectionHealthWindow, key *ConnectionHealthKey) (ConnectionHealthSummary, error) {
	var none ConnectionHealthSummary
	if key != nil {
		if err := key.validate(); err != nil {
			return none, err
		}
	}
	gateway, err := h.gateway(ctx, target)
	if err != nil {
		return none, err
	}
	if key != nil {
		// A key is answered only when the caller may see every part of it, as
		// the hotspot labels would show it; how much traffic it has is not a
		// condition.
		labels, err := h.resolveLabels(ctx, target, gateway.UserSessionIssuerID, []repo.GatewayConnectionHotspot{{Subject: key.Subject, ProviderID: key.ProviderID, ClientKey: key.ClientKey, Attempts: 0, Failed: 0, Blocked: 0, Outcomes: nil, LastSeenUnixNano: 0}})
		if err != nil {
			return none, err
		}
		if !labels.subject(key.Subject).Selectable || !labels.provider(key.ProviderID).Selectable || !labels.client(key.ClientKey).Selectable {
			return none, ErrConnectionHealthKeyNotFound
		}
	}
	stageNames := connectionStages
	var repoKey *repo.GatewayConnectionKey
	if key != nil {
		stageNames = []string{key.Stage}
		repoKey = &repo.GatewayConnectionKey{Stage: key.Stage, Subject: key.Subject, ProviderID: key.ProviderID, ClientKey: key.ClientKey}
	}
	timeStart, timeEnd := window.Start.UnixNano(), window.End.UnixNano()
	interval := discoveryFailureInterval(timeStart, timeEnd)
	rows, err := h.ch.GatewayConnectionAttempts(ctx, repo.GatewayConnectionAttemptsParams{
		GramProjectID:   target.ProjectID.String(),
		MetaMCPServerID: target.GatewayID.String(),
		TimeStart:       timeStart,
		TimeEnd:         timeEnd,
		IntervalSeconds: interval,
		Key:             repoKey,
	})
	if err != nil {
		return none, fmt.Errorf("read gateway connection attempts: %w", err)
	}

	intervalNanos := interval * int64(time.Second)
	first := timeStart / intervalNanos * intervalNanos
	stages := make([]ConnectionStageSummary, 0, len(stageNames))
	for _, stage := range stageNames {
		summary := ConnectionStageSummary{Stage: stage, Attempts: 0, Outcomes: []ConnectionOutcomeCount{}, Retries: 0, Recovered: 0, LastSeen: time.Time{}, Buckets: []ConnectionStageBucket{}, LastCodeIssued: time.Time{}}
		totals := map[string]uint64{}
		perBucket := map[int64]map[string]uint64{}
		for _, row := range rows {
			if row.Stage != stage {
				continue
			}
			summary.Attempts += row.Attempts
			summary.Retries += row.Retries
			summary.Recovered += row.Recovered
			if seen := time.Unix(0, row.LastSeenUnixNano).UTC(); row.LastSeenUnixNano > 0 && seen.After(summary.LastSeen) {
				summary.LastSeen = seen
			}
			if seen := time.Unix(0, row.LastSeenUnixNano).UTC(); stage == ConnectionStageInboundToken && row.Outcome == "code_issued" && row.LastSeenUnixNano > 0 && seen.After(summary.LastCodeIssued) {
				summary.LastCodeIssued = seen
			}
			totals[row.Outcome] += row.Attempts
			if perBucket[row.BucketTimeUnixNano] == nil {
				perBucket[row.BucketTimeUnixNano] = map[string]uint64{}
			}
			perBucket[row.BucketTimeUnixNano][row.Outcome] += row.Attempts
		}
		summary.Outcomes = outcomeCounts(totals)
		for bucket := first; bucket < timeEnd; bucket += intervalNanos {
			summary.Buckets = append(summary.Buckets, ConnectionStageBucket{Start: time.Unix(0, bucket).UTC(), Outcomes: outcomeCounts(perBucket[bucket])})
		}
		stages = append(stages, summary)
	}
	return ConnectionHealthSummary{Window: window, IntervalSeconds: interval, Stages: stages, NotRecorded: connectionHealthNotRecorded, Scope: connectionHealthScope}, nil
}

// ConnectionHotspotQuery selects and ranks hotspots within one stage.
type ConnectionHotspotQuery struct {
	Stage       string
	ByRate      bool
	MinAttempts int
	Limit       int
}

// ConnectionLabel names a caller, provider, or client. ID is set only when
// the caller may see the named row; otherwise only a safe label is given.
// Key is the raw value to filter a summary by, set when Selectable: for a
// visible row, an empty key, or a recorded client kind, never for a row the
// caller may not see.
type ConnectionLabel struct {
	Kind       string
	Label      string
	ID         string
	Key        string
	Selectable bool
}

func namedLabel(kind, label, raw string) ConnectionLabel {
	return ConnectionLabel{Kind: kind, Label: label, ID: raw, Key: raw, Selectable: true}
}

func keyedLabel(kind, label, raw string) ConnectionLabel {
	return ConnectionLabel{Kind: kind, Label: label, ID: "", Key: raw, Selectable: true}
}

func hiddenLabel(kind, label string) ConnectionLabel {
	return ConnectionLabel{Kind: kind, Label: label, ID: "", Key: "", Selectable: false}
}

// ConnectionReasonCount is a hotspot's attempts with one sanitized reason.
type ConnectionReasonCount struct {
	Reason string
	Count  uint64
}

// ConnectionHotspot is one caller × provider × client within a stage.
// Attempts is the denominator for both Failed and Blocked.
type ConnectionHotspot struct {
	Subject  ConnectionLabel
	Provider ConnectionLabel
	Client   ConnectionLabel
	Attempts uint64
	Failed   uint64
	Blocked  uint64
	Outcomes []ConnectionOutcomeCount
	Reasons  []ConnectionReasonCount
	LastSeen time.Time
}

// ConnectionHotspots is a ranked page of hotspots.
type ConnectionHotspots struct {
	Window      ConnectionHealthWindow
	Stage       string
	ByRate      bool
	MinAttempts int
	Hotspots    []ConnectionHotspot
}

// Hotspots ranks a stage's caller × provider × client keys by failed attempts
// or by failure rate, among keys with at least MinAttempts attempts.
func (h *ConnectionHealth) Hotspots(ctx context.Context, target ConnectionHealthTarget, window ConnectionHealthWindow, query ConnectionHotspotQuery) (ConnectionHotspots, error) {
	var none ConnectionHotspots
	if !validConnectionStage(query.Stage) {
		return none, fmt.Errorf("%w: stage must be one of %s", ErrConnectionHealthInvalid, strings.Join(connectionStages, ", "))
	}
	if query.MinAttempts == 0 {
		query.MinAttempts = ConnectionHotspotDefaultMinAttempts
	}
	if query.Limit == 0 {
		query.Limit = ConnectionHotspotMaxLimit
	}
	if query.MinAttempts < 1 || query.MinAttempts > connectionHotspotMaxMinAttempts || query.Limit < 1 || query.Limit > ConnectionHotspotMaxLimit {
		return none, fmt.Errorf("%w: min_attempts must be 1 to %d and limit 1 to %d", ErrConnectionHealthInvalid, connectionHotspotMaxMinAttempts, ConnectionHotspotMaxLimit)
	}
	gateway, err := h.gateway(ctx, target)
	if err != nil {
		return none, err
	}
	rows, err := h.ch.GatewayConnectionHotspots(ctx, repo.GatewayConnectionHotspotsParams{
		GramProjectID:   target.ProjectID.String(),
		MetaMCPServerID: target.GatewayID.String(),
		TimeStart:       window.Start.UnixNano(),
		TimeEnd:         window.End.UnixNano(),
		Stage:           query.Stage,
		ByRate:          query.ByRate,
		MinAttempts:     uint64(query.MinAttempts),
		Limit:           uint64(query.Limit),
	})
	if err != nil {
		return none, fmt.Errorf("read gateway connection hotspots: %w", err)
	}
	labels, err := h.resolveLabels(ctx, target, gateway.UserSessionIssuerID, rows)
	if err != nil {
		return none, err
	}
	hotspots := make([]ConnectionHotspot, 0, len(rows))
	for _, row := range rows {
		totals, reasons := map[string]uint64{}, map[string]uint64{}
		for _, outcome := range row.Outcomes {
			totals[outcome.Outcome] += outcome.Count
			if outcome.Reason != "" {
				reasons[outcome.Reason] += outcome.Count
			}
		}
		hotspot := ConnectionHotspot{
			Subject:  labels.subject(row.Subject),
			Provider: labels.provider(row.ProviderID),
			Client:   labels.client(row.ClientKey),
			Attempts: row.Attempts,
			Failed:   row.Failed,
			Blocked:  row.Blocked,
			Outcomes: outcomeCounts(totals),
			Reasons:  topReasons(reasons),
			LastSeen: time.Time{},
		}
		if row.LastSeenUnixNano > 0 {
			hotspot.LastSeen = time.Unix(0, row.LastSeenUnixNano).UTC()
		}
		hotspots = append(hotspots, hotspot)
	}
	return ConnectionHotspots{Window: window, Stage: query.Stage, ByRate: query.ByRate, MinAttempts: query.MinAttempts, Hotspots: hotspots}, nil
}

// gateway authorizes the read and loads the gateway. A gateway that is not in
// the project and one the caller may not read are both not found.
func (h *ConnectionHealth) gateway(ctx context.Context, target ConnectionHealthTarget) (metamcpRepo.MetaMcpServer, error) {
	var none metamcpRepo.MetaMcpServer
	// project:read covers the provider and client names, as it does where
	// those are listed; log:read covers the attempts themselves.
	for _, scope := range []authz.Scope{authz.ScopeProjectRead, authz.ScopeLogRead} {
		if err := h.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: target.ProjectID.String(), Dimensions: nil}); err != nil {
			return none, fmt.Errorf("authorize %s: %w", scope, err)
		}
	}
	enabled, err := h.logsEnabled(ctx, target.OrganizationID)
	if err != nil {
		return none, fmt.Errorf("check logs enabled: %w", err)
	}
	if !enabled {
		return none, telemetryerrs.ErrLogsDisabled
	}
	gateway, err := metamcpRepo.New(h.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcpRepo.GetMetaMCPServerByIDAndProjectIDParams{ID: target.GatewayID, ProjectID: target.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return none, ErrConnectionHealthGatewayNotFound
	}
	if err != nil {
		return none, fmt.Errorf("load gateway: %w", err)
	}
	if err := h.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, target.GatewayID.String(), target.ProjectID.String())); err != nil {
		if denied, ok := errors.AsType[*oops.ShareableError](err); ok && denied.Code == oops.CodeForbidden {
			return none, ErrConnectionHealthGatewayNotFound
		}
		return none, fmt.Errorf("authorize gateway read: %w", err)
	}
	return gateway, nil
}

// connectionLabels holds the names the caller may see, keyed by raw value.
type connectionLabels struct {
	people    map[string]string
	agents    map[string]string
	providers map[string]string
	clients   map[string]string
}

// resolveLabels names hotspot keys in one bulk read per kind: people who are
// members of the organization, agents the caller may read, providers the
// project can see, and clients registered with the gateway's sign-in server.
func (h *ConnectionHealth) resolveLabels(ctx context.Context, target ConnectionHealthTarget, issuerID uuid.NullUUID, rows []repo.GatewayConnectionHotspot) (connectionLabels, error) {
	labels := connectionLabels{people: map[string]string{}, agents: map[string]string{}, providers: map[string]string{}, clients: map[string]string{}}
	var userIDs []string
	var agentIDs, providerIDs, clientIDs []uuid.UUID
	for _, row := range rows {
		subject, err := urn.ParseSessionSubject(row.Subject)
		switch {
		case err != nil:
		case subject.Kind == urn.SessionSubjectKindUser && !slices.Contains(userIDs, subject.ID):
			userIDs = append(userIDs, subject.ID)
		case subject.Kind == urn.SessionSubjectKindAgent:
			if id, err := uuid.Parse(subject.ID); err == nil && !slices.Contains(agentIDs, id) {
				agentIDs = append(agentIDs, id)
			}
		}
		if id, err := uuid.Parse(row.ProviderID); err == nil && !slices.Contains(providerIDs, id) {
			providerIDs = append(providerIDs, id)
		}
		if id, err := uuid.Parse(row.ClientKey); err == nil && !slices.Contains(clientIDs, id) {
			clientIDs = append(clientIDs, id)
		}
	}
	if len(userIDs) > 0 {
		members, err := orgsRepo.New(h.db).FilterOrganizationMemberUserIDs(ctx, orgsRepo.FilterOrganizationMemberUserIDsParams{OrganizationID: target.OrganizationID, UserIds: userIDs})
		if err != nil {
			return labels, fmt.Errorf("filter organization members: %w", err)
		}
		if len(members) > 0 {
			users, err := usersRepo.New(h.db).GetUsersByIDs(ctx, members)
			if err != nil {
				return labels, fmt.Errorf("load people: %w", err)
			}
			for _, user := range users {
				labels.people[user.ID] = firstNonEmpty(user.DisplayName, user.Email)
			}
		}
	}
	if len(agentIDs) > 0 {
		names, err := h.agents.ReadableAgentNames(ctx, h.db, agentIDs)
		if err != nil {
			return labels, fmt.Errorf("load readable agents: %w", err)
		}
		for id, name := range names {
			labels.agents[id.String()] = name
		}
	}
	if len(providerIDs) > 0 {
		providers, err := remotesessionsRepo.New(h.db).ListRemoteSessionIssuerLabelsByIDs(ctx, remotesessionsRepo.ListRemoteSessionIssuerLabelsByIDsParams{
			Ids:            providerIDs,
			ProjectID:      uuid.NullUUID{UUID: target.ProjectID, Valid: true},
			OrganizationID: pgtype.Text{String: target.OrganizationID, Valid: true},
		})
		if err != nil {
			return labels, fmt.Errorf("load providers: %w", err)
		}
		for _, provider := range providers {
			labels.providers[provider.ID.String()] = firstNonEmpty(provider.Name.String, provider.Slug)
		}
	}
	if len(clientIDs) > 0 && issuerID.Valid {
		clients, err := usersessionsRepo.New(h.db).ListUserSessionClientLabelsByIDs(ctx, usersessionsRepo.ListUserSessionClientLabelsByIDsParams{UserSessionIssuerID: issuerID.UUID, Ids: clientIDs})
		if err != nil {
			return labels, fmt.Errorf("load clients: %w", err)
		}
		for _, client := range clients {
			labels.clients[client.ID.String()] = client.ClientName
		}
	}
	return labels, nil
}

// subject names a caller: a person in the organization, or an agent the
// caller may read. Every other caller is named by kind alone.
func (l connectionLabels) subject(raw string) ConnectionLabel {
	if raw == "" {
		return keyedLabel("anonymous", "No signed-in caller", "")
	}
	subject, err := urn.ParseSessionSubject(raw)
	if err != nil {
		return hiddenLabel("unknown", "Unknown caller")
	}
	switch subject.Kind {
	case urn.SessionSubjectKindUser:
		if name, ok := l.people[subject.ID]; ok && name != "" {
			return namedLabel("user", name, raw)
		}
		return hiddenLabel("user", "Unknown person")
	case urn.SessionSubjectKindAgent:
		if name, ok := l.agents[subject.ID]; ok && name != "" {
			return namedLabel("agent", name, raw)
		}
		return hiddenLabel("agent", "Unknown agent")
	case urn.SessionSubjectKindAPIKey:
		return hiddenLabel("apikey", "API key")
	case urn.SessionSubjectKindWorkload:
		return hiddenLabel("workload", "Workload")
	case urn.SessionSubjectKindAnonymous:
		return hiddenLabel("anonymous", "No signed-in caller")
	}
	return hiddenLabel("unknown", "Unknown caller")
}

// provider names an upstream provider the project can see.
func (l connectionLabels) provider(raw string) ConnectionLabel {
	if raw == "" {
		return keyedLabel("none", "No upstream provider", "")
	}
	if name, ok := l.providers[raw]; ok {
		return namedLabel("provider", name, raw)
	}
	return hiddenLabel("provider", "Unknown provider")
}

// client names a client registered with the gateway's sign-in server, or the
// bounded kind recorded when there was none.
func (l connectionLabels) client(raw string) ConnectionLabel {
	if name, ok := l.clients[raw]; ok {
		return namedLabel("registered", name, raw)
	}
	if _, err := uuid.Parse(raw); err == nil {
		return hiddenLabel("unknown", "Unknown client")
	}
	switch {
	case raw == "anonymous":
		return keyedLabel("anonymous", "No sign-in", raw)
	case raw == "unregistered_client":
		return keyedLabel("unregistered", "Unregistered client", raw)
	case raw == "unverified_client":
		return keyedLabel("unverified", "Client not verified", raw)
	case strings.HasPrefix(raw, "credential:") && recordedClientKind(raw):
		return keyedLabel("credential", credentialLabel(strings.TrimPrefix(raw, "credential:")), raw)
	case raw == "unknown" || raw == "":
		return keyedLabel("unknown", "Unknown client", raw)
	}
	return hiddenLabel("unknown", "Unknown client")
}

func credentialLabel(kind string) string {
	switch urn.SessionSubjectKind(kind) {
	case urn.SessionSubjectKindUser:
		return "Signed in without a registered client"
	case urn.SessionSubjectKindAgent:
		return "Agent credential"
	case urn.SessionSubjectKindAPIKey:
		return "API key"
	case urn.SessionSubjectKindWorkload:
		return "Workload credential"
	case urn.SessionSubjectKindAnonymous:
		return "No sign-in"
	}
	return "Unknown client"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// outcomeCounts lists nonzero outcomes in name order.
func outcomeCounts(totals map[string]uint64) []ConnectionOutcomeCount {
	counts := make([]ConnectionOutcomeCount, 0, len(totals))
	for outcome, count := range totals {
		if count > 0 {
			counts = append(counts, ConnectionOutcomeCount{Outcome: outcome, Count: count})
		}
	}
	slices.SortFunc(counts, func(a, b ConnectionOutcomeCount) int { return strings.Compare(a.Outcome, b.Outcome) })
	return counts
}

// topReasons lists the most frequent reasons, ties in name order.
func topReasons(totals map[string]uint64) []ConnectionReasonCount {
	reasons := make([]ConnectionReasonCount, 0, len(totals))
	for reason, count := range totals {
		reasons = append(reasons, ConnectionReasonCount{Reason: reason, Count: count})
	}
	slices.SortFunc(reasons, func(a, b ConnectionReasonCount) int {
		if a.Count != b.Count {
			if a.Count > b.Count {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	return reasons[:min(len(reasons), connectionHotspotReasons)]
}
