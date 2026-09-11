package mcp_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A committed grant is probed without a click: the consent page renders Verified on its first view.
func TestConsentAutoVerify_FreshGrantIsProbedOnCommit(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "aim204-auto")
	require.Contains(t, renderConsent(t, fx), "Not yet verified")
	require.Empty(t, fx.member.drain(), "rendering alone never probes")

	fx.ti.service.VerifyRemoteGrantOn(ctx, fx.endpoint, remotesessions.RemoteGrant{
		ParentChallengeID:     fx.stateID,
		UserSessionIssuerID:   fx.endpoint.UserSessionIssuerID,
		RemoteSessionClientID: fx.clientID,
		Subject:               fx.subject,
	})
	require.NoError(t, fx.ti.service.Shutdown(ctx))

	requireProbe(t, fx.member.drain(), "token-aim204-auto", true, true)
	sess := storedSession(t, ctx, fx)
	require.Equal(t, string(remotesessions.ValidationOutcomeValid), sess.ValidationStatus.String)
	require.Contains(t, renderConsent(t, fx), `data-validation="valid"`)
	require.Equal(t, map[string]int64{"valid": 1}, validationCounts(t, fx.reader))
}

// A rejecting member is recorded as such on commit, with the Reconnect control ready on the first view.
func TestConsentAutoVerify_RejectedGrantShowsReconnect(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "aim204-auto-401")
	fx.member.set(memberForbids)

	fx.ti.service.VerifyRemoteGrantOn(ctx, fx.endpoint, remotesessions.RemoteGrant{
		ParentChallengeID:     fx.stateID,
		UserSessionIssuerID:   fx.endpoint.UserSessionIssuerID,
		RemoteSessionClientID: fx.clientID,
		Subject:               fx.subject,
	})
	require.NoError(t, fx.ti.service.Shutdown(ctx))

	sess := storedSession(t, ctx, fx)
	require.Equal(t, string(remotesessions.ValidationOutcomeRejectedByMember), sess.ValidationStatus.String)
	page := renderConsent(t, fx)
	require.Contains(t, page, "Rejected by "+fx.name+", reconnect")
	require.Contains(t, page, `data-connect-link > Reconnect`)
}

// A grant that does not belong to its consent challenge, or that the endpoint does not bind, is left alone: no probe, no verdict, the manual Verify still works.
func TestConsentAutoVerify_UnplaceableGrantIsSkipped(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "aim204-auto-skip")
	other := urn.NewUserSubject("someone-else-" + uuid.NewString())

	fx.ti.service.VerifyRemoteGrant(ctx, remotesessions.RemoteGrant{ParentChallengeID: uuid.NewString(), UserSessionIssuerID: fx.endpoint.UserSessionIssuerID, RemoteSessionClientID: fx.clientID, Subject: fx.subject})
	fx.ti.service.VerifyRemoteGrant(ctx, remotesessions.RemoteGrant{ParentChallengeID: fx.stateID, UserSessionIssuerID: uuid.New(), RemoteSessionClientID: fx.clientID, Subject: fx.subject})
	fx.ti.service.VerifyRemoteGrant(ctx, remotesessions.RemoteGrant{ParentChallengeID: fx.stateID, UserSessionIssuerID: fx.endpoint.UserSessionIssuerID, RemoteSessionClientID: fx.clientID, Subject: other})
	fx.ti.service.VerifyRemoteGrantOn(ctx, fx.endpoint, remotesessions.RemoteGrant{ParentChallengeID: fx.stateID, UserSessionIssuerID: fx.endpoint.UserSessionIssuerID, RemoteSessionClientID: uuid.New(), Subject: fx.subject})
	require.NoError(t, fx.ti.service.Shutdown(ctx))

	require.Empty(t, fx.member.drain())
	require.False(t, storedSession(t, ctx, fx).ValidationStatus.Valid)
	require.Empty(t, validationCounts(t, fx.reader))
}

// heldProbeFixture is a standalone fixture whose member holds initialize until release is called, with a callback wait far shorter than the probe.
func heldProbeFixture(t *testing.T, prefix string) (context.Context, validationFixture, remotesessions.RemoteGrant, chan struct{}, func()) {
	t.Helper()
	ctx, fx := seedStandaloneValidationFixtureWith(t, prefix, mcp.MetaRuntimeConfig{MemberCallTimeout: 0, ValidationTimeout: 10 * time.Second, AutoVerifyWait: 50 * time.Millisecond})
	reached := make(chan struct{})
	released := make(chan struct{})
	var reachOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(released) }) }
	t.Cleanup(release)
	fx.member.onInitialize = func() error {
		reachOnce.Do(func() { close(reached) })
		<-released
		return nil
	}
	grant := remotesessions.RemoteGrant{
		ParentChallengeID:     fx.stateID,
		UserSessionIssuerID:   fx.endpoint.UserSessionIssuerID,
		RemoteSessionClientID: fx.clientID,
		Subject:               fx.subject,
	}
	return ctx, fx, grant, reached, release
}

