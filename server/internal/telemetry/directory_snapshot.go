package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

// directorySnapshotTTL bounds how stale a hydrated snapshot can be. The
// directory writer advances state in the worker process; the shared Redis
// cache expires entries so the log path picks up new state within the TTL.
const directorySnapshotTTL = 30 * time.Second

// directoryUserAttributeAllowlist is the v0 set of attributes stamped onto
// telemetry rows. These are WorkOS predefined attributes
// (https://workos.com/docs/directory-sync/attributes): named and schematized
// by WorkOS, auto-mapped across directory providers, so they mean the same
// thing for every organization. Customer-defined custom attributes are
// deliberately excluded for now; Postgres keeps the full payload, so
// expanding this list later only requires hydrating new rows.
var directoryUserAttributeAllowlist = []string{
	"department_name",
	"job_title",
	"employee_type",
	"division_name",
	"cost_center_name",
}

// DirectorySnapshot is the denormalized point-in-time directory state stamped
// onto telemetry log rows for a resolved user.
type DirectorySnapshot struct {
	// Attributes is the allowlisted subset of the WorkOS custom attributes
	// payload from the user's directory_users row: predefined WorkOS
	// attribute names with string values only.
	Attributes map[string]any `json:"attributes"`
	// Groups are the user's current directory groups, limited to ID and
	// name. Group raw_attributes are the full uncurated IdP payload and are
	// deliberately not stamped onto log rows.
	Groups []DirectoryGroupSnapshot `json:"groups"`
	// Roles are the user's current role slugs from the existing role tables.
	Roles []string `json:"roles"`
}

// DirectoryGroupSnapshot is one current directory group in a snapshot.
type DirectoryGroupSnapshot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cachedDirectorySnapshot struct {
	OrganizationID string            `json:"organization_id"`
	UserID         string            `json:"user_id"`
	Snapshot       DirectorySnapshot `json:"snapshot"`
}

var _ cache.CacheableObject[cachedDirectorySnapshot] = (*cachedDirectorySnapshot)(nil)

func (c cachedDirectorySnapshot) CacheKey() string {
	return directorySnapshotCacheKey(c.OrganizationID, c.UserID)
}

func (c cachedDirectorySnapshot) AdditionalCacheKeys() []string {
	return []string{}
}

func (c cachedDirectorySnapshot) TTL() time.Duration {
	return directorySnapshotTTL
}

func directorySnapshotCacheKey(organizationID string, userID string) string {
	return fmt.Sprintf("directorySnapshot:%s:%s", organizationID, userID)
}

// DirectorySnapshotResolver resolves the current directory snapshot for a
// user from Postgres, with a short-TTL Redis cache shared across processes so
// the log path does not query Postgres for every row.
type DirectorySnapshotResolver struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	cache  cache.TypedCacheObject[cachedDirectorySnapshot]
}

func NewDirectorySnapshotResolver(logger *slog.Logger, db *pgxpool.Pool, cacheImpl cache.Cache) *DirectorySnapshotResolver {
	return &DirectorySnapshotResolver{
		logger: logger.With(attr.SlogComponent("directory_snapshot_resolver")),
		db:     db,
		cache: cache.NewTypedObjectCache[cachedDirectorySnapshot](
			logger.With(attr.SlogCacheNamespace("directory_snapshot")),
			cacheImpl,
			cache.SuffixNone,
		),
	}
}

// Resolve returns the current directory snapshot for the user in the
// organization. Cache and lookup failures degrade gracefully: any cache error
// is treated as a miss, and Postgres lookup failures resolve to an empty
// snapshot that is still cached so a struggling database does not get
// hammered by the log path.
func (r *DirectorySnapshotResolver) Resolve(ctx context.Context, organizationID string, userID string) DirectorySnapshot {
	key := directorySnapshotCacheKey(organizationID, userID)
	if cached, err := r.cache.Get(ctx, key); err == nil {
		return cached.Snapshot
	}

	snapshot := r.load(ctx, organizationID, userID)

	if err := r.cache.Store(ctx, cachedDirectorySnapshot{
		OrganizationID: organizationID,
		UserID:         userID,
		Snapshot:       snapshot,
	}); err != nil {
		r.logger.WarnContext(ctx, "failed to cache directory snapshot",
			attr.SlogError(err), attr.SlogUserID(userID), attr.SlogOrganizationID(organizationID))
	}

	return snapshot
}

func (r *DirectorySnapshotResolver) load(ctx context.Context, organizationID string, userID string) DirectorySnapshot {
	snapshot := DirectorySnapshot{Attributes: nil, Groups: nil, Roles: nil}

	workosQueries := workosrepo.New(r.db)

	raw, err := workosQueries.GetDirectoryUserAttributesByUserID(ctx, workosrepo.GetDirectoryUserAttributesByUserIDParams{
		UserID:         conv.ToPGText(userID),
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No directory user for this org/user (no Directory Sync, or the
		// user was directory-deleted): attributes stay empty.
	case err != nil:
		r.logger.WarnContext(ctx, "failed to load directory user attributes for telemetry snapshot",
			attr.SlogError(err), attr.SlogUserID(userID), attr.SlogOrganizationID(organizationID))
		return snapshot
	default:
		payload := unmarshalAttributesPayload(ctx, r.logger, raw)
		for _, key := range directoryUserAttributeAllowlist {
			// Values come from customer-controlled IdP mappings: accept
			// non-empty strings only so the ClickHouse JSON column sees
			// consistent types.
			value, ok := payload[key].(string)
			if !ok || value == "" {
				continue
			}
			if snapshot.Attributes == nil {
				snapshot.Attributes = make(map[string]any, len(directoryUserAttributeAllowlist))
			}
			snapshot.Attributes[key] = value
		}
	}

	groupRows, err := workosQueries.ListCurrentDirectoryGroupsByUserID(ctx, workosrepo.ListCurrentDirectoryGroupsByUserIDParams{
		UserID:         conv.ToPGText(userID),
		OrganizationID: organizationID,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to load directory groups for telemetry snapshot",
			attr.SlogError(err), attr.SlogUserID(userID), attr.SlogOrganizationID(organizationID))
		return snapshot
	}
	for _, row := range groupRows {
		snapshot.Groups = append(snapshot.Groups, DirectoryGroupSnapshot{
			ID:   row.WorkosDirectoryGroupID,
			Name: row.Name,
		})
	}

	roleRows, err := accessrepo.New(r.db).ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to load role context for telemetry snapshot",
			attr.SlogError(err), attr.SlogUserID(userID), attr.SlogOrganizationID(organizationID))
		return snapshot
	}
	for _, row := range roleRows {
		snapshot.Roles = append(snapshot.Roles, row.RoleSlug)
	}

	return snapshot
}

func unmarshalAttributesPayload(ctx context.Context, logger *slog.Logger, raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		logger.WarnContext(ctx, "failed to unmarshal directory attributes payload", attr.SlogError(err))
		return nil
	}
	return payload
}
