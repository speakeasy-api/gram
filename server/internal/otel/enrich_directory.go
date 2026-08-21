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

const directoryContextTTL = 5 * time.Minute

type enrichDirectory struct {
	logger *slog.Logger
	db     database.DBTX
	cache  cache.TypedCacheObject[cachedDirectoryContext]
	loads  singleflight.Group
}

func NewEnrichDirectory(logger *slog.Logger, db database.DBTX, cacheImpl cache.Cache) *enrichDirectory {
	logger = logger.With(attr.SlogComponent("enrich-directory"))
	return &enrichDirectory{
		logger: logger,
		db:     db,
		cache: cache.NewTypedObjectCache[cachedDirectoryContext](
			logger.With(attr.SlogCacheNamespace("otel_directory_context")),
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
	return e.enrich(ctx, organizationID, email), nil
}

func (e *enrichDirectory) enrich(ctx context.Context, organizationID string, email string) []attribute.KeyValue {
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
		e.logger.WarnContext(ctx, "failed to resolve directory context", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	}
	return resolved.attributes()
}

func (e *enrichDirectory) load(ctx context.Context, organizationID string, email string) (directoryContext, error) {
	user, err := usersrepo.New(e.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		var result directoryContext
		return result, nil
	case err != nil:
		var result directoryContext
		return result, fmt.Errorf("resolve connected user by email: %w", err)
	}

	var result directoryContext
	var profileErr error
	profile, err := directory.NewService(e.db).GetUserProfile(ctx, organizationID, user.ID)
	switch {
	case errors.Is(err, directory.ErrUserNotFound):
	case err != nil:
		profileErr = fmt.Errorf("load directory user profile: %w", err)
	default:
		result.ID = profile.ExternalID
		result.Attributes = profile.RawAttributes
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

func (e *enrichDirectory) resolve(ctx context.Context, organizationID string, emailHash string, email string) (directoryContext, error) {
	cacheKey := directoryContextCacheKey(organizationID, emailHash)
	if cached, err := e.cache.Get(ctx, cacheKey); err == nil {
		return cached.Context, nil
	}

	value, err, _ := e.loads.Do(cacheKey, func() (any, error) {
		if cached, err := e.cache.Get(ctx, cacheKey); err == nil {
			return cached.Context, nil
		}

		resolved, loadErr := e.load(ctx, organizationID, email)
		cacheErr := e.cache.Store(ctx, cachedDirectoryContext{
			OrganizationID: organizationID,
			EmailHash:      emailHash,
			Context:        resolved,
		})
		return resolved, errors.Join(loadErr, cacheErr)
	})
	resolved, ok := value.(directoryContext)
	if !ok {
		var empty directoryContext
		return empty, fmt.Errorf("resolve directory context: unexpected result type %T", value)
	}
	return resolved, err
}

type directoryContext struct {
	ID         string         `json:"id,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	GroupIDs   []string       `json:"group_ids,omitempty"`
	GroupNames []string       `json:"group_names,omitempty"`
	Roles      []string       `json:"roles,omitempty"`
}

func (c directoryContext) attributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(c.Attributes)+4)
	if c.ID != "" {
		attrs = append(attrs, DirectoryID(c.ID))
	}

	for name, value := range c.Attributes {
		if attr, ok := directoryAttribute(name, value); ok {
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

func directoryAttribute(name string, value any) (attribute.KeyValue, bool) {
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

type cachedDirectoryContext struct {
	OrganizationID string           `json:"organization_id"`
	EmailHash      string           `json:"email_hash"`
	Context        directoryContext `json:"context"`
}

var _ cache.CacheableObject[cachedDirectoryContext] = (*cachedDirectoryContext)(nil)

func (c cachedDirectoryContext) CacheKey() string {
	return directoryContextCacheKey(c.OrganizationID, c.EmailHash)
}

func (c cachedDirectoryContext) TTL() time.Duration {
	return directoryContextTTL
}

func directoryContextCacheKey(organizationID string, emailHash string) string {
	return fmt.Sprintf("otelDirectoryContext:v1:%s:%s", organizationID, emailHash)
}
