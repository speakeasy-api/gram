package litellm

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestInstanceResolverForgetWinsOverInflightResolution(t *testing.T) {
	t.Parallel()

	resolver := NewInstanceResolver(testenv.NewLogger(t), nil)
	projectID := uuid.New()
	apiKeyID := uuid.New().String()
	instanceID := uuid.New()
	cacheKey := instanceResolverCacheKey("org-test", projectID.String(), apiKeyID)
	started := make(chan struct{})
	release := make(chan struct{})
	resolved := make(chan struct{})
	go func() {
		defer close(resolved)
		_, _, _ = resolver.group.Do(cacheKey, func() (any, error) {
			close(started)
			<-release
			resolver.cache.Add(cacheKey, instanceID)
			return instanceID, nil
		})
	}()
	<-started

	forgotten := make(chan struct{})
	go func() {
		defer close(forgotten)
		resolver.Forget("org-test", projectID, apiKeyID)
	}()
	require.Eventually(t, func() bool {
		cached, ok := resolver.cache.Get(cacheKey)
		return ok && cached == uuid.Nil
	}, time.Second, 10*time.Millisecond)
	close(release)
	<-resolved
	<-forgotten

	cached, ok := resolver.cache.Get(cacheKey)
	require.True(t, ok)
	require.Equal(t, uuid.Nil, cached)
}

func TestInstanceAttributionStopsAfterDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rows := []telemetry.LogParams{{
		ToolInfo: telemetry.ToolInfo{
			OrganizationID: "org-test",
			ProjectID:      uuid.New().String(),
		},
		Attributes: map[attr.Key]any{attr.APIKeyIDKey: uuid.New().String()},
	}}

	enrichLiteLLMInstanceAttribution(ctx, NewInstanceResolver(testenv.NewLogger(t), nil), rows)

	require.NotContains(t, rows[0].Attributes, attr.LiteLLMInstanceIDKey)
}
