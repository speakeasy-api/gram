package otel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/singleflight"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const directoryUserContextTTL = 5 * time.Minute

type enrichDirectory struct {
	logger *slog.Logger
	db     database.DBTX
	cache  cache.TypedCacheObject[cachedDirectoryUserContext]
	loads  singleflight.Group
}

func NewEnrichDirectory(logger *slog.Logger, db database.DBTX, cacheImpl cache.Cache) *enrichDirectory {
	logger = logger.With(attr.SlogComponent("enrich-directory"))
	return &enrichDirectory{
		logger: logger,
		db:     db,
		cache: cache.NewTypedObjectCache[cachedDirectoryUserContext](
			logger.With(attr.SlogCacheNamespace("otel_directory_user_context")),
			cacheImpl,
			cache.SuffixNone,
		),
		loads: singleflight.Group{},
	}
}

func (e *enrichDirectory) Name() string {
	return "enrich-directory"
}

func (e *enrichDirectory) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	organizationID := span.GetProvenance().GetOrganizationId()
	_, email, err := dialect.ForSpan(span).ExternalUserEmail(span)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to read user email for directory span enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil, nil
	}
	return e.enrichUser(ctx, organizationID, email), nil
}

type enrichLogDirectory struct {
	directory *enrichDirectory
}

func (e *enrichLogDirectory) Name() string {
	return e.directory.Name()
}

func (e *enrichLogDirectory) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	organizationID := record.GetProvenance().GetOrganizationId()
	_, email, err := dialect.ForLog(record).ExternalUserEmail(record)
	if err != nil {
		e.directory.logger.WarnContext(ctx, "failed to read user email for directory log enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil, nil
	}
	return e.directory.enrichUser(ctx, organizationID, email), nil
}

func (e *enrichDirectory) enrichUser(ctx context.Context, organizationID string, email string) []attribute.KeyValue {
	if organizationID == "" {
		return nil
	}

	email = conv.NormalizeEmail(email)
	if email == "" {
		return nil
	}

	emailDigest := sha256.Sum256([]byte(email))
	emailHash := hex.EncodeToString(emailDigest[:])
	resolved, err := e.resolve(ctx, organizationID, emailHash, email)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to resolve directory user context", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	}
	return resolved.attributes()
}

func (e *enrichDirectory) load(ctx context.Context, organizationID string, email string) (directoryUserContext, error) {
	user, err := usersrepo.New(e.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		var result directoryUserContext
		return result, nil
	case err != nil:
		var result directoryUserContext
		return result, fmt.Errorf("resolve connected user by email: %w", err)
	}

	var result directoryUserContext
	var profileErr error
	profile, err := directory.NewService(e.db).GetUserProfile(ctx, organizationID, user.ID)
	switch {
	case errors.Is(err, directory.ErrUserNotFound):
	case err != nil:
		profileErr = fmt.Errorf("load directory user profile: %w", err)
	default:
		result.DirectoryUserID = profile.ExternalID
		result.UserAttributes = profile.RawAttributes
		result.GroupIDs = make([]string, len(profile.Groups))
		result.GroupNames = profile.GroupNames()
		for i, group := range profile.Groups {
			result.GroupIDs[i] = group.ExternalID
		}
	}

	roles, err := accessrepo.New(e.db).ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{
		OrganizationID: organizationID,
		UserID:         user.ID,
	})
	if err != nil {
		return result, errors.Join(profileErr, fmt.Errorf("load user roles: %w", err))
	}
	result.Roles = make([]string, len(roles))
	for i, role := range roles {
		result.Roles[i] = role.RoleSlug
	}

	return result, profileErr
}

func (e *enrichDirectory) resolve(ctx context.Context, organizationID string, emailHash string, email string) (directoryUserContext, error) {
	cacheKey := directoryUserContextCacheKey(organizationID, emailHash)
	if cached, err := e.cache.Get(ctx, cacheKey); err == nil {
		return cached.Context, nil
	}

	value, err, _ := e.loads.Do(cacheKey, func() (any, error) {
		if cached, err := e.cache.Get(ctx, cacheKey); err == nil {
			return cached.Context, nil
		}

		resolved, loadErr := e.load(ctx, organizationID, email)
		cacheErr := e.cache.Store(ctx, cachedDirectoryUserContext{
			OrganizationID: organizationID,
			EmailHash:      emailHash,
			Context:        resolved,
		})
		return resolved, errors.Join(loadErr, cacheErr)
	})
	resolved, ok := value.(directoryUserContext)
	if !ok {
		var empty directoryUserContext
		return empty, fmt.Errorf("resolve directory span context: unexpected result type %T", value)
	}
	return resolved, err
}

type directoryUserContext struct {
	DirectoryUserID string         `json:"directory_user_id,omitempty"`
	UserAttributes  map[string]any `json:"user_attributes,omitempty"`
	GroupIDs        []string       `json:"group_ids,omitempty"`
	GroupNames      []string       `json:"group_names,omitempty"`
	Roles           []string       `json:"roles,omitempty"`
}

func (c directoryUserContext) attributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(c.UserAttributes)+4)
	if c.DirectoryUserID != "" {
		attrs = append(attrs, DirectoryID(c.DirectoryUserID))
	}

	for name, value := range c.UserAttributes {
		if attr, ok := directoryUserAttribute(name, value); ok {
			attrs = append(attrs, attr)
		}
	}

	if len(c.GroupIDs) > 0 {
		attrs = append(attrs, DirectoryGroupIDs(c.GroupIDs))
	}
	if len(c.GroupNames) > 0 {
		attrs = append(attrs, DirectoryGroupNames(c.GroupNames))
	}
	if len(c.Roles) > 0 {
		attrs = append(attrs, GramUserRoles(c.Roles))
	}
	return attrs
}

func directoryUserAttribute(name string, value any) (attribute.KeyValue, bool) {
	var empty attribute.KeyValue
	if name == "" || value == nil {
		return empty, false
	}

	key := DirectoryAttribute(name)
	converted, ok := directoryAttributeValue(value)
	if ok {
		return attribute.KeyValue{Key: key, Value: converted}, true
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return empty, false
	}
	return key.String(string(encoded)), true
}

func directoryAttributeValue(value any) (attribute.Value, bool) {
	var empty attribute.Value
	switch value := value.(type) {
	case bool:
		return attribute.BoolValue(value), true
	case float64:
		return attribute.Float64Value(value), true
	case string:
		return attribute.StringValue(value), true
	case []any:
		items := make([]attribute.Value, len(value))
		for i, item := range value {
			converted, ok := directoryAttributeValue(item)
			if !ok {
				return empty, false
			}
			items[i] = converted
		}
		return attribute.SliceValue(items...), true
	default:
		return empty, false
	}
}

type cachedDirectoryUserContext struct {
	OrganizationID string               `json:"organization_id"`
	EmailHash      string               `json:"email_hash"`
	Context        directoryUserContext `json:"context"`
}

var _ cache.CacheableObject[cachedDirectoryUserContext] = (*cachedDirectoryUserContext)(nil)

func (c cachedDirectoryUserContext) CacheKey() string {
	return directoryUserContextCacheKey(c.OrganizationID, c.EmailHash)
}

func (c cachedDirectoryUserContext) TTL() time.Duration {
	return directoryUserContextTTL
}

func directoryUserContextCacheKey(organizationID string, emailHash string) string {
	return fmt.Sprintf("otelDirectoryUserContext:v1:%s:%s", organizationID, emailHash)
}
