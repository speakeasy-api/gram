package remotesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
)

const (
	// emaResourceCacheTTL bounds how long a changed resource/issuer declaration
	// can remain in use across replicas before fresh discovery is required.
	emaResourceCacheTTL = 5 * time.Minute
	// emaResourceFailureTTL suppresses repeated failed probes without treating
	// stale successful metadata as authority after its cache lifetime.
	emaResourceFailureTTL = 15 * time.Second
	// emaResourceProbeBudget bounds discovery on authenticated consent/status paths.
	emaResourceProbeBudget = 3 * time.Second
)

type emaResourceCacheEntry struct {
	Resource protectedresource.EndpointResource `json:"resource"`
	Invalid  bool                               `json:"invalid"`
	Failed   bool                               `json:"failed"`
}

// ResolveEMAResource discovers a direct transport's authorization resource.
// Callers must first authorize the org/project and establish configured EMA.
// Shared cache entries allow first consent and status on any replica to agree;
// transport URLs remain unchanged for actual HTTP requests.
func (m *ChallengeManager) ResolveEMAResource(ctx context.Context, organizationID string, projectID uuid.UUID, endpoint string) (protectedresource.EndpointResource, error) {
	var none protectedresource.EndpointResource
	if m.policy == nil {
		err := fmt.Errorf("EMA resource discovery has no egress policy")
		m.logger.ErrorContext(ctx, "EMA resource discovery unavailable", attr.SlogError(err))
		return none, err
	}
	if organizationID == "" || projectID == uuid.Nil || endpoint == "" {
		return none, protectedresource.ErrEndpointResourceInvalid
	}
	digest := sha256.Sum256([]byte(organizationID + "\n" + projectID.String() + "\n" + endpoint))
	key := "emaEndpointResource:v1:" + hex.EncodeToString(digest[:])
	var entry emaResourceCacheEntry
	if m.locks != nil && m.locks.Get(ctx, key, &entry) == nil {
		if entry.Invalid {
			return none, protectedresource.ErrEndpointResourceInvalid
		}
		if entry.Failed {
			return none, fmt.Errorf("endpoint resource discovery temporarily unavailable")
		}
		return entry.Resource, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, emaResourceProbeBudget)
	defer cancel()
	resource, err := protectedresource.DiscoverEndpointResource(probeCtx, m.policy, endpoint)
	entry = emaResourceCacheEntry{Resource: resource, Invalid: errors.Is(err, protectedresource.ErrEndpointResourceInvalid), Failed: err != nil}
	ttl := emaResourceCacheTTL
	if err != nil || !resource.Discovered {
		ttl = emaResourceFailureTTL
	}
	if m.locks != nil && ctx.Err() == nil {
		if cacheErr := m.locks.Set(ctx, key, entry, ttl); cacheErr != nil {
			m.logger.WarnContext(ctx, "cache endpoint authorization resource", attr.SlogError(cacheErr))
		}
	}
	if err != nil {
		return resource, fmt.Errorf("resolve EMA resource: %w", err)
	}
	return resource, nil
}
