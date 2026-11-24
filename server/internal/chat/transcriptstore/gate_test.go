package transcriptstore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type countingProvisioner struct {
	calls int
	err   error
}

func (p *countingProvisioner) EnsureUseCase(context.Context, string, orgencryption.UseCase, orgencryption.Actor) error {
	p.calls++
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
	require.Equal(t, 1, provisioner.calls)
	require.False(t, gate.OrganizationReady(t.Context(), ""))
}

func TestCaptureGateSkipsCaptureWhenProvisioningFails(t *testing.T) {
	t.Parallel()
	provisioner := &countingProvisioner{err: errors.New("kms unavailable")}
	gate := NewCaptureGate(testenv.NewLogger(t), &feature.InMemory{}, provisioner)

	require.False(t, gate.OrganizationReady(t.Context(), "org_a"))
	// A failure is not cached, so the next write tries again.
	require.False(t, gate.OrganizationReady(t.Context(), "org_a"))
	require.Equal(t, 2, provisioner.calls)
}
