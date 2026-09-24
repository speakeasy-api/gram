package anthropicinference

import (
	"testing"

	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

func TestCheckpointInvalidatesChangedAudienceMembership(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	userID := "context-user"
	_, err := usersrepo.New(db).UpsertUser(t.Context(), usersrepo.UpsertUserParams{ID: userID, Email: "context@example.test", DisplayName: "Context Example"})
	require.NoError(t, err)
	policyID := uuid.New()
	_, err = riskrepo.New(db).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{ID: policyID, ProjectID: config.ProjectID, OrganizationID: config.OrganizationID, Name: "Targeted Example", PolicyType: "standard", Sources: []string{"gitleaks"}, Enabled: true, Action: "block", AudienceType: "targeted"})
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: config.OrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, userID), Scope: "risk_policy:evaluate", Selectors: []byte(`{"resource_kind":"risk_policy","resource_id":"` + policyID.String() + `"}`),
	})
	require.NoError(t, err)
	saveFrame(t, store, config, frame, userID)
	before, err := store.Begin(t.Context(), config, frame, userID)
	require.NoError(t, err)
	_, err = before.Load(t.Context())
	require.NoError(t, err)
	require.NoError(t, before.Accept(t.Context(), transcriptHashes(frame.Messages)))
	stale, err := store.Begin(t.Context(), config, frame, userID)
	require.NoError(t, err)
	accepted, err := stale.Load(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, accepted)
	// Same policy version and user ID, but the user now belongs to the org and
	// their existing evaluate grant becomes applicable.
	_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: config.OrganizationID, UserID: conv.ToPGText(userID)})
	require.NoError(t, err)
	require.ErrorContains(t, stale.Accept(t.Context(), transcriptHashes(frame.Messages)), "changed during evaluation")
	after, err := store.Begin(t.Context(), config, frame, userID)
	require.NoError(t, err)
	accepted, err = after.Load(t.Context())
	require.NoError(t, err)
	require.Empty(t, accepted)
}

func TestCheckpointInvalidatesRemovedExclusion(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	exclusion, err := riskrepo.New(db).CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{ProjectID: config.ProjectID, OrganizationID: config.OrganizationID, MatchType: "exact", MatchValue: "EXAMPLE", Enabled: true})
	require.NoError(t, err)
	saveFrame(t, store, config, frame, "")
	before, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	_, err = before.Load(t.Context())
	require.NoError(t, err)
	require.NoError(t, before.Accept(t.Context(), transcriptHashes(frame.Messages)))
	stale, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	accepted, err := stale.Load(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, accepted)
	require.NoError(t, riskrepo.New(db).DeleteRiskExclusion(t.Context(), riskrepo.DeleteRiskExclusionParams{ID: exclusion.ID, ProjectID: config.ProjectID}))
	require.ErrorContains(t, stale.Accept(t.Context(), transcriptHashes(frame.Messages)), "changed during evaluation")
	after, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	accepted, err = after.Load(t.Context())
	require.NoError(t, err)
	require.Empty(t, accepted)
}

func TestCheckpointReusesAcceptedPromptPolicyHistory(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "first prompt"), textMessage("assistant", "first reply"), textMessage("user", "current prompt")}
	_, err := riskrepo.New(db).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{ID: uuid.New(), ProjectID: config.ProjectID, OrganizationID: config.OrganizationID, Name: "Prompt Example", PolicyType: "prompt_based", Sources: []string{}, Enabled: true, Action: "block", AudienceType: "everyone", Prompt: conv.ToPGText("Find EXAMPLE content")})
	require.NoError(t, err)
	scanner := &recordingScanner{}
	service := NewService(testenv.NewLogger(t), db, store.writer, scanner, nil)
	verdict, err := service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Len(t, scanner.inputs, 3)
	after, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	accepted, err := after.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, transcriptHashes(frame.Messages), accepted)
	// A prompt policy's presence must not defeat delta scanning. The current
	// turn is still evaluated on every delivery, even when already accepted.
	scanner.reset()
	verdict, err = service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Len(t, scanner.inputs, 1)
	require.Equal(t, "current prompt", scanner.inputs[0].text)
}
