package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
		UserCount:   conv.Ptr(int64(3)),
		DeviceCount: conv.Ptr(int64(4)),
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
	// The counts are attribution. They are only passed through because admit
	// rechecked org:admin live on this very call.
	require.True(t, detections.lastInput.Attributed)
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
	blockable.Enabled = true
	blockable.GatewayClient.OAuthClientIDs = []string{"https://chatgpt.com/oauth/codex/client.json"}

	inert := aitargets.ZeroTarget()
	inert.ID = "acme-tool"
	inert.DisplayName = "Acme Tool"
	inert.Category = aitargets.CategoryHarness
	inert.Enabled = true

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

	require.Equal(t, "codex", out.Targets[0].TargetID)
	require.True(t, out.Targets[0].Blockable, "a target publishing a CIMD document can be blocked at the gateway")
	require.False(t, out.Targets[1].Blockable, "one publishing none cannot")
}

func TestShadowAIServiceRequiresEveryDependency(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewShadowAIService(nil, &stubLibraryReader{}, stubAuthorizer{}, allowingBudget()))
	require.Nil(t, NewShadowAIService(&stubDetectionReader{}, nil, stubAuthorizer{}, allowingBudget()))
	require.Nil(t, NewShadowAIService(&stubDetectionReader{}, &stubLibraryReader{}, nil, allowingBudget()))
	require.Nil(t, NewShadowAIService(&stubDetectionReader{}, &stubLibraryReader{}, stubAuthorizer{}, OperationBudget{}))
}
