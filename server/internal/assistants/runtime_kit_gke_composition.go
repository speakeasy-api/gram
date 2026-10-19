package assistants

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// KitGKECompositionConfig opts new version-3 conversations into the production
// runtime. It does not convert or import existing conversations. MCP uses the
// same issuer-bound workload registration as local Kit, never a user's token.
type KitGKECompositionConfig struct {
	KitLocalCompositionConfig
	GKE       GKERuntimeConfig
	Provision KitGKEProvisionInput
	// Snapshots must verify the deployed CRD contract and cold restore evidence.
	// Nil deliberately leaves checkpoint/restore unavailable.
	Snapshots KitGKECheckpointDriver
}

func validateKitGKEComposition(c KitGKECompositionConfig) error {
	if c.Environment == "" || c.Environment == "local" {
		return errors.New("Kit GKE composition requires an explicit non-local environment")
	}
	if c.Network != "" || c.ExtraCACertFile != "" || c.AllowInsecureInference {
		return errors.New("Kit GKE composition rejects local network and TLS overrides")
	}
	if err := validateKitComposition(c.KitLocalCompositionConfig); err != nil {
		return err
	}
	if c.GKE.Dynamic == nil || c.GKE.Namespace == "" || len(c.GKE.RunnerCIDRBlocks) == 0 {
		return errors.New("Kit GKE composition requires Kubernetes, namespace and explicit gateway CIDRs")
	}
	return nil
}

// InstallKitGKERuntime installs only the v3 dispatch branch. All startup
// credentials are minted under the existing runtime admission lock. The
// production gateway uses the same canonical transcript reducer and session
// binding checks as local Kit; no legacy protocol fallback is installed.
// Platform MCP parity: provisioning/checkpoint operations are internal lifecycle
// machinery, not member operations. Existing assistant tools remain unchanged.
func (s *ServiceCore) InstallKitGKERuntime(c KitGKECompositionConfig) error {
	if err := validateKitGKEComposition(c); err != nil {
		return err
	}
	if s.guardianPolicy == nil || s.chatWriter == nil || s.assistantTokens == nil || s.encryptionClient == nil {
		return errors.New("Kit requires guarded HTTP, transcript, inference and encrypted gateway credential services")
	}
	if c.GKE.GuestPort == 0 {
		c.GKE.GuestPort = 8081
	}
	c.Provision.Image = c.Image
	c.Provision.GuestPort = int32(c.GKE.GuestPort)
	backend, err := NewKitGKERuntimeBackend(KitGKERuntimeConfig{
		GKE: c.GKE, Provision: c.Provision, Snapshots: c.Snapshots,
		Fence: s.fenceKitRuntimeAlreadyLocked,
		Bootstrap: func(ctx context.Context, r assistantRuntimeRecord) (KitLocalBootstrap, error) {
			return s.kitCompositionBootstrap(ctx, r, c.KitLocalCompositionConfig)
		},
		Protocol: s.kitProtocolFactory(c.KitLocalCompositionConfig, c.GKE.RunnerCIDRBlocks),
	})
	if err != nil {
		return err
	}
	if err := s.installKitRuntime(backend); err != nil {
		return err
	}
	s.newAssistantRuntimeVersion = 3
	return nil
}

// fenceKitRuntimeAlreadyLocked revokes runtime-scoped external authority before
// capturing or deleting a settled process. Failure never authorizes teardown.
// It must run under the same admission/recycling lock as bootstrap.
func (s *ServiceCore) fenceKitRuntimeAlreadyLocked(ctx context.Context, r assistantRuntimeRecord) error {
	if r.RuntimeVersion != 3 {
		return ErrKitRuntimeBinding
	}
	q := assistantrepo.New(s.db)
	row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: r.ProjectID, ID: r.ID})
	if err != nil {
		return err
	}
	if row.AssistantID != r.AssistantID || row.RuntimeVersion != 3 {
		return ErrKitRuntimeBinding
	}
	if err := s.assistantTokens.RevokeRuntimeInferenceCredentials(ctx, r.ProjectID, r.ID); err != nil {
		return err
	}
	// Retirement is already a durable admission fence for workload OAuth and
	// gateway binding reads. Do not try to issue a new gateway key for a row
	// which the canonical credential query correctly refuses to resurrect.
	if row.Ended || row.Deleted {
		return nil
	}
	a, err := q.GetAssistant(ctx, assistantrepo.GetAssistantParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID})
	if err != nil {
		return err
	}
	_, err = usersessionsrepo.New(s.db).RevokeAssistantRuntimeWorkloadClients(ctx, usersessionsrepo.RevokeAssistantRuntimeWorkloadClientsParams{
		RuntimeID: uuid.NullUUID{UUID: r.ID, Valid: true}, OrganizationID: conv.ToPGText(a.OrganizationID), ProjectID: r.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("fence Kit workload credentials: %w", err)
	}
	_, err = s.rotateKitGatewayTokenAlreadyLocked(ctx, r)
	return err
}
