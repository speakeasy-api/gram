package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

type stubDetectionReader struct {
	lastInput access.AIDetectionsReadInput
	result    *accessgen.ListAIDetectionsResult
}

func (s *stubDetectionReader) ReadAIDetections(_ context.Context, input access.AIDetectionsReadInput) (*accessgen.ListAIDetectionsResult, error) {
	s.lastInput = input
	return s.result, nil
}

type stubLibraryReader struct{ list *aitargets.OrganizationList }

func (s *stubLibraryReader) LoadLibrary(context.Context, string) (*aitargets.OrganizationList, error) {
	return s.list, nil
}

type stubAuthorizer struct{ err error }

func (s stubAuthorizer) RequireLiveOrgAdmin(context.Context, Principal) error { return s.err }

func allowingBudget() OperationBudget {
	allow := func() Limiter { return &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}} }
	return OperationBudget{Connection: allow(), Organization: allow()}
}

func shadowAIPrincipal() Principal {
	return Principal{UserID: "user_1", OrganizationID: "org_1", ConnectionID: "conn_1", Generation: "g1", ClientID: "client_1", Surface: SurfacePlatformMCP}
}

func TestShadowAIListToolsProjectsTheAccessVerdict(t *testing.T) {
	t.Parallel()

	detections := &stubDetectionReader{result: &accessgen.ListAIDetectionsResult{Detections: []*accessgen.AIDetection{{
		TargetID:    "codex",
		DisplayName: "Codex",
		Category:    "harness",
		Signals:     []string{"installed"},
		UserCount:   3,
		DeviceCount: 4,
		LastSeen:    "2026-09-11T00:00:00Z",
		Access:      &accessgen.AIToolAccessSummary{State: "blocked", Enforceable: true},
	}}}}
	service := NewShadowAIService(detections, &stubLibraryReader{}, stubAuthorizer{}, allowingBudget())
	require.NotNil(t, service)

	out, err := service.ListTools(t.Context(), shadowAIPrincipal(), ListShadowAIToolsInput{Category: "harness"})
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)
	require.Equal(t, "codex", out.Tools[0].TargetID)
	require.Equal(t, "blocked", out.Tools[0].State)
	require.True(t, out.Tools[0].Enforceable)
	require.EqualValues(t, 3, out.Tools[0].UserCount)

	require.Equal(t, "org_1", detections.lastInput.OrganizationID)
	require.Equal(t, "harness", detections.lastInput.Category)
}

// The dashboard withholds per-tool user and device counts below org:admin, so
// this surface must refuse rather than become the way around that.
func TestShadowAIRefusesWithoutLiveOrgAdmin(t *testing.T) {
	t.Parallel()

	denied := errors.New("forbidden")
	service := NewShadowAIService(&stubDetectionReader{}, &stubLibraryReader{}, stubAuthorizer{err: denied}, allowingBudget())
	require.NotNil(t, service)

	_, err := service.ListTools(t.Context(), shadowAIPrincipal(), ListShadowAIToolsInput{})
	require.ErrorIs(t, err, denied)

	_, err = service.ListLibrary(t.Context(), shadowAIPrincipal(), ListAIScanLibraryInput{})
	require.ErrorIs(t, err, denied)
}

func TestShadowAIListLibraryReportsOriginAndBlockability(t *testing.T) {
	t.Parallel()

	blockable := aitargets.ZeroTarget()
	blockable.ID = "codex"
	blockable.DisplayName = "Codex"
	blockable.Category = aitargets.CategoryHarness
	blockable.GatewayClient.OAuthClientIDs = []string{"https://chatgpt.com/oauth/codex/client.json"}

	inert := aitargets.ZeroTarget()
	inert.ID = "acme-tool"
	inert.DisplayName = "Acme Tool"
	inert.Category = aitargets.CategoryHarness

	list := &aitargets.OrganizationList{
		Entries: []aitargets.Entry{
			{Target: blockable, Source: aitargets.SourceDefault},
			{Target: inert, Source: aitargets.SourceOrganization},
		},
		Snapshot: aitargets.NewSnapshot(7, nil),
	}
	service := NewShadowAIService(&stubDetectionReader{}, &stubLibraryReader{list: list}, stubAuthorizer{}, allowingBudget())
	require.NotNil(t, service)

	out, err := service.ListLibrary(t.Context(), shadowAIPrincipal(), ListAIScanLibraryInput{})
	require.NoError(t, err)
	require.EqualValues(t, 7, out.LibraryVersion)
	require.Len(t, out.Targets, 2)

	// ListLibrary preserves entry order, so the two entries can be checked
	// positionally: the built-in first, the organization's own addition second.
	require.Equal(t, "codex", out.Targets[0].TargetID)
	require.Equal(t, "default", out.Targets[0].Origin, "a target Speakeasy ships reports as a default")
	require.True(t, out.Targets[0].Blockable, "a target publishing a CIMD document can be blocked at the gateway")
	require.Equal(t, "acme-tool", out.Targets[1].TargetID)
	require.Equal(t, "organization", out.Targets[1].Origin, "one this organization added reports as its own")
	require.False(t, out.Targets[1].Blockable, "one publishing none cannot")
}

