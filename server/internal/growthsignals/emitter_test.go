package growthsignals_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/growthsignals"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestEmitterCapturesEnrichedActivity(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	client := &capturePostHog{}
	enricher := &fakeEnricher{
		organization: growthsignals.OrganizationDetails{Slug: "acme", Name: "Acme Incorporated"},
		project:      growthsignals.ProjectDetails{Slug: "widgets", Name: "Widgets"},
		userEmails:   map[string]string{"user_placeholder": "person@example.test"},
	}

	growthsignals.NewEmitter(testenv.NewLogger(t), client, enricher, emitterSiteURL()).Emit(t.Context(), growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityProjectCreated,
		OrganizationID: "org_placeholder",
		ProjectID:      projectID,
		ActorID:        "user_placeholder",
		ActorType:      urn.PrincipalTypeUser,
		SubjectName:    "Widgets",
		ActingSurface:  string(audit.SurfaceDashboard),
		AuditAction:    audit.ActionProjectCreate,
	})

	captured := client.Captured()
	require.Len(t, captured, 1)
	require.Equal(t, growthsignals.EventName, captured[0].Name)
	require.Equal(t, "person@example.test", captured[0].DistinctID)
	require.Equal(t, "acme", captured[0].Properties["organization_slug"])
	require.Equal(t, "widgets", captured[0].Properties["project_slug"])
	require.Equal(t, "person@example.test", captured[0].Properties["actor_email"])
}

// The demo organization is reseeded daily from a fixture, so every one of its
// mutations would post to Slack every morning.
func TestEmitterSkipsDemoOrganization(t *testing.T) {
	t.Parallel()

	tests := []growthsignals.Activity{
		growthsignals.ActivityOrganizationCreated,
		growthsignals.ActivityProjectCreated,
		growthsignals.ActivityMcpServerCreated,
		growthsignals.ActivityDeviceFirstSeen,
	}

	client := &capturePostHog{}
	emitter := growthsignals.NewEmitter(testenv.NewLogger(t), client, &fakeEnricher{}, emitterSiteURL())

	for _, activity := range tests {
		emitter.Emit(t.Context(), growthsignals.ActivityEvent{
			Activity:       activity,
			OrganizationID: constants.DemoOrganizationID,
		})
	}

	require.Empty(t, client.Captured())
}

func TestEmitterSkipsExcludedActivities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		activity growthsignals.Activity
	}{
		{name: "explicitly skipped", activity: growthsignals.ActivitySkip},
		{name: "unset", activity: ""},
	}

	client := &capturePostHog{}
	emitter := growthsignals.NewEmitter(testenv.NewLogger(t), client, &fakeEnricher{}, emitterSiteURL())

	for _, tt := range tests {
		emitter.Emit(t.Context(), growthsignals.ActivityEvent{
			Activity:       tt.activity,
			OrganizationID: "org_placeholder",
		})

		require.Empty(t, client.Captured(), "captured a %s activity", tt.name)
	}
}

// A lookup that fails narrows the event. Dropping it instead would lose the
// moment entirely over a property nobody filters on.
func TestEmitterShipsActivityWhenEnrichmentFails(t *testing.T) {
	t.Parallel()

	client := &capturePostHog{}
	enricher := &fakeEnricher{
		organizationErr: errors.New("organization lookup unavailable"),
		projectErr:      errors.New("project lookup unavailable"),
		userEmailErr:    errors.New("user lookup unavailable"),
	}

	growthsignals.NewEmitter(testenv.NewLogger(t), client, enricher, emitterSiteURL()).Emit(t.Context(), growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityProjectCreated,
		OrganizationID: "org_placeholder",
		ProjectID:      uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		ActorID:        "user_placeholder",
		ActorType:      urn.PrincipalTypeUser,
		AuditAction:    audit.ActionProjectCreate,
	})

	captured := client.Captured()
	require.Len(t, captured, 1)
	require.Equal(t, "org_placeholder", captured[0].DistinctID)
	require.Equal(t, "project_created", captured[0].Properties["activity"])
	require.Equal(t, "project:create", captured[0].Properties["audit_action"])
	require.NotContains(t, captured[0].Properties, "organization_slug")
	require.NotContains(t, captured[0].Properties, "project_slug")
	require.NotContains(t, captured[0].Properties, "actor_email")
}

