package transcriptstore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type countingProvisioner struct {
	calls atomic.Int32
	err   error

	// release, when set, holds each call until it is closed.
	release chan struct{}
}

func (p *countingProvisioner) EnsureUseCase(context.Context, string, orgencryption.UseCase, orgencryption.Actor) error {
	p.calls.Add(1)
	if p.release != nil {
		<-p.release
	}
	return p.err
}

func TestCaptureGateFollowsTheProjectFlag(t *testing.T) {
	t.Parallel()
	on, off := uuid.New(), uuid.New()
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagChatTranscriptShadowCapture, on.String(), true)
	gate := NewCaptureGate(testenv.NewLogger(t), flags, &countingProvisioner{})

	require.True(t, gate.ProjectEnabled(t.Context(), on))
	require.False(t, gate.ProjectEnabled(t.Context(), off), "a project without the flag stays off")
	require.False(t, gate.ProjectEnabled(t.Context(), uuid.Nil))
	require.False(t, NewCaptureGate(testenv.NewLogger(t), nil, &countingProvisioner{}).ProjectEnabled(t.Context(), on), "no flag provider stays off")
}

func TestCaptureGateProvisionsEachOrganizationOnce(t *testing.T) {
	t.Parallel()
	provisioner := &countingProvisioner{}
	gate := NewCaptureGate(testenv.NewLogger(t), &feature.InMemory{}, provisioner)

	require.True(t, gate.OrganizationReady(t.Context(), "org_a"))
	require.True(t, gate.OrganizationReady(t.Context(), "org_a"))
	require.EqualValues(t, 1, provisioner.calls.Load())
	require.False(t, gate.OrganizationReady(t.Context(), ""))
}

func TestCaptureGateSkipsCaptureWhenProvisioningFails(t *testing.T) {
	t.Parallel()
	provisioner := &countingProvisioner{err: errors.New("kms unavailable")}
	gate := NewCaptureGate(testenv.NewLogger(t), &feature.InMemory{}, provisioner)

	require.False(t, gate.OrganizationReady(t.Context(), "org_a"))
	// A failure is not cached, so the next write tries again.
	require.False(t, gate.OrganizationReady(t.Context(), "org_a"))
	require.EqualValues(t, 2, provisioner.calls.Load())
}

func TestCaptureGateSharesProvisioningAcrossConcurrentWrites(t *testing.T) {
	t.Parallel()
	provisioner := &countingProvisioner{release: make(chan struct{})}
	gate := NewCaptureGate(testenv.NewLogger(t), &feature.InMemory{}, provisioner)

	const writes = 8
	var wg sync.WaitGroup
	results := make(chan bool, writes)
	for range writes {
		wg.Go(func() { results <- gate.OrganizationReady(t.Context(), "org_a") })
	}
	require.Eventually(t, func() bool { return provisioner.calls.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	close(provisioner.release)
	wg.Wait()
	close(results)

	for ready := range results {
		require.True(t, ready)
	}
	require.EqualValues(t, 1, provisioner.calls.Load())
}

func TestCaptureGateWaiterReturnsWhenItsWriteIsCancelled(t *testing.T) {
	t.Parallel()
	provisioner := &countingProvisioner{release: make(chan struct{})}
	t.Cleanup(func() { close(provisioner.release) })
	gate := NewCaptureGate(testenv.NewLogger(t), &feature.InMemory{}, provisioner)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.False(t, gate.OrganizationReady(ctx, "org_a"), "a cancelled write must not wait on a hung provisioning call")
}
