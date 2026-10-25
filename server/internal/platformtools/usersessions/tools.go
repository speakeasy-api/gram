package usersessions

import (
	"context"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientcred"
	"github.com/speakeasy-api/gram/server/internal/usersessions/gatewayscope"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/sessionfilter"
)

const maxLimit = 100

// maxSearchLength matches the userSessions.list search limit.
const maxSearchLength = 200

type listInput struct {
	Status                     string   `json:"status,omitempty" jsonschema:"Filter by status: active, expired, revoked, or all. Defaults to live sessions."`
	UserSessionIssuerID        string   `json:"user_session_issuer_id,omitempty" jsonschema:"Filter to one issuer/server id (UUID)."`
	SubjectURN                 string   `json:"subject_urn,omitempty" jsonschema:"Exact subject URN to filter by (e.g. user:<id>)."`
	ClientID                   string   `json:"client_id,omitempty" jsonschema:"Filter to one connecting client id (UUID)."`
	MetaMcpServerID            string   `json:"meta_mcp_server_id,omitempty" jsonschema:"Only sessions issued for this gateway (UUID): those that recorded it and, when none was recorded, those whose consent selection names it."`
	Search                     string   `json:"search,omitempty" jsonschema:"Case-insensitive substring match on the person, API key, workload, client, MCP server, or upstream provider of a session. Workload issuer and assignment labels and upstream provider labels may match without being included in the response."`
	Identities                 []string `json:"identities,omitempty" jsonschema:"Only sessions whose subject URN is one of these."`
	ExcludeIdentities          []string `json:"exclude_identities,omitempty" jsonschema:"Leave out sessions whose subject URN is one of these."`
	IdentityTypes              []string `json:"identity_types,omitempty" jsonschema:"Only sessions whose subject is one of these kinds: user, agent, workload, apikey, anonymous."`
	ExcludeIdentityTypes       []string `json:"exclude_identity_types,omitempty" jsonschema:"Leave out sessions whose subject is one of these kinds."`
	Servers                    []string `json:"servers,omitempty" jsonschema:"Only sessions issued by one of these sign-in servers (issuer UUIDs)."`
	ExcludeServers             []string `json:"exclude_servers,omitempty" jsonschema:"Leave out sessions issued by these sign-in servers (issuer UUIDs)."`
	Clients                    []string `json:"clients,omitempty" jsonschema:"Only sessions from one of these clients: a client UUID, dashboard, or none."`
	ExcludeClients             []string `json:"exclude_clients,omitempty" jsonschema:"Leave out sessions from these clients."`
	ClientRegistrations        []string `json:"client_registrations,omitempty" jsonschema:"Only sessions whose client registered this way: cimd, dcr, assistant, or none."`
	ExcludeClientRegistrations []string `json:"exclude_client_registrations,omitempty" jsonschema:"Leave out sessions whose client registered this way."`
	Providers                  []string `json:"providers,omitempty" jsonschema:"Only sessions admitted to one of these upstream providers: a provider (remote session issuer) UUID, or none for sessions with no admitted provider sign-in."`
	ExcludeProviders           []string `json:"exclude_providers,omitempty" jsonschema:"Leave out sessions admitted to any of these providers: a provider (remote session issuer) UUID, or none to leave out sessions with no admitted provider sign-in."`
	LastUsedWithin             *int     `json:"last_used_within,omitempty" jsonschema:"Only sessions used within this many seconds, inclusive. Sessions with no recorded use never match."`
	LastUsedOlderThan          *int     `json:"last_used_older_than,omitempty" jsonschema:"Only sessions last used more than this many seconds ago. Sessions with no recorded use never match."`
	LastUsedNever              bool     `json:"last_used_never,omitempty" jsonschema:"Only sessions with no recorded use."`
	Cursor                     string   `json:"cursor,omitempty" jsonschema:"Pagination cursor: id of the last item from the previous page."`
	Limit                      int      `json:"limit,omitempty" jsonschema:"Page size (default 50, max 100)."`
}

type getInput struct {
	ID string `json:"id" jsonschema:"The user session id (UUID)."`
}

type listResult struct {
	Items      []*types.UserSession `json:"items"`
	NextCursor *string              `json:"next_cursor,omitempty"`
}

