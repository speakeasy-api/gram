package authclients

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	LevelUse     = "use"
	LevelBlocked = "blocked"

	AppliesToClient     = "client"
	AppliesToClientType = "client_type"
	AppliesToAllClients = "all_clients"

	// NoteOwnConnectionOnly labels a grant naming one person on a personal
	// client: it never reaches anyone else's connection.
	NoteOwnConnectionOnly = "Does not let this user use another user's connection."
	// NoteActingForOwnerOnly labels a grant naming a group, an agent or
	// everyone on a personal client.
	NoteActingForOwnerOnly = "Only usable when acting for the connection owner."
)

var levelScopes = map[string]authz.Scope{
	LevelUse:     authz.ScopeMCPAuth,
	LevelBlocked: authz.ScopeMCPBlockedAuth,
}

// Widest first, so a principal's block is listed before its grant.
var levelOrder = []string{LevelBlocked, LevelUse}

func (s *Service) GetAuthClientAudience(ctx context.Context, payload *gen.GetAuthClientAudiencePayload) (*types.AuthClientAudience, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid auth client id").LogError(ctx, logger)
	}

	queries := repo.New(s.db)
	client, err := queries.GetAuthClient(ctx, repo.GetAuthClientParams{ID: clientID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "auth client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get auth client").LogError(ctx, logger)
	}

	view, err := readAudience(ctx, s.db, client)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read auth client audience").LogError(ctx, logger)
	}
	return view, nil
}

func (s *Service) SetAuthClientAudience(ctx context.Context, payload *gen.SetAuthClientAudiencePayload) (*types.AuthClientAudience, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid auth client id").LogError(ctx, logger)
	}

	desired := make(map[string]audienceRule, len(payload.Entries))
	for _, entry := range payload.Entries {
		if entry == nil {
			continue
		}
		scope, ok := levelScopes[entry.Level]
		if !ok {
			return nil, oops.E(oops.CodeInvalid, nil, "unknown level %q", entry.Level)
		}
		principal, err := parseAudiencePrincipal(entry.PrincipalUrn)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid principal %q", entry.PrincipalUrn).LogError(ctx, logger)
		}
		key := ruleKey(principal.String(), scope)
		if _, dup := desired[key]; dup {
			return nil, oops.E(oops.CodeInvalid, nil, "principal %q is listed more than once at the same level", entry.PrincipalUrn)
		}
		if err := authz.ValidatePrincipal(ctx, s.db, authCtx.ActiveOrganizationID, principal); err != nil {
			if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
				return nil, oops.E(oops.CodeInvalid, err, "unknown principal %q", entry.PrincipalUrn)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "validate principal").LogError(ctx, logger)
		}
		desired[key] = audienceRule{principal: principal, level: entry.Level}
	}

	after, err := saveAudience(ctx, s.db, s.audit, audienceSave{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ClientID:         clientID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ExpectedVersion:  &payload.ExpectedVersion,
		Desired: func([]authz.Grant) map[string]audienceRule {
			return desired
		},
	})
	switch {
	case errors.Is(err, ErrAuthClientNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "auth client not found").LogError(ctx, logger)
	case errors.Is(err, ErrAudienceChanged):
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "who may use this auth client changed while you were editing; reload and try again")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "save auth client audience").LogError(ctx, logger)
	}
	return after, nil
}

var (
	ErrAuthClientNotFound = errors.New("auth client not found")
	ErrAudienceChanged    = errors.New("auth client audience changed")
	ErrPrincipalInvalid   = errors.New("principal cannot hold auth client rules")
)

type audienceRule struct {
	principal urn.Principal
	level     string
}

type audienceSave struct {
	OrganizationID   string
	ClientID         uuid.UUID
	Actor            urn.Principal
	ActorDisplayName *string
	// ExpectedVersion, when set, refuses the save unless the client's own
	// rules are unchanged since the caller read them.
	ExpectedVersion *string
	// Desired computes the client's complete set of own rules, keyed by
	// ruleKey, from the grants currently held under the client row lock.
	Desired func(current []authz.Grant) map[string]audienceRule
}

