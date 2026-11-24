package mcpconnectionhealth_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

const groupCondition = `{"aggregation_group_type_index": 0, "groups": [{"rollout_percentage": 100, "properties": [
  {"key": "$group_key", "type": "group", "group_type_index": 0, "operator": "exact", "value": [%q]}]}]}`

const personCondition = `{"groups": [{"rollout_percentage": 100, "properties": [
  {"key": "email", "type": "person", "operator": "exact", "value": [%q]}]}]}`

type fakePostHog struct {
	mu     sync.Mutex
	remote []map[string]any
}

func (f *fakePostHog) remoteRequests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.remote)
}

// usePostHog points the service at the real PostHog provider, polling
// definitions for FlagMCPConnectionHealth with the given filters from a fake
// server that fails every remote evaluation.
func (ti *testInstance) usePostHog(t *testing.T, filters string) *fakePostHog {
	t.Helper()
	fake := &fakePostHog{}
	definitions := fmt.Sprintf(`{"flags": [{"key": %q, "active": true, "filters": %s}], "group_type_mapping": {"0": "organization", "1": "slug"}, "cohorts": {}}`,
		string(feature.FlagMCPConnectionHealth), filters)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/feature_flag/local_evaluation"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, definitions)
		case strings.Contains(r.URL.Path, "/flags"), strings.Contains(r.URL.Path, "/decide"):
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fake.mu.Lock()
			fake.remote = append(fake.remote, body)
			fake.mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(server.Close)

	provider := posthog.New(t.Context(), testenv.NewLogger(t), "project-key", server.URL, "personal-key")
	require.Eventually(t, func() bool {
		at, ok := provider.FlagDefinitionsConfirmedAt()
		return ok && !at.IsZero()
	}, 5*time.Second, 10*time.Millisecond)
	ti.flags.use(provider)
	return fake
}

func TestPostHogFlag_TodaysOrganizationConditionIsUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fake := ti.usePostHog(t, fmt.Sprintf(groupCondition, authCtx.OrganizationSlug))

	require.True(t, ti.check(t, ctx))
	require.True(t, ti.check(t, asPerson(t, ctx, new("someone@example.com"))))
	require.True(t, ti.check(t, asPerson(t, ctx, nil)))
	require.True(t, ti.check(t, asProducerKey(t, ctx)))
	require.Empty(t, fake.remoteRequests())
}

func TestPostHogFlag_APersonConditionMatchesOnlyListedPeople(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	fake := ti.usePostHog(t, fmt.Sprintf(personCondition, "on@example.com"))

	require.True(t, ti.check(t, asPerson(t, ctx, new("On@Example.com"))))
	require.Empty(t, fake.remoteRequests(), "a listed person is decided locally")

	require.False(t, ti.check(t, asPerson(t, ctx, new("off@example.com"))))
	require.False(t, ti.check(t, asProducerKey(t, ctx)))
	// Callers the local check turns away get today's organization check,
	// which asks PostHog for the organization ID with no email.
	remote := fake.remoteRequests()
	require.Len(t, remote, 2)
	for _, request := range remote {
		require.Equal(t, ti.org, request["distinct_id"])
		require.Empty(t, request["person_properties"])
	}
}
