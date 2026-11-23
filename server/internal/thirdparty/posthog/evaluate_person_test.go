package posthog

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	posthoggo "github.com/posthog/posthog-go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

const (
	groupTargetedFlag  feature.Flag = "group-targeted"
	personTargetedFlag feature.Flag = "person-targeted"
)

// flagDefinitions mirrors PostHog's local evaluation payload: one flag matching
// an organization group key, one matching a person's email.
const flagDefinitions = `{
  "flags": [
    {"key": "group-targeted", "active": true, "filters": {
      "aggregation_group_type_index": 0,
      "groups": [{"rollout_percentage": 100, "properties": [
        {"key": "$group_key", "type": "group", "group_type_index": 0, "operator": "exact", "value": ["org-on"]}
      ]}]
    }},
    {"key": "person-targeted", "active": true, "filters": {
      "groups": [{"rollout_percentage": 100, "properties": [
        {"key": "email", "type": "person", "operator": "exact", "value": ["on@example.com"]}
      ]}]
    }}
  ],
  "group_type_mapping": {"0": "organization", "1": "slug"},
  "cohorts": {}
}`

type remoteRequests struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (r *remoteRequests) all() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.bodies)
}

// newSDKBackedPosthog runs the real SDK against a fake PostHog that serves
// flagDefinitions and fails every remote evaluation.
func newSDKBackedPosthog(t *testing.T) (*Posthog, *remoteRequests) {
	t.Helper()
	remote := &remoteRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/feature_flag/local_evaluation") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, flagDefinitions)
			return
		}
		if strings.Contains(r.URL.Path, "/flags") || strings.Contains(r.URL.Path, "/decide") {
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			remote.mu.Lock()
			remote.bodies = append(remote.bodies, body)
			remote.mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	logger := slog.New(slog.DiscardHandler) //nolint:forbidigo // importing testenv would create an import cycle through thirdparty/posthog
	p := New(t.Context(), logger, "project-key", server.URL, "personal-key")
	require.False(t, p.disabled)
	t.Cleanup(func() { _ = p.client.Close() })

	require.Eventually(t, func() bool {
		definitions, err := p.client.GetFeatureFlags()
		return err == nil && slices.ContainsFunc(definitions, func(d posthoggo.FeatureFlag) bool { return d.Key == string(personTargetedFlag) })
	}, 5*time.Second, 10*time.Millisecond)
	return p, remote
}

func TestPosthogEvaluateFlag_PersonAndGroupConditions(t *testing.T) {
	t.Parallel()
	p, remote := newSDKBackedPosthog(t)
	ctx := t.Context()
	groupsOn := map[string]string{"organization": "org-on", "slug": "org-on/project"}
	person := func(email string) map[string]string { return map[string]string{"email": email} }

	// A group condition ignores the distinct ID, so a person distinct ID keeps
	// today's organization targeting.
	got, err := p.EvaluateFlag(ctx, groupTargetedFlag, "on@example.com", groupsOn, person("on@example.com"))
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationEnabled, got)
	got, err = p.EvaluateFlag(ctx, groupTargetedFlag, "organization-id", groupsOn, nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationEnabled, got)
	got, err = p.EvaluateFlag(ctx, groupTargetedFlag, "on@example.com", map[string]string{"organization": "org-off"}, person("on@example.com"))
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationDisabled, got)

	got, err = p.EvaluateFlag(ctx, personTargetedFlag, "on@example.com", groupsOn, person("on@example.com"))
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationEnabled, got)
	got, err = p.EvaluateFlag(ctx, personTargetedFlag, "off@example.com", groupsOn, person("off@example.com"))
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationDisabled, got)
	require.Empty(t, remote.all(), "supplied person properties decide locally")

	// Without person properties the condition cannot decide locally; the
	// remote fallback gets no email and the failure stays closed.
	got, err = p.EvaluateFlag(ctx, personTargetedFlag, "organization-id", groupsOn, nil)
	require.Error(t, err)
	require.Equal(t, feature.EvaluationIndeterminate, got)
	requests := remote.all()
	require.NotEmpty(t, requests)
	require.Equal(t, "organization-id", requests[0]["distinct_id"])
	require.Empty(t, requests[0]["person_properties"])
}