// A category the library does not use is the caller's mistake to correct, so it
// must not come back as the feature being switched off for the organization.
func TestShadowAIListLibraryRejectsUnknownCategory(t *testing.T) {
	t.Parallel()

	service := NewShadowAIService(&stubDetectionReader{}, &stubLibraryReader{list: &aitargets.OrganizationList{}}, stubAuthorizer{}, allowingBudget())
	require.NotNil(t, service)

	_, err := service.ListLibrary(t.Context(), shadowAIPrincipal(), ListAIScanLibraryInput{Category: "not-a-category"})
	require.ErrorIs(t, err, ErrShadowAIInvalid)
	require.NotErrorIs(t, err, ErrShadowAIUnavailable)
}

func TestShadowAIServiceRequiresEveryDependency(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewShadowAIService(nil, &stubLibraryReader{}, stubAuthorizer{}, allowingBudget()))
	require.Nil(t, NewShadowAIService(&stubDetectionReader{}, nil, stubAuthorizer{}, allowingBudget()))
	require.Nil(t, NewShadowAIService(&stubDetectionReader{}, &stubLibraryReader{}, nil, allowingBudget()))
	require.Nil(t, NewShadowAIService(&stubDetectionReader{}, &stubLibraryReader{}, stubAuthorizer{}, OperationBudget{}))
}

// TestShadowAIListToolsProjectsTheUnenforceableHalf covers the distinction the
// feature exists to make. A tool publishing no client ID metadata document
// reads unreviewed because no decision about it can be enforced, and a
// detection may carry no access summary at all. Both must project as
// unreviewed with enforceable false, and the earlier test only pinned the
// blocked-and-enforceable half.
func TestShadowAIListToolsProjectsTheUnenforceableHalf(t *testing.T) {
	t.Parallel()

	detections := &stubDetectionReader{result: &accessgen.ListAIDetectionsResult{Detections: []*accessgen.AIDetection{
		{
			TargetID:    "unreviewed-but-enforceable",
			DisplayName: "Reviewable",
			Category:    "assistant",
			Signals:     []string{"running"},
			Access:      &accessgen.AIToolAccessSummary{State: "unreviewed", Enforceable: true},
		},
		{
			// The headline case: it reads unreviewed BECAUSE no decision about
			// it could be enforced, not because nobody has looked at it. A
			// caller has to be able to tell this apart from the row above,
			// and enforceable is the only field that says which it is.
			TargetID:    "unreviewed-because-unenforceable",
			DisplayName: "Not blockable",
			Category:    "harness",
			Signals:     []string{"installed"},
			Access:      &accessgen.AIToolAccessSummary{State: "unreviewed", Enforceable: false},
		},
		{
			TargetID:    "no-summary-at-all",
			DisplayName: "Unknown",
			Category:    "local_model",
			Signals:     []string{"installed"},
			Access:      nil,
		},
	}}}
	service := NewShadowAIService(detections, &stubLibraryReader{}, stubAuthorizer{}, allowingBudget())
	require.NotNil(t, service)

	out, err := service.ListTools(t.Context(), shadowAIPrincipal(), ListShadowAIToolsInput{})
	require.NoError(t, err)
	require.Len(t, out.Tools, 3)

	require.Equal(t, "unreviewed", out.Tools[0].State)
	require.True(t, out.Tools[0].Enforceable, "nobody has decided, but a decision could be enforced")

	require.Equal(t, "unreviewed", out.Tools[1].State, "a tool publishing no document always reads unreviewed")
	require.False(t, out.Tools[1].Enforceable, "and enforceable is what says why")

	require.Empty(t, out.Tools[2].State, "a detection with no summary carries no state")
	require.False(t, out.Tools[2].Enforceable, "and nothing about it can be enforced")

	// The two unreviewed rows differ only in enforceable, which is the whole
	// distinction the feature exists to draw.
	require.Equal(t, out.Tools[0].State, out.Tools[1].State)
	require.NotEqual(t, out.Tools[0].Enforceable, out.Tools[1].Enforceable)
}

// TestShadowAIListToolsRejectsUnknownCategory: an unknown category is a
// mistake the caller can correct, so it must reach the invalid-argument
// refusal rather than fall through to the read and surface as a raw handler
// error. ListLibrary already behaved this way; ListTools did not.
func TestShadowAIListToolsRejectsUnknownCategory(t *testing.T) {
	t.Parallel()

	detections := &stubDetectionReader{result: &accessgen.ListAIDetectionsResult{Detections: nil}}
	service := NewShadowAIService(detections, &stubLibraryReader{}, stubAuthorizer{}, allowingBudget())
	require.NotNil(t, service)

	_, err := service.ListTools(t.Context(), shadowAIPrincipal(), ListShadowAIToolsInput{Category: "not-a-category"})
	require.ErrorIs(t, err, ErrShadowAIInvalid)
	require.NotErrorIs(t, err, ErrShadowAIUnavailable, "the service is configured; only the argument is wrong")
	require.Empty(t, detections.lastInput.OrganizationID, "the read must not run for an argument that cannot be valid")
}
