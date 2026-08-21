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

const directorySpanContextTTL = 5 * time.Minute

type enrichDirectory struct {
	logger *slog.Logger
	db     database.DBTX
	cache  cache.TypedCacheObject[cachedDirectorySpanContext]
	loads  singleflight.Group
}

func NewEnrichDirectory(logger *slog.Logger, db database.DBTX, cacheImpl cache.Cache) *enrichDirectory {
	logger = logger.With(attr.SlogComponent("enrich-directory"))
	return &enrichDirectory{
		logger: logger,
		db:     db,
		cache: cache.NewTypedObjectCache[cachedDirectorySpanContext](
			logger.With(attr.SlogCacheNamespace("otel_directory_span_context")),
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
	if organizationID == "" {
		return nil, nil
	}

	_, email, err := dialect.ForSpan(span).ExternalUserEmail(span)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to read user email for directory span enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil, nil
	}
	email = conv.NormalizeEmail(email)
	if email == "" {
		return nil, nil
	}

	emailDigest := sha256.Sum256([]byte(email))
	emailHash := hex.EncodeToString(emailDigest[:])
	resolved, err := e.resolve(ctx, organizationID, emailHash, email)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to resolve directory span context", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	}
	return resolved.attributes(), nil
}

func (e *enrichDirectory) load(ctx context.Context, organizationID string, email string) (directorySpanContext, error) {
	user, err := usersrepo.New(e.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		var result directorySpanContext
		return result, nil
	}
	if err != nil {
		var result directorySpanContext
		return result, fmt.Errorf("resolve connected user by email: %w", err)
	}

	var result directorySpanContext
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

func (e *enrichDirectory) resolve(ctx context.Context, organizationID string, emailHash string, email string) (directorySpanContext, error) {
	cacheKey := directorySpanContextCacheKey(organizationID, emailHash)
	if cached, err := e.cache.Get(ctx, cacheKey); err == nil {
		return cached.Context, nil
	}

	value, err, _ := e.loads.Do(cacheKey, func() (any, error) {
		if cached, err := e.cache.Get(ctx, cacheKey); err == nil {
			return cached.Context, nil
		}

		resolved, loadErr := e.load(ctx, organizationID, email)
		cacheErr := e.cache.Store(ctx, cachedDirectorySpanContext{
			OrganizationID: organizationID,
			EmailHash:      emailHash,
			Context:        resolved,
		})
		return resolved, errors.Join(loadErr, cacheErr)
	})
	resolved, ok := value.(directorySpanContext)
	if !ok {
		var empty directorySpanContext
		return empty, fmt.Errorf("resolve directory span context: unexpected result type %T", value)
	}
	return resolved, err
}

type directorySpanContext struct {
	DirectoryUserID string         `json:"directory_user_id,omitempty"`
	UserAttributes  map[string]any `json:"user_attributes,omitempty"`
	GroupIDs        []string       `json:"group_ids,omitempty"`
	GroupNames      []string       `json:"group_names,omitempty"`
	Roles           []string       `json:"roles,omitempty"`
}

func (c directorySpanContext) attributes() []attribute.KeyValue {
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

type cachedDirectorySpanContext struct {
	OrganizationID string               `json:"organization_id"`
	EmailHash      string               `json:"email_hash"`
	Context        directorySpanContext `json:"context"`
}

var _ cache.CacheableObject[cachedDirectorySpanContext] = (*cachedDirectorySpanContext)(nil)

func (c cachedDirectorySpanContext) CacheKey() string {
	return directorySpanContextCacheKey(c.OrganizationID, c.EmailHash)
}

func (c cachedDirectorySpanContext) TTL() time.Duration {
	return directorySpanContextTTL
}

func directorySpanContextCacheKey(organizationID string, emailHash string) string {
	return fmt.Sprintf("otelDirectorySpanContext:v1:%s:%s", organizationID, emailHash)
}
