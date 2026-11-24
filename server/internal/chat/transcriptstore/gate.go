package transcriptstore

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// provisionTimeout bounds one organization's transcript key provisioning. It
// runs while a message transaction is open, and minting a key is a few KMS and
// database round trips, so 10 seconds is generous without letting a hung KMS
// call hold writes indefinitely.
const provisionTimeout = 10 * time.Second

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
	done := g.provisioning.DoChan(organizationID, func() (any, error) {
		if _, ok := g.ready.Load(organizationID); ok {
			return nil, nil
		}
		// Every waiting write shares this call, so it is bounded on its own
		// rather than by the first caller's context.
		provisionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout)
		defer cancel()
		if err := g.provisioner.EnsureUseCase(provisionCtx, organizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
			return nil, fmt.Errorf("provision chat transcript key: %w", err)
		}
		g.ready.Store(organizationID, struct{}{})
		return nil, nil
	})
	var err error
	select {
	case <-ctx.Done():
		err = fmt.Errorf("wait for chat transcript key: %w", ctx.Err())
	case result := <-done:
		err = result.Err
	}
	if err != nil {
		g.logger.ErrorContext(ctx, "chat transcript capture stays off for this write",
			attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return false
	}
	return true
}
