package telemetry

import (
	"context"
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
	"github.com/speakeasy-api/gram/server/internal/directory"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// userInfoSnapshotTTL bounds how stale a hydrated snapshot can be. The
// directory writer advances state in the worker process; the shared Redis
// cache expires entries so the log path picks up new state within the TTL.
const userInfoSnapshotTTL = 5 * time.Minute

// userInfoSnapshot is the denormalized point-in-time directory state for a
// resolved user, used to fill the directory-derived parts of a UserInfo.
type userInfoSnapshot struct {
	Attributes directory.UserAttributes `json:"attributes"`
	Groups     []string                 `json:"groups"`
	Roles      []string                 `json:"roles"`
}

func (s userInfoSnapshot) AsAttributes() map[attr.Key]any {
	attrs := make(map[attr.Key]any, 3)
	if !s.Attributes.IsZero() {
		attrs[attr.UserAttributesKey] = s.Attributes
	}
	if len(s.Groups) > 0 {
		attrs[attr.UserGroupsKey] = s.Groups
	}
	if len(s.Roles) > 0 {
		attrs[attr.UserRolesKey] = s.Roles
	}
	return attrs
}

type cachedUserInfoSnapshot struct {
	OrganizationID string           `json:"organization_id"`
	UserID         string           `json:"user_id"`
	Snapshot       userInfoSnapshot `json:"snapshot"`
}

var _ cache.CacheableObject[cachedUserInfoSnapshot] = (*cachedUserInfoSnapshot)(nil)

func (c cachedUserInfoSnapshot) CacheKey() string {
	return userInfoSnapshotCacheKey(c.OrganizationID, c.UserID)
}

func (c cachedUserInfoSnapshot) AdditionalCacheKeys() []string {
	return []string{}
}

func (c cachedUserInfoSnapshot) TTL() time.Duration {
	return userInfoSnapshotTTL
}

func userInfoSnapshotCacheKey(organizationID string, userID string) string {
	return fmt.Sprintf("userInfoSnapshot:%s:%s", organizationID, userID)
}

// UserInfoResolver fills the directory-derived parts of a UserInfo
// (attributes, groups, roles) from Postgres, with a short-TTL Redis cache
// shared across processes so the log path does not query Postgres for every
// row.
type UserInfoResolver struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	cache  cache.TypedCacheObject[cachedUserInfoSnapshot]
}

func NewUserInfoResolver(logger *slog.Logger, db *pgxpool.Pool, cacheImpl cache.Cache) *UserInfoResolver {
	return &UserInfoResolver{
		logger: logger.With(attr.SlogComponent("user_info_resolver")),
		db:     db,
		cache: cache.NewTypedObjectCache[cachedUserInfoSnapshot](
			logger.With(attr.SlogCacheNamespace("user_info_snapshot")),
			cacheImpl,
			cache.SuffixNone,
		),
	}
}

// Hydrate completes the caller-provided user identity and returns the
// directory-derived snapshot for that user in the organization. Cache and
// lookup failures degrade gracefully: any cache error is treated as a miss,
// and Postgres lookup failures resolve to an empty snapshot that is still
// cached so a struggling database does not get hammered by the log path.
func (r *UserInfoResolver) Hydrate(ctx context.Context, organizationID string, info UserInfo) (UserInfo, userInfoSnapshot) {
	info = r.resolveIdentity(ctx, organizationID, info)
	if info.userID == "" {
		var snapshot userInfoSnapshot
		return info, snapshot
	}
	return info, r.resolve(ctx, organizationID, info.userID)
}

func (r *UserInfoResolver) resolveIdentity(ctx context.Context, organizationID string, info UserInfo) UserInfo {
	if info.userID != "" && info.email != "" {
		return info
	}

	users := usersrepo.New(r.db)
	email := conv.NormalizeEmail(info.email)
	if email == "" && info.userID != "" {
		user, err := users.GetUser(ctx, info.userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return info
		}
		if err != nil {
			r.logger.WarnContext(ctx, "failed to load telemetry user by ID",
				attr.SlogError(err), attr.SlogUserID(info.userID), attr.SlogOrganizationID(organizationID))
			return info
		}
		email = conv.NormalizeEmail(user.Email)
	}
	if email == "" {
		return info
	}

	user, err := users.GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		info.email = email
		return info
	}
	if err != nil {
		r.logger.WarnContext(ctx, "failed to load connected telemetry user by email",
			attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		info.email = email
		return info
	}
	info.userID = user.ID
	info.email = user.Email
	return info
}

func (r *UserInfoResolver) resolve(ctx context.Context, organizationID string, userID string) userInfoSnapshot {
	key := userInfoSnapshotCacheKey(organizationID, userID)
	if cached, err := r.cache.Get(ctx, key); err == nil {
		return cached.Snapshot
	}

	snapshot := r.load(ctx, organizationID, userID)

	if err := r.cache.Store(ctx, cachedUserInfoSnapshot{
		OrganizationID: organizationID,
		UserID:         userID,
		Snapshot:       snapshot,
	}); err != nil {
		r.logger.WarnContext(ctx, "failed to cache user info snapshot",
			attr.SlogError(err), attr.SlogUserID(userID), attr.SlogOrganizationID(organizationID))
	}

	return snapshot
}

func (r *UserInfoResolver) load(ctx context.Context, organizationID string, userID string) userInfoSnapshot {
	var snapshot userInfoSnapshot

	profile, err := directory.NewService(r.db).GetUserProfile(ctx, organizationID, userID)
	switch {
	case errors.Is(err, directory.ErrUserNotFound):
		// No directory user for this org/user (no Directory Sync, or the
		// user was directory-deleted): attributes stay empty.
	case err != nil:
		r.logger.WarnContext(ctx, "failed to load directory user profile for telemetry snapshot",
			attr.SlogError(err), attr.SlogUserID(userID), attr.SlogOrganizationID(organizationID))
		return snapshot
	default:
		snapshot.Attributes = profile.Attributes()
		snapshot.Groups = profile.GroupNames()
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
