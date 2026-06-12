package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

// directorySnapshotTTL bounds how stale a hydrated snapshot can be. The
// directory writer advances state in the worker process; the shared Redis
// cache expires entries so the log path picks up new state within the TTL.
const directorySnapshotTTL = 30 * time.Second

// DirectorySnapshot is the denormalized point-in-time directory state stamped
// onto telemetry log rows for a resolved user.
type DirectorySnapshot struct {
	// Attributes is the merged WorkOS custom attributes payload from the
	// user's directory_users rows, exactly as persisted.
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

	attributeRows, err := workosQueries.ListDirectoryUserAttributesByUserID(ctx, workosrepo.ListDirectoryUserAttributesByUserIDParams{
		UserID:         pgtype.Text{String: userID, Valid: true},
		OrganizationID: organizationID,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to load directory user attributes for telemetry snapshot",
			attr.SlogError(err), attr.SlogUserID(userID), attr.SlogOrganizationID(organizationID))
		return snapshot
	}
	for _, raw := range attributeRows {
		payload := unmarshalAttributesPayload(ctx, r.logger, raw)
		if len(payload) == 0 {
			continue
		}
		if snapshot.Attributes == nil {
			snapshot.Attributes = make(map[string]any, len(payload))
		}
		maps.Copy(snapshot.Attributes, payload)
	}

	groupRows, err := workosQueries.ListCurrentDirectoryGroupsByUserID(ctx, workosrepo.ListCurrentDirectoryGroupsByUserIDParams{
		UserID:         pgtype.Text{String: userID, Valid: true},
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