// buildView converts a DB row to the API type. This mirrors mv.BuildUserSessionView
// but is inlined here to avoid an import cycle (mv imports platformtools).
func buildView(row repo.ListUserSessionsByProjectIDRow) *types.UserSession {
	subjectType := string(row.SubjectUrn.Kind)

	var subjectName *string
	switch row.SubjectUrn.Kind {
	case urn.SessionSubjectKindUser:
		if name := conv.FromPGText[string](row.UserDisplayName); name != nil && *name != "" {
			subjectName = name
		} else {
			subjectName = conv.FromPGText[string](row.UserEmail)
		}
	case urn.SessionSubjectKindAPIKey:
		subjectName = conv.FromPGText[string](row.ApiKeyName)
	case urn.SessionSubjectKindAnonymous, urn.SessionSubjectKindAgent:
		// anonymous and agent subjects have no resolved display name
	case urn.SessionSubjectKindWorkload:
		// The session row does not join the workload issuer, so there is no
		// display name; subjectType names the caller.
	}

	var revokedAt *string
	if row.Deleted && row.DeletedAt.Valid {
		s := row.DeletedAt.Time.Format(time.RFC3339)
		revokedAt = &s
	}

	// A session can be issued without a client (the API key and anonymous
	// paths mint one directly), so the column is nullable.
	var clientID *string
	if row.UserSessionClientID.Valid {
		s := row.UserSessionClientID.UUID.String()
		clientID = &s
	}

	credentialKind, declaredAuthMethod := clientcred.ForBoundClient(
		row.UserSessionClientID.Valid,
		row.ClientTokenEndpointAuthMethod,
		row.ClientHasSecret,
	)

	// Null means the session has not been used since the column was introduced,
	// which is distinct from the zero time.
	var lastUsedAt *string
	if row.LastUsedAt.Valid {
		s := row.LastUsedAt.Time.Format(time.RFC3339)
		lastUsedAt = &s
	}

	return &types.UserSession{
		ID:                            row.ID.String(),
		UserSessionIssuerID:           row.UserSessionIssuerID.String(),
		SubjectUrn:                    row.SubjectUrn.String(),
		Jti:                           row.Jti,
		RefreshExpiresAt:              row.RefreshExpiresAt.Time.Format(time.RFC3339),
		ExpiresAt:                     row.ExpiresAt.Time.Format(time.RFC3339),
		CreatedAt:                     row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                     row.UpdatedAt.Time.Format(time.RFC3339),
		IssuerSlug:                    row.IssuerSlug,
		UserSessionClientID:           clientID,
		ClientName:                    conv.FromPGText[string](row.ClientName),
		ClientIDMetadataURI:           conv.FromPGText[string](row.ClientIDMetadataUri),
		ClientCredentialKind:          credentialKind,
		ClientTokenEndpointAuthMethod: declaredAuthMethod,
		SubjectType:                   subjectType,
		SubjectDisplayName:            subjectName,
		// Only a user subject resolves to a users row, so the join leaves this
		// NULL for API key and anonymous subjects.
		SubjectPhotoURL: conv.FromPGText[string](row.UserPhotoUrl),
		RevokedAt:       revokedAt,
		LastUsedAt:      lastUsedAt,
		// The platform tool answers "which sessions exist", not "what is this
		// connection wired to", so it does not pay for the upstream join. Empty
		// rather than nil: the field is required, and absent upstreams is a
		// meaningful answer this caller simply is not computing.
		Upstreams: []*types.UserSessionUpstream{},
		// Not resolved here: labelling a workload needs the workload issuer and
		// agent assignment lookups the management API performs.
		Workload: nil,
	}
}

func projectTenancy(ctx context.Context) (uuid.UUID, string, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return uuid.Nil, "", oops.C(oops.CodeUnauthorized)
	}
	return *authCtx.ProjectID, authCtx.ActiveOrganizationID, nil
}