func requireReached(t *testing.T, reached <-chan struct{}) {
	t.Helper()
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the probe never reached the member")
	}
}

// The callback redirects before a slow probe finishes: the page it lands on
// reads Verifying… and declares the budget its script refreshes under, stays
// open, and reads Verified once the probe is done, without a click.
func TestConsentAutoVerify_PendingUntilProbeFinishes(t *testing.T) {
	t.Parallel()

	ctx, fx, grant, reached, release := heldProbeFixture(t, "aim204-auto-pending")

	// What HandleRemoteLoginCallback runs between committing the grant and redirecting.
	deadline := fx.ti.service.VerifyRemoteGrant(ctx, grant)
	requireReached(t, reached)

	target := "/mcp/" + fx.endpoint.Slug + "/connect?state=" + fx.stateID +
		"&verifying_client=" + fx.clientID.String() +
		"&verifying_until=" + strconv.FormatInt(deadline.UnixMilli(), 10)
	page := renderConsentAt(t, fx, target)
	require.Contains(t, page, `data-validation="pending"`)
	require.Contains(t, page, `Connected · <span class="text-muted-foreground" data-validation="pending" >Verifying…`)
	require.Contains(t, page, `data-verify-deadline-ms="`+strconv.FormatInt(deadline.UnixMilli(), 10)+`"`)
	require.NotContains(t, page, "Not yet verified")
	require.NotContains(t, page, "data-auto-close", "a first-party tab stays open while a verdict is pending")

	release()
	require.NoError(t, fx.ti.service.Shutdown(ctx))

	page = renderConsent(t, fx)
	require.NotContains(t, page, `data-validation="pending"`)
	require.Contains(t, page, `data-validation="valid"`)
	require.Contains(t, page, "data-auto-close", "the verdict completes the first-party connection")
	require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)
}

// A detached verifier must not probe a replacement credential committed after
// the callback grant it was admitted for.
func TestConsentAutoVerify_ChangedGrantIsSkipped(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "aim204-auto-stale")
	sess := storedSession(t, ctx, fx)
	fx.ti.service.VerifyRemoteGrantOn(ctx, fx.endpoint, remotesessions.RemoteGrant{
		ParentChallengeID:      fx.stateID,
		UserSessionIssuerID:    fx.endpoint.UserSessionIssuerID,
		RemoteSessionClientID:  fx.clientID,
		Subject:                fx.subject,
		RemoteSessionID:        uuid.New(),
		RemoteSessionUpdatedAt: sess.UpdatedAt.Time,
	})

	require.Empty(t, fx.member.drain())
	require.False(t, storedSession(t, ctx, fx).ValidationStatus.Valid)
}

// Shutdown waits for a probe in flight and admits none after it.
func TestConsentAutoVerify_ShutdownDrainsAndClosesAdmission(t *testing.T) {
	t.Parallel()

	ctx, fx, grant, reached, release := heldProbeFixture(t, "aim204-auto-shutdown")
	fx.ti.service.VerifyRemoteGrant(ctx, grant)
	requireReached(t, reached)

	stopped := make(chan error, 1)
	go func() { stopped <- fx.ti.service.Shutdown(ctx) }()
	select {
	case err := <-stopped:
		t.Fatalf("shutdown returned while the probe was still held: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	release()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not return once the probe finished")
	}
	requireProbe(t, fx.member.drain(), "token-aim204-auto-shutdown", true, true)

	fx.ti.service.VerifyRemoteGrant(ctx, grant)
	require.Empty(t, fx.member.drain(), "nothing is admitted after shutdown")
	require.NotContains(t, renderConsent(t, fx), `data-validation="pending"`)
	require.Equal(t, map[string]int64{"valid": 1}, validationCounts(t, fx.reader))
}

// Placement can consume part of the callback's budget; the verdict is still written inside what is left, even when a leg hangs.
func TestConsentAutoVerify_PreservesRemainingBudgetForVerdict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode validationMemberMode
		want string
	}{
		{mode: memberHangsAck, want: "unknown"},
		{mode: memberHangsClose, want: "valid"},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Parallel()
			ctx, fx := seedStandaloneValidationFixture(t, "auto-budget-"+string(tc.mode))
			fx.member.set(tc.mode)
			// Model placement having consumed half of the configured two-second probe budget.
			remaining, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			fx.ti.service.VerifyRemoteGrantOn(remaining, fx.endpoint, remotesessions.RemoteGrant{
				ParentChallengeID:     fx.stateID,
				UserSessionIssuerID:   fx.endpoint.UserSessionIssuerID,
				RemoteSessionClientID: fx.clientID,
				Subject:               fx.subject,
			})
			require.NoError(t, remaining.Err(), "cleanup must leave time to persist the verdict")
			requireProbe(t, fx.member.drain(), "token-auto-budget-"+string(tc.mode), true, true)
			require.Equal(t, tc.want, storedSession(t, ctx, fx).ValidationStatus.String)
		})
	}
}
