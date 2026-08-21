package otel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/singleflight"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const userEnrichmentTTL = 5 * time.Minute

func fetchUserEnrichment(
	ctx context.Context,
	replicaDB database.DBTX,
	enrichmentCache *cache.TypedCacheObject[cachedUserEnrichment],
	loads *singleflight.Group,
	organizationID string,
	email string,
) (userEnrichment, error) {
	if organizationID == "" {
		var empty userEnrichment
		return empty, nil
	}

	email = conv.NormalizeEmail(email)
	if email == "" {
		var empty userEnrichment
		return empty, nil
	}

	emailDigest := sha256.Sum256([]byte(email))
	emailHash := hex.EncodeToString(emailDigest[:])
	cacheKey := userEnrichmentCacheKey(organizationID, emailHash)
	if cached, err := enrichmentCache.Get(ctx, cacheKey); err == nil {
		return cached.Enrichment, nil
	}

	value, err, _ := loads.Do(cacheKey, func() (any, error) {
		if cached, err := enrichmentCache.Get(ctx, cacheKey); err == nil {
			return cached.Enrichment, nil
		}

		resolved, err := loadUserEnrichment(ctx, replicaDB, organizationID, email)
		if err != nil {
			return resolved, err
		}

		err = enrichmentCache.Store(ctx, cachedUserEnrichment{
			OrganizationID: organizationID,
			EmailHash:      emailHash,
			Enrichment:     resolved,
		})
		if err != nil {
			return resolved, fmt.Errorf("cache user enrichment: %w", err)
		}
		return resolved, nil
	})
	resolved, ok := value.(userEnrichment)
	if !ok {
		var empty userEnrichment
		return empty, fmt.Errorf("resolve user enrichment: unexpected result type %T", value)
	}
	return resolved, err
}

func loadUserEnrichment(ctx context.Context, replicaDB database.DBTX, organizationID string, email string) (userEnrichment, error) {
	user, err := usersrepo.New(replicaDB).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		var empty userEnrichment
		return empty, nil
	case err != nil:
		var empty userEnrichment
		return empty, fmt.Errorf("resolve connected user by email: %w", err)
	}

	var result userEnrichment
	var profileErr error
	profile, err := directory.NewService(replicaDB).GetUserProfile(ctx, organizationID, user.ID)
	switch {
	case errors.Is(err, directory.ErrUserNotFound):
	case err != nil:
		profileErr = fmt.Errorf("load directory user profile: %w", err)
	default:
		result.DirectoryID = profile.ExternalID
		result.DirectoryAttributes = profile.RawAttributes
		result.DirectoryGroupIDs = make([]string, len(profile.Groups))
		result.DirectoryGroupNames = profile.GroupNames()
		for i, group := range profile.Groups {
			result.DirectoryGroupIDs[i] = group.ExternalID
		}
	}

	roles, err := accessrepo.New(replicaDB).ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{
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

type userEnrichment struct {
	DirectoryID         string         `json:"directory_id,omitempty"`
	DirectoryAttributes map[string]any `json:"directory_attributes,omitempty"`
	DirectoryGroupIDs   []string       `json:"directory_group_ids,omitempty"`
	DirectoryGroupNames []string       `json:"directory_group_names,omitempty"`
	Roles               []string       `json:"roles,omitempty"`
}

func (e userEnrichment) attributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(e.DirectoryAttributes)+4)
	if e.DirectoryID != "" {
		attrs = append(attrs, DirectoryID(e.DirectoryID))
	}

	for name, value := range e.DirectoryAttributes {
		if attr, ok := directoryAttribute(name, value); ok {
			attrs = append(attrs, attr)
		}
	}

	if len(e.DirectoryGroupIDs) > 0 {
		attrs = append(attrs, DirectoryGroupIDs(e.DirectoryGroupIDs))
	}
	if len(e.DirectoryGroupNames) > 0 {
		attrs = append(attrs, DirectoryGroupNames(e.DirectoryGroupNames))
	}
	if len(e.Roles) > 0 {
		attrs = append(attrs, GramUserRoles(e.Roles))
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

type cachedUserEnrichment struct {
	OrganizationID string         `json:"organization_id"`
	EmailHash      string         `json:"email_hash"`
	Enrichment     userEnrichment `json:"enrichment"`
}

var _ cache.CacheableObject[cachedUserEnrichment] = (*cachedUserEnrichment)(nil)

func (c cachedUserEnrichment) CacheKey() string {
	return userEnrichmentCacheKey(c.OrganizationID, c.EmailHash)
}

func (c cachedUserEnrichment) TTL() time.Duration {
	return userEnrichmentTTL
}

func userEnrichmentCacheKey(organizationID string, emailHash string) string {
	return fmt.Sprintf("otelUserEnrichment:v1:%s:%s", organizationID, emailHash)
}