// SetPrincipalLevel sets one principal's own rule on one auth client to
// LevelUse, LevelBlocked, or, when level is empty, no rule, and keeps every
// other principal's rules. It writes the same audit event as a full save.
func SetPrincipalLevel(ctx context.Context, db *pgxpool.Pool, auditLogger *audit.Logger, organizationID string, actor urn.Principal, clientID uuid.UUID, principal urn.Principal, level string) (*types.AuthClientAudience, error) {
	if _, ok := levelScopes[level]; !ok && level != "" {
		return nil, fmt.Errorf("unknown level %q", level)
	}
	if _, err := parseAudiencePrincipal(principal.String()); err != nil {
		return nil, errors.Join(ErrPrincipalInvalid, err)
	}
	if err := authz.ValidatePrincipal(ctx, db, organizationID, principal); err != nil {
		if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
			return nil, errors.Join(ErrPrincipalInvalid, err)
		}
		return nil, fmt.Errorf("validate principal: %w", err)
	}
	return saveAudience(ctx, db, auditLogger, audienceSave{
		OrganizationID:   organizationID,
		ClientID:         clientID,
		Actor:            actor,
		ActorDisplayName: nil,
		ExpectedVersion:  nil,
		Desired: func(current []authz.Grant) map[string]audienceRule {
			desired := make(map[string]audienceRule, len(current)+1)
			for _, grant := range current {
				if grant.PrincipalUrn == principal.String() {
					continue
				}
				held, err := urn.ParsePrincipal(grant.PrincipalUrn)
				if err != nil {
					continue
				}
				grantLevel := LevelUse
				if grant.Scope == authz.ScopeMCPBlockedAuth {
					grantLevel = LevelBlocked
				}
				desired[ruleKey(grant.PrincipalUrn, grant.Scope)] = audienceRule{principal: held, level: grantLevel}
			}
			if level != "" {
				desired[ruleKey(principal.String(), levelScopes[level])] = audienceRule{principal: principal, level: level}
			}
			return desired
		},
	})
}