func parseNullUUID(s string, field string) (uuid.NullUUID, error) {
	if s == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, oops.E(oops.CodeBadRequest, err, "invalid %s", field)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// ListTool returns a page of user sessions for the authenticated project.
type ListTool struct{ db *pgxpool.Pool }

func NewListUserSessionsTool(db *pgxpool.Pool) core.PlatformToolExecutor { return &ListTool{db: db} }

func (t *ListTool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "user-sessions",
		HandlerName: "list",
		Name:        "platform_list_user_sessions",
		Description: "List user sessions (clients connected into this project's MCP toolsets) with optional filters. Read-only.",
		InputSchema: core.BuildInputSchema[listInput](
			core.WithPropertyEnum("status", "active", "expired", "revoked", "all"),
			core.WithPropertyNumberRange("limit", 1, maxLimit),
			core.WithPropertyItemsRange("identities", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("exclude_identities", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("identity_types", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("exclude_identity_types", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("servers", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("exclude_servers", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("clients", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("exclude_clients", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("client_registrations", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("exclude_client_registrations", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("providers", 0, sessionfilter.MaxValues),
			core.WithPropertyItemsRange("exclude_providers", 0, sessionfilter.MaxValues),
			core.WithPropertyNumberRange("last_used_within", 1, sessionfilter.MaxLastUsedSeconds),
			core.WithPropertyNumberRange("last_used_older_than", 1, sessionfilter.MaxLastUsedSeconds),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ListTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	pid, org, err := projectTenancy(ctx)
	if err != nil {
		return err
	}

	var in listInput
	if err := core.DecodeInput(payload, &in); err != nil {
		return err
	}

	limit := int32(50)
	if in.Limit != 0 {
		if in.Limit < 1 || in.Limit > maxLimit {
			return oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and %d", maxLimit)
		}
		limit = int32(in.Limit)
	}

	cursor, err := parseNullUUID(in.Cursor, "cursor")
	if err != nil {
		return err
	}
	if utf8.RuneCountInString(in.Search) > maxSearchLength {
		return oops.E(oops.CodeBadRequest, nil, "search must be at most %d characters", maxSearchLength)
	}

	filter, err := sessionfilter.Parse(sessionfilter.Input{
		Status:                     conv.PtrEmpty(in.Status),
		SubjectURN:                 conv.PtrEmpty(in.SubjectURN),
		IssuerID:                   conv.PtrEmpty(in.UserSessionIssuerID),
		ClientID:                   conv.PtrEmpty(in.ClientID),
		Search:                     conv.PtrEmpty(in.Search),
		Identities:                 in.Identities,
		ExcludeIdentities:          in.ExcludeIdentities,
		IdentityTypes:              in.IdentityTypes,
		ExcludeIdentityTypes:       in.ExcludeIdentityTypes,
		Servers:                    in.Servers,
		ExcludeServers:             in.ExcludeServers,
		Clients:                    in.Clients,
		ExcludeClients:             in.ExcludeClients,
		ClientRegistrations:        in.ClientRegistrations,
		ExcludeClientRegistrations: in.ExcludeClientRegistrations,
		Providers:                  in.Providers,
		ExcludeProviders:           in.ExcludeProviders,
		LastUsedWithin:             in.LastUsedWithin,
		LastUsedOlderThan:          in.LastUsedOlderThan,
		LastUsedNever:              in.LastUsedNever,
	})
	if err != nil {
		return fmt.Errorf("parse session filter: %w", err)
	}
	if in.MetaMcpServerID != "" {
		if filter.Gateway, err = gatewayscope.Resolve(ctx, t.db, pid, in.MetaMcpServerID); err != nil {
			return fmt.Errorf("scope sessions to gateway: %w", err)
		}
	}

	rows, err := repo.New(t.db).ListUserSessionsByProjectID(ctx, filter.ListParams(pid, org, []string{}, cursor, limit))
	if err != nil {
		return fmt.Errorf("list user sessions: %w", err)
	}

	items := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		items[i] = buildView(row)
	}

	var next *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		next = &c
	}

	return core.EncodeResult(wr, listResult{Items: items, NextCursor: next})
}

// GetTool fetches a single user session by ID for the authenticated project.
type GetTool struct{ db *pgxpool.Pool }

func NewGetUserSessionTool(db *pgxpool.Pool) core.PlatformToolExecutor { return &GetTool{db: db} }

func (t *GetTool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "user-sessions",
		HandlerName: "get",
		Name:        "platform_get_user_session",
		Description: "Get a single user session by id (read-only).",
		InputSchema: core.BuildInputSchema[getInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *GetTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	pid, org, err := projectTenancy(ctx)
	if err != nil {
		return err
	}

	var in getInput
	if err := core.DecodeInput(payload, &in); err != nil {
		return err
	}

	id, err := parseNullUUID(in.ID, "id")
	if err != nil {
		return err
	}
	if !id.Valid {
		return oops.E(oops.CodeBadRequest, nil, "id is required")
	}

	// status "all" ensures revoked sessions are visible by ID too.
	filter, err := sessionfilter.Parse(sessionfilter.Input{
		Status:                     new("all"),
		SubjectURN:                 nil,
		IssuerID:                   nil,
		ClientID:                   nil,
		Search:                     nil,
		Identities:                 nil,
		ExcludeIdentities:          nil,
		IdentityTypes:              nil,
		ExcludeIdentityTypes:       nil,
		Servers:                    nil,
		ExcludeServers:             nil,
		Clients:                    nil,
		ExcludeClients:             nil,
		ClientRegistrations:        nil,
		ExcludeClientRegistrations: nil,
		Providers:                  nil,
		ExcludeProviders:           nil,
		LastUsedWithin:             nil,
		LastUsedOlderThan:          nil,
		LastUsedNever:              false,
	})
	if err != nil {
		return fmt.Errorf("parse session filter: %w", err)
	}
	params := filter.ListParams(pid, org, []string{}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, 1)
	params.ID = id
	rows, err := repo.New(t.db).ListUserSessionsByProjectID(ctx, params)
	if err != nil {
		return fmt.Errorf("get user session: %w", err)
	}
	if len(rows) == 0 {
		return oops.E(oops.CodeNotFound, nil, "user session not found")
	}

	return core.EncodeResult(wr, buildView(rows[0]))
}
