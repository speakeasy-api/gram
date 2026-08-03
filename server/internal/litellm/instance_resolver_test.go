package litellm

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/litellm/repo"
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
	resolver.lookup = func(context.Context, repo.GetActiveLiteLLMInstanceIDByAPIKeyParams) (uuid.UUID, error) {
		close(started)
		<-release
		return instanceID, nil
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	result := make(chan uuid.UUID, 1)
	go func() {
		resolvedID, _ := resolver.Resolve(t.Context(), "org-test", projectID.String(), apiKeyID)
		result <- resolvedID
	}()
	<-started

	resolver.Forget("org-test", projectID, apiKeyID)
	close(release)

	require.Equal(t, uuid.Nil, <-result)
	cached, ok := resolver.cache.Get(cacheKey)
	require.True(t, ok)
	require.Equal(t, uuid.Nil, cached)
}

func TestInstanceResolverForgetLeavesOtherKeysInflight(t *testing.T) {
	t.Parallel()

	resolver := NewInstanceResolver(testenv.NewLogger(t), nil)
	projectID := uuid.New()
	apiKeyID := uuid.New().String()
	otherKeyID := uuid.New().String()
	instanceID := uuid.New()
	cacheKey := instanceResolverCacheKey("org-test", projectID.String(), apiKeyID)
	started := make(chan struct{})
	release := make(chan struct{})
	resolver.lookup = func(context.Context, repo.GetActiveLiteLLMInstanceIDByAPIKeyParams) (uuid.UUID, error) {
		close(started)
		<-release
		return instanceID, nil
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	result := make(chan uuid.UUID, 1)
	go func() {
		resolvedID, _ := resolver.Resolve(t.Context(), "org-test", projectID.String(), apiKeyID)
		result <- resolvedID
	}()
	<-started

	resolver.Forget("org-test", projectID, otherKeyID)
	close(release)

	require.Equal(t, instanceID, <-result)
	cached, ok := resolver.cache.Get(cacheKey)
	require.True(t, ok)
	require.Equal(t, instanceID, cached)
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

func TestInstanceAttributionKeepsResolvedRowsAfterDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	resolver := NewInstanceResolver(testenv.NewLogger(t), nil)
	projectID := uuid.New().String()
	firstKeyID := uuid.New().String()
	secondKeyID := uuid.New().String()
	instanceID := uuid.New()
	resolver.lookup = func(_ context.Context, params repo.GetActiveLiteLLMInstanceIDByAPIKeyParams) (uuid.UUID, error) {
		if params.ApiKeyID.String() == firstKeyID {
			cancel()
			return instanceID, nil
		}
		t.Fatal("resolution continued after deadline")
		return uuid.Nil, nil
	}
	rows := []telemetry.LogParams{
		liteLLMAttributionRow(projectID, firstKeyID),
		liteLLMAttributionRow(projectID, secondKeyID),
		liteLLMAttributionRow(projectID, firstKeyID),
	}

	enrichLiteLLMInstanceAttribution(ctx, resolver, rows)

	require.Equal(t, instanceID.String(), rows[0].Attributes[attr.LiteLLMInstanceIDKey])
	require.NotContains(t, rows[1].Attributes, attr.LiteLLMInstanceIDKey)
	require.Equal(t, instanceID.String(), rows[2].Attributes[attr.LiteLLMInstanceIDKey])
}

func liteLLMAttributionRow(projectID, apiKeyID string) telemetry.LogParams {
	return telemetry.LogParams{
		ToolInfo: telemetry.ToolInfo{
			OrganizationID: "org-test",
			ProjectID:      projectID,
		},
		Attributes: map[attr.Key]any{attr.APIKeyIDKey: apiKeyID},
	}
}