func saveAudience(ctx context.Context, db *pgxpool.Pool, auditLogger *audit.Logger, save audienceSave) (*types.AuthClientAudience, error) {
	dbtx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	// The client row lock serializes saves, so the version check and the
	// replacement are one step.
	client, err := queries.LockAuthClient(ctx, repo.LockAuthClientParams{ID: save.ClientID, OrganizationID: save.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAuthClientNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock auth client: %w", err)
	}

	current, err := clientGrants(ctx, dbtx, save.OrganizationID, save.ClientID.String())
	if err != nil {
		return nil, fmt.Errorf("list auth client grants: %w", err)
	}
	if save.ExpectedVersion != nil && audienceVersion(current) != *save.ExpectedVersion {
		return nil, ErrAudienceChanged
	}

	before, err := readAudience(ctx, dbtx, client)
	if err != nil {
		return nil, err
	}

	desired := save.Desired(current)
	// A rule kept as it is keeps the grants that express it, whatever their
	// selector, so saving never takes over a provisioned grant nobody touched.
	held := make(map[string]struct{}, len(current))
	for _, grant := range current {
		key := ruleKey(grant.PrincipalUrn, grant.Scope)
		held[key] = struct{}{}
		if _, keep := desired[key]; keep {
			continue
		}
		principal, err := urn.ParsePrincipal(grant.PrincipalUrn)
		if err != nil {
			return nil, fmt.Errorf("parse grant principal: %w", err)
		}
		remove := []*authz.RoleGrant{{Scope: string(grant.Scope), Selectors: []authz.Selector{grant.Selector}}}
		if err := authz.PatchPrincipalGrants(ctx, dbtx, save.OrganizationID, principal, nil, remove); err != nil {
			return nil, fmt.Errorf("remove auth client grant: %w", err)
		}
		if err := takeOver(ctx, dbtx, save.OrganizationID, grant.PrincipalUrn, grant.Scope, grant.Selector); err != nil {
			return nil, fmt.Errorf("take over removed auth client grant: %w", err)
		}
	}

	addKeys := make([]string, 0, len(desired))
	for key := range desired {
		if _, ok := held[key]; !ok {
			addKeys = append(addKeys, key)
		}
	}
	sort.Strings(addKeys)
	for _, key := range addKeys {
		rule := desired[key]
		scope := levelScopes[rule.level]
		selector := ruleSelector(client, scope)
		add := []*authz.RoleGrant{{Scope: string(scope), Selectors: []authz.Selector{selector}}}
		if err := authz.PatchPrincipalGrants(ctx, dbtx, save.OrganizationID, rule.principal, add, nil); err != nil {
			return nil, fmt.Errorf("write auth client grant: %w", err)
		}
		if err := takeOver(ctx, dbtx, save.OrganizationID, rule.principal.String(), scope, selector); err != nil {
			return nil, fmt.Errorf("take over auth client grant: %w", err)
		}
	}

	after, err := readAudience(ctx, dbtx, client)
	if err != nil {
		return nil, err
	}

	if err := auditLogger.LogAuthClientAudienceUpdate(ctx, dbtx, audit.LogAuthClientAudienceUpdateEvent{
		OrganizationID:         save.OrganizationID,
		ProjectID:              client.ProjectID.UUID,
		Actor:                  save.Actor,
		ActorDisplayName:       save.ActorDisplayName,
		ActorSlug:              nil,
		AuthClientURN:          urn.NewAuthClient(client.ID),
		AuthClientName:         client.Name,
		AudienceSnapshotBefore: before,
		AudienceSnapshotAfter:  after,
	}); err != nil {
		return nil, fmt.Errorf("log auth client audience update: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return after, nil
}

// ruleSelector names the client. A grant on a personal client is written for
// the represented user's own connection, so the stored rule says what the
// engine enforces anyway: it never reaches another person's connection.
func ruleSelector(client repo.McpAuthClient, scope authz.Scope) authz.Selector {
	selector := authz.AuthClientSelector(client.ID.String(), "")
	if scope == authz.ScopeMCPAuth && client.ClientType == authz.AuthClientTypeRemoteSession {
		selector[authz.SelectorKeyClientType] = authz.AuthClientTypeRemoteSession
		selector[authz.SelectorKeyConnectionOwner] = authz.SelectorValueRepresentedUser
	}
	return selector
}

func readAudience(ctx context.Context, db repo.DBTX, client repo.McpAuthClient) (*types.AuthClientAudience, error) {
	own, err := clientGrants(ctx, db, client.OrganizationID, client.ID.String())
	if err != nil {
		return nil, fmt.Errorf("list auth client grants: %w", err)
	}
	broad, err := clientGrants(ctx, db, client.OrganizationID, authz.WildcardResource)
	if err != nil {
		return nil, fmt.Errorf("list broad auth client grants: %w", err)
	}
	servers, err := repo.New(db).ListAuthClientServers(ctx, repo.ListAuthClientServersParams{OrganizationID: client.OrganizationID, AuthClientIds: []uuid.UUID{client.ID}})
	if err != nil {
		return nil, fmt.Errorf("list auth client servers: %w", err)
	}

	personal := client.ClientType == authz.AuthClientTypeRemoteSession
	type entryKey struct{ principal, level, appliesTo, project string }
	seen := make(map[entryKey]struct{})
	entries := make([]*types.AuthClientAudienceEntry, 0, len(own)+len(broad))
	add := func(grant authz.Grant, appliesTo string) {
		level := LevelUse
		if grant.Scope == authz.ScopeMCPBlockedAuth {
			level = LevelBlocked
		}
		project := grant.Selector[authz.SelectorKeyProjectID]
		key := entryKey{principal: grant.PrincipalUrn, level: level, appliesTo: appliesTo, project: project}
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}

		var projectID *string
		if project != "" {
			projectID = new(project)
		}
		var note *string
		if personal && level == LevelUse {
			note = new(connectionNote(grant.PrincipalUrn))
		}
		entries = append(entries, &types.AuthClientAudienceEntry{
			PrincipalUrn:   grant.PrincipalUrn,
			Level:          level,
			AppliesTo:      appliesTo,
			ProjectID:      projectID,
			ConnectionNote: note,
		})
	}

	for _, grant := range own {
		add(grant, AppliesToClient)
	}
	serverProjects := make(map[string]struct{}, len(servers))
	for _, srv := range servers {
		serverProjects[srv.ProjectID.String()] = struct{}{}
	}
	for _, grant := range broad {
		if !reaches(grant.Selector, client, serverProjects) {
			continue
		}
		appliesTo := AppliesToAllClients
		if grant.Selector[authz.SelectorKeyClientType] != "" && grant.Selector[authz.SelectorKeyClientType] != authz.WildcardResource {
			appliesTo = AppliesToClientType
		}
		add(grant, appliesTo)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if left.AppliesTo != right.AppliesTo {
			return left.AppliesTo != AppliesToClient && right.AppliesTo == AppliesToClient
		}
		if left.Level != right.Level {
			return slices.Index(levelOrder, left.Level) < slices.Index(levelOrder, right.Level)
		}
		return left.PrincipalUrn < right.PrincipalUrn
	})

	publicServers := make([]string, 0)
	for _, srv := range servers {
		if srv.Visibility == visibility.Public {
			publicServers = append(publicServers, srv.ID.String())
		}
	}

	return &types.AuthClientAudience{
		AuthClientID:  client.ID.String(),
		Entries:       entries,
		Version:       audienceVersion(own),
		PublicServers: publicServers,
	}, nil
}

// reaches reports whether a grant covering many clients covers this one. A
// project filter on an auth client grant names the project of the server
// using the client, so it reaches an organization-wide client only through a
// server in that project.
func reaches(selector authz.Selector, client repo.McpAuthClient, serverProjects map[string]struct{}) bool {
	if clientType := selector[authz.SelectorKeyClientType]; clientType != "" && clientType != authz.WildcardResource && clientType != client.ClientType {
		return false
	}
	project := selector[authz.SelectorKeyProjectID]
	if project == "" {
		return true
	}
	if client.ProjectID.Valid {
		return project == client.ProjectID.UUID.String()
	}
	_, offered := serverProjects[project]
	return offered
}

// takeOver records that an explicit audience save wrote or removed a grant,
// so the default auth permission backfill treats it as the admin's.
func takeOver(ctx context.Context, dbtx pgx.Tx, organizationID, principalURN string, scope authz.Scope, selector authz.Selector) error {
	raw, err := selector.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal grant selector: %w", err)
	}
	if err := authz.MarkAuthPermissionTakenOverTx(ctx, dbtx, organizationID, principalURN, string(scope), raw); err != nil {
		return fmt.Errorf("take over grant: %w", err)
	}
	return nil
}

