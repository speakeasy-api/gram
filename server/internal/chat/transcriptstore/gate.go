package transcriptstore

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

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

	// provisioning collapses concurrent first writes for one organization into
	// a single key provisioning call.
	provisioning singleflight.Group
}

func NewCaptureGate(logger *slog.Logger, flags feature.Provider, provisioner keyProvisioner) *CaptureGate {
	return &CaptureGate{logger: logger, flags: flags, provisioner: provisioner, ready: sync.Map{}, provisioning: singleflight.Group{}}
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
// once per process; concurrent first writes share one provisioning call. A failure is logged and reads as not ready, so the write
// commits without a shadow instead of failing.
func (g *CaptureGate) OrganizationReady(ctx context.Context, organizationID string) bool {
	if g == nil || g.provisioner == nil || organizationID == "" {
		return false
	}
	if _, ok := g.ready.Load(organizationID); ok {
		return true
	}
	_, err, _ := g.provisioning.Do(organizationID, func() (any, error) {
		if _, ok := g.ready.Load(organizationID); ok {
			return nil, nil
		}
		// Detach from the first caller's cancellation: every waiting write
		// shares this result.
		if err := g.provisioner.EnsureUseCase(context.WithoutCancel(ctx), organizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
			return nil, fmt.Errorf("provision chat transcript key: %w", err)
		}
		g.ready.Store(organizationID, struct{}{})
		return nil, nil
	})
	if err != nil {
		g.logger.ErrorContext(ctx, "chat transcript capture stays off for this write",
			attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return false
	}
	return true
}