// Emission failures are logged rather than returned, because the work that
// produced the activity has already succeeded.
func TestEmitterLogsCaptureFailure(t *testing.T) {
	t.Parallel()

	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))
	client := &capturePostHog{failWith: errors.New("posthog unavailable")}

	growthsignals.NewEmitter(logger, client, &fakeEnricher{}, emitterSiteURL()).Emit(t.Context(), growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityProjectCreated,
		OrganizationID: "org_placeholder",
	})

	require.Len(t, client.Captured(), 1)
	require.Contains(t, logs.String(), "capture growth activity")
	require.Contains(t, logs.String(), "posthog unavailable")
}

func TestEmitterResolvesActorEmailByPrincipalType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		actorID       string
		actorType     urn.PrincipalType
		suppliedEmail string
		wantEmail     string
		wantLookups   []string
	}{
		{
			name:        "user principal is looked up",
			actorID:     "user_placeholder",
			actorType:   urn.PrincipalTypeUser,
			wantEmail:   "person@example.test",
			wantLookups: []string{"user_placeholder"},
		},
		{
			name:        "email principal is already an address",
			actorID:     "person@example.test",
			actorType:   urn.PrincipalTypeEmail,
			wantEmail:   "person@example.test",
			wantLookups: nil,
		},
		{
			name:        "role principal has no person behind it",
			actorID:     "admin",
			actorType:   urn.PrincipalTypeRole,
			wantEmail:   "",
			wantLookups: nil,
		},
		{
			name:        "subject set principal stands for everyone",
			actorID:     urn.AllUsersPrincipalID,
			actorType:   urn.PrincipalTypeUser,
			wantEmail:   "",
			wantLookups: nil,
		},
		{
			name:          "supplied email skips the lookup",
			actorID:       "user_placeholder",
			actorType:     urn.PrincipalTypeUser,
			suppliedEmail: "known@example.test",
			wantEmail:     "known@example.test",
			wantLookups:   nil,
		},
		{
			name:        "no actor at all",
			actorID:     "",
			actorType:   "",
			wantEmail:   "",
			wantLookups: nil,
		},
	}

	for _, tt := range tests {
		client := &capturePostHog{}
		enricher := &fakeEnricher{userEmails: map[string]string{"user_placeholder": "person@example.test"}}

		growthsignals.NewEmitter(testenv.NewLogger(t), client, enricher, emitterSiteURL()).Emit(t.Context(), growthsignals.ActivityEvent{
			Activity:       growthsignals.ActivityProjectCreated,
			OrganizationID: "org_placeholder",
			ActorID:        tt.actorID,
			ActorType:      tt.actorType,
			ActorEmail:     tt.suppliedEmail,
		})

		captured := client.Captured()
		require.Len(t, captured, 1, "captures for %s", tt.name)
		require.Equal(t, tt.wantEmail, propertyString(captured[0].Properties, "actor_email"), "actor email for %s", tt.name)
		require.Equal(t, tt.wantLookups, enricher.UserEmailCalls(), "user lookups for %s", tt.name)
	}
}

// propertyString reads a property that may legitimately be absent, so a missing
// one compares as empty instead of as nil.
func propertyString(properties map[string]any, key string) string {
	value, ok := properties[key].(string)
	if !ok {
		return ""
	}

	return value
}

// emitterSiteURL is the dashboard base URL used to build the fallback
// dashboard_url property.
func emitterSiteURL() *url.URL {
	return &url.URL{Scheme: "https", Host: "app.example.test"}
}