func connectionNote(principalURN string) string {
	principal, err := urn.ParsePrincipal(principalURN)
	if err == nil && principal.Type == urn.PrincipalTypeUser && principal.ID != urn.AllUsersPrincipalID {
		return NoteOwnConnectionOnly
	}
	return NoteActingForOwnerOnly
}

func clientGrants(ctx context.Context, db repo.DBTX, organizationID, resourceID string) ([]authz.Grant, error) {
	var grants []authz.Grant
	for _, scope := range []authz.Scope{authz.ScopeMCPAuth, authz.ScopeMCPBlockedAuth} {
		scoped, err := authz.ListGrantsForResource(ctx, db, authz.Resource{OrganizationID: organizationID, Scope: scope, ResourceID: resourceID})
		if err != nil {
			return nil, fmt.Errorf("list %s grants: %w", scope, err)
		}
		grants = append(grants, scoped...)
	}
	return grants, nil
}

func audienceVersion(grants []authz.Grant) string {
	parts := make([]string, 0, len(grants))
	for _, grant := range grants {
		encoded, _ := grant.Selector.MarshalJSON()
		parts = append(parts, grant.PrincipalUrn+"|"+string(grant.Scope)+"|"+string(encoded))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func ruleKey(principalURN string, scope authz.Scope) string {
	return principalURN + "|" + string(scope)
}

// parseAudiencePrincipal accepts people, roles, agents and everyone in the
// organization. API keys hold their own policy, edited on the key.
func parseAudiencePrincipal(value string) (urn.Principal, error) {
	if value == urn.PrincipalWildcard {
		return authz.AllUsersPrincipal(), nil
	}
	principal, err := urn.ParsePrincipal(value)
	if err != nil {
		return urn.Principal{}, fmt.Errorf("parse principal: %w", err)
	}
	switch principal.Type {
	case urn.PrincipalTypeUser, urn.PrincipalTypeRole, urn.PrincipalTypeAgent:
		return principal, nil
	default:
		return urn.Principal{}, fmt.Errorf("auth client rules name people, roles, agents or everyone, not %s principals", principal.Type)
	}
}
