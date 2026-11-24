package transcriptstore

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// keyProvisioner creates an organization's data key for a use case when it has
// none. *orgencryption.Provisioner satisfies it.
type keyProvisioner interface {
	EnsureUseCase(ctx context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error
}

// CaptureGate turns shadow capture on per project from the
// chat-transcript-shadow-capture PostHog flag. Before a project's first
// capture it makes sure the organization has its transcript data key, so
// turning the flag on never fails a message write for a missing key.
type CaptureGate struct {
	logger      *slog.Logger
	flags       feature.Provider
	provisioner keyProvisioner

	// ready holds organization ids whose transcript key is known to exist.
	ready sync.Map
}

func NewCaptureGate(logger *slog.Logger, flags feature.Provider, provisioner keyProvisioner) *CaptureGate {
	return &CaptureGate{logger: logger, flags: flags, provisioner: provisioner, ready: sync.Map{}}
}

// ProjectEnabled evaluates the flag from locally cached definitions only, so a
// write never waits on PostHog; an error or inconclusive result reads as off.
func (g *CaptureGate) ProjectEnabled(ctx context.Context, projectID uuid.UUID) bool {
	if g == nil || g.flags == nil || projectID == uuid.Nil {
		return false
	}
	enabled, err := g.flags.IsFlagEnabledLocal(ctx, feature.FlagChatTranscriptShadowCapture, projectID.String(), nil, nil)
	return err == nil && enabled
}

// OrganizationReady makes sure the organization has its transcript data key,
// once per process. A failure is logged and reads as not ready, so the write
// commits without a shadow instead of failing.
func (g *CaptureGate) OrganizationReady(ctx context.Context, organizationID string) bool {
	if g == nil || g.provisioner == nil || organizationID == "" {
		return false
	}
	if _, ok := g.ready.Load(organizationID); ok {
		return true
	}
	if err := g.provisioner.EnsureUseCase(ctx, organizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
		g.logger.ErrorContext(ctx, "provision chat transcript key; capture stays off for this write",
			attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return false
	}
	g.ready.Store(organizationID, struct{}{})
	return true
}
