package mcpconnectionattempts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (f *failingFixture) refusals() []Refusals {
	f.t.Helper()
	got, err := ReadManagedSignInRefusals(f.t.Context(), f.ch, logsOn, f.refusalFilter(), f.now)
	require.NoError(f.t, err)
	return got
}

func (f *failingFixture) refusalFilter() RefusalFilter {
	return RefusalFilter{OrganizationID: f.org, ProjectIDs: []uuid.UUID{f.project}, ServerIDs: []string{f.server.String()}, Since: f.now.Add(-refusalTestWindow)}
}

const refusalTestWindow = 24 * time.Hour

// Each person counts once however often they were refused, by who refused them.
func TestManagedSignInRefusals_CountsDistinctPeoplePerParty(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	a, b := person(), person()
	for _, ago := range []time.Duration{3 * time.Hour, 2 * time.Hour, time.Hour} {
		f.attempt(f.direct(AppDCR), ago, StepUpstreamEMA, CodeAccessDenied, Subject(&a))
	}
	f.attempt(f.direct(AppDCR), 90*time.Minute, StepMCPRequest, CodeAccessDenied, Subject(&b))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepMCPRequest, CodeEMAProviderRejected, Subject(&b))

	got := f.refusals()
	require.Len(t, got, 2)
	require.Equal(t, RefusedByIdentityProvider, got[0].RefusedBy)
	require.Equal(t, f.server.String(), got[0].ServerID)
	require.Equal(t, uint64(2), got[0].People)
	require.Equal(t, f.at(time.Hour), got[0].LastRefusedAt.UTC())
	require.Equal(t, RefusedByService, got[1].RefusedBy)
	require.Equal(t, uint64(1), got[1].People)
	require.Equal(t, f.at(30*time.Minute), got[1].LastRefusedAt.UTC())
}

// A refusal older than the window, one followed by an answered request, an anonymous
// caller and another reason are not shown.
func TestManagedSignInRefusals_LeavesOutOldRecoveredAnonymousAndOtherReasons(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	old, recovered, other := person(), person(), person()
	anonymous := urn.NewAnonymousSubject(uuid.NewString())
	f.attempt(f.direct(AppDCR), refusalTestWindow+time.Hour, StepUpstreamEMA, CodeAccessDenied, Subject(&old))
	f.attempt(f.direct(AppDCR), 2*time.Hour, StepUpstreamEMA, CodeAccessDenied, Subject(&recovered))
	f.answered(f.onServer(recovered, AppDCR), time.Hour)
	f.attempt(f.direct(AppDCR), time.Hour, StepUpstreamEMA, CodeAccessDenied, Subject(&anonymous))
	f.attempt(f.direct(AppDCR), time.Hour, StepMCPRequest, CodeInvalidRemoteSession, Subject(&other))
	f.attempt(ServerScope(f.org, f.project, uuid.New(), AppDCR), time.Hour, StepUpstreamEMA, CodeAccessDenied, Subject(&other))
	require.Empty(t, f.refusals())
}

// A person refused at a gateway counts on the member server.
func TestManagedSignInRefusals_CountsGatewayMembers(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(GatewayScope(f.org, f.project, uuid.New(), AppDCR), time.Hour, StepMCPRequest, CodeEMAProviderRejected, Subject(&p), Member(f.server))
	got := f.refusals()
	require.Len(t, got, 1)
	require.Equal(t, f.server.String(), got[0].ServerID)
	require.Equal(t, uint64(1), got[0].People)
}

func TestManagedSignInRefusals_LogsOff(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	_, err := ReadManagedSignInRefusals(t.Context(), f.ch, func(context.Context, string) (bool, error) { return false, nil }, f.refusalFilter(), f.now)
	require.ErrorIs(t, err, ErrLogsOff)
}

// Every refusal code counts for its party, codes that record no refusal never count, and
// another project's server is not read.
func TestManagedSignInRefusals_PartiesFromCatalog(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	for code, party := range refusingParty {
		p := person()
		f.attempt(f.direct(AppDCR), time.Hour, StepUpstreamEMA, code, Subject(&p))
		require.Equal(t, party, code.RefusedBy())
	}
	for _, code := range []Code{CodeConfigurationRequired, CodeInvalidTarget, CodeInsufficientScope, CodeMalformedResponse, CodeMalformedAssertion, CodeReauthenticationRequired, CodeInvalidRemoteSession, CodeUpstreamEMARefused, CodeUpstreamEMANotRenewed} {
		p := person()
		f.attempt(f.direct(AppDCR), time.Hour, StepUpstreamEMA, code, Subject(&p))
		require.Empty(t, code.RefusedBy())
	}
	other := person()
	f.attempt(ServerScope(f.org, uuid.New(), f.server, AppDCR), time.Hour, StepUpstreamEMA, CodeAccessDenied, Subject(&other))

	want := map[RefusedBy]uint64{RefusedByIdentityProvider: 0, RefusedByService: 0}
	for _, party := range refusingParty {
		want[party]++
	}
	got := map[RefusedBy]uint64{RefusedByIdentityProvider: 0, RefusedByService: 0}
	for _, r := range f.refusals() {
		got[r.RefusedBy] = r.People
	}
	require.Equal(t, want, got)
}

// A person never answered on the server, and one answered only before the window, still count.
func TestManagedSignInRefusals_CountsNeverAndLongAgoAnswered(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	never, longAgo := person(), person()
	f.attempt(f.direct(AppDCR), time.Hour, StepUpstreamEMA, CodeEMAProviderRejected, Subject(&never))
	f.attempt(f.direct(AppDCR), time.Hour, StepUpstreamEMA, CodeEMAProviderRejected, Subject(&longAgo))
	f.answered(f.onServer(longAgo, AppDCR), refusalTestWindow+2*time.Hour)
	got := f.refusals()
	require.Len(t, got, 1)
	require.Equal(t, uint64(2), got[0].People)
}
