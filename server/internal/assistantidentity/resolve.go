package assistantidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Resolve reads tenant-pinned history and every live authority dependency from
// one repeatable-read snapshot. Storage failures are never legacy fallbacks.
func (s *Service) Resolve(ctx context.Context, db DB, org string, project, assistant, trigger uuid.UUID) (Resolution, error) {
	tx, err := readSnapshot(ctx, db)
	if err != nil {
		return Resolution{}, err
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	result, err := s.resolve(ctx, tx, org, project, assistant, trigger)
	if err != nil {
		return Resolution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Resolution{}, fmt.Errorf("commit identity read snapshot: %w", err)
	}
	return result, nil
}

func (s *Service) resolve(ctx context.Context, tx pgx.Tx, org string, project, assistant, trigger uuid.UUID) (Resolution, error) {
	if s == nil || org == "" || project == uuid.Nil || assistant == uuid.Nil || trigger == uuid.Nil {
		return Resolution{}, ErrInvalidIdentity
	}
	q := repo.New(tx)
	ab, aerr := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if aerr != nil && !errors.Is(aerr, pgx.ErrNoRows) {
		return Resolution{}, fmt.Errorf("resolve assistant history: %w", aerr)
	}
	tb, terr := q.GetTriggerBinding(ctx, repo.GetTriggerBindingParams{PlatformIssuer: s.issuer, PlatformJwksUri: s.jwksURI, OrganizationID: org, ProjectID: project, TriggerID: trigger})
	if terr != nil && !errors.Is(terr, pgx.ErrNoRows) {
		return Resolution{}, fmt.Errorf("resolve trigger history: %w", terr)
	}
	// Original identifiers remain authoritative even when all FK mirrors were
	// nulled by hard deletes. Never reinterpret retained history as legacy.
	if aerr == nil && ab.Tombstoned {
		return Resolution{State: Tombstoned, Identity: nil}, nil
	}
	if terr == nil && (tb.Deleted || !tb.Eligible) {
		return Resolution{State: Tombstoned, Identity: nil}, nil
	}
	if aerr == nil && !ab.Eligible {
		return Resolution{State: Unavailable, Identity: nil}, nil
	}
	if aerr == nil && errors.Is(terr, pgx.ErrNoRows) {
		return Resolution{}, ErrBrokenMapping
	}
	if aerr == nil && terr == nil {
		a, err := q.GetAssistant(ctx, repo.GetAssistantParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
		if err != nil {
			return Resolution{}, resourceError("resolve bound assistant lifecycle", err)
		}
		if a.Status != "active" {
			return Resolution{State: Unavailable, Identity: nil}, nil
		}
		if tb.OriginalAssistantBindingID != ab.ID || tb.AssistantBindingGeneration != ab.Generation {
			return Resolution{}, ErrBrokenMapping
		}
		id := Identity{OrganizationID: org, ProjectID: project, AssistantID: assistant, AgentID: ab.OriginalAgentID,
			TriggerID: trigger, IssuerID: tb.OriginalWorkloadIssuerID, Subject: tb.Subject,
			AssistantGeneration: ab.Generation, TriggerGeneration: tb.Generation}
		return Resolution{State: Active, Identity: &id}, nil
	}
	if terr == nil {
		return Resolution{}, ErrBrokenMapping
	}
	// The legacy state is meaningful only for a real same-tenant target pair.
	a, err := q.GetAssistant(ctx, repo.GetAssistantParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if err != nil {
		return Resolution{}, resourceError("resolve legacy assistant", err)
	}
	t, err := q.GetTrigger(ctx, repo.GetTriggerParams{OrganizationID: org, ProjectID: project, TriggerID: trigger})
	if err != nil {
		return Resolution{}, resourceError("resolve legacy trigger", err)
	}
	if a.Deleted || !a.ProjectLive || t.Deleted {
		return Resolution{}, ErrNotFound
	}
	if t.DefinitionSlug == "wake" {
		return Resolution{}, ErrInvalidIdentity
	}
	if t.TargetKind != "assistant" || t.TargetRef != assistant.String() {
		return Resolution{}, ErrBrokenMapping
	}
	return Resolution{State: NeverConfigured, Identity: nil}, nil
}

// Validate rejects stale generations, owner transfers, and any live
// lifecycle, target, assignment, admission, issuer, or tenancy inconsistency.
func (s *Service) Validate(ctx context.Context, db DB, expected Identity) error {
	resolved, err := s.Resolve(ctx, db, expected.OrganizationID, expected.ProjectID, expected.AssistantID, expected.TriggerID)
	if err != nil {
		return err
	}
	return matchExpected(resolved, expected)
}

func matchExpected(resolved Resolution, expected Identity) error {
	if resolved.State != Active || resolved.Identity == nil || *resolved.Identity != expected {
		return ErrInvalidIdentity
	}
	return nil
}

// SnapshotCeiling validates and derives the bounded policy within the same
// snapshot. Subsequent grant expansion cannot mutate these canonical bytes.
func (s *Service) SnapshotCeiling(ctx context.Context, db DB, expected Identity) (CeilingSnapshot, error) {
	tx, err := readSnapshot(ctx, db)
	if err != nil {
		return CeilingSnapshot{}, err
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	resolved, err := s.resolve(ctx, tx, expected.OrganizationID, expected.ProjectID, expected.AssistantID, expected.TriggerID)
	if err != nil {
		return CeilingSnapshot{}, err
	}
	if err := matchExpected(resolved, expected); err != nil {
		return CeilingSnapshot{}, err
	}
	caps, err := ConfiguredCapabilities(ctx, tx, expected.OrganizationID, expected.ProjectID, expected.AssistantID)
	if err != nil {
		return CeilingSnapshot{}, err
	}
	policy, err := runtimepolicy.LoadAgentPolicy(ctx, tx, expected.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, expected.AgentID.String()))
	if err != nil {
		return CeilingSnapshot{}, fmt.Errorf("load ceiling agent policy: %w", err)
	}
	grants, err := runtimepolicy.DelegableGrants(caps, policy, policy)
	if err != nil {
		return CeilingSnapshot{}, fmt.Errorf("derive assistant ceiling: %w", err)
	}
	version := runtimepolicy.CurrentDelegatedPolicyVersion
	delegated, err := runtimepolicy.NewDelegatedPolicy(version, grants)
	if err != nil {
		return CeilingSnapshot{}, fmt.Errorf("construct assistant ceiling: %w", err)
	}
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(version, delegated)
	if err != nil {
		return CeilingSnapshot{}, fmt.Errorf("encode assistant ceiling: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if err := tx.Commit(ctx); err != nil {
		return CeilingSnapshot{}, fmt.Errorf("commit ceiling snapshot: %w", err)
	}
	return CeilingSnapshot{EncodingVersion: version, Policy: encoded, Digest: hex.EncodeToString(digest[:])}, nil
}

func readSnapshot(ctx context.Context, db DB) (pgx.Tx, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, fmt.Errorf("begin identity read snapshot: %w", err)
	}
	return tx, nil
}
