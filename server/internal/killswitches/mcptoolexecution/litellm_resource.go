package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	litellmrepo "github.com/speakeasy-api/gram/server/internal/litellm/repo"
)

// LiteLLMInstanceSource contains only callback authentication values. The
// adapter derives the resource from an uncached, project-scoped lookup of the
// current managed integration key; caller-supplied LiteLLM metadata is unused.
type LiteLLMInstanceSource struct {
	ProjectID uuid.UUID
	APIKeyID  uuid.UUID
}

// LiteLLMInstanceResourceAdapter canonicalizes active managed LiteLLM instance
// IDs. Derive is the authoritative callback path and must be used for policy
// evaluation; Canonicalize exists for selected-resource prescription input.
type LiteLLMInstanceResourceAdapter struct {
	db *pgxpool.Pool
}

func NewLiteLLMInstanceResourceAdapter(db *pgxpool.Pool) *LiteLLMInstanceResourceAdapter {
	return &LiteLLMInstanceResourceAdapter{db: db}
}

func (*LiteLLMInstanceResourceAdapter) Kind() killswitches.ResourceKind {
	return ResourceKindLiteLLMInstance
}

func (*LiteLLMInstanceResourceAdapter) Canonicalize(_ killswitches.OrganizationID, input string) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	return canonicalUUIDResourceKey(input)
}

func (a *LiteLLMInstanceResourceAdapter) ValidateCurrentOrganization(ctx context.Context, organizationID killswitches.OrganizationID, key killswitches.ResourceKey) (bool, error) {
	id, err := uuid.Parse(string(key))
	if err != nil || id == uuid.Nil || id.String() != string(key) || organizationID == "" || a == nil || a.db == nil {
		return false, nil
	}
	active, err := litellmrepo.New(a.db).IsActiveLiteLLMInstanceInOrganization(ctx, litellmrepo.IsActiveLiteLLMInstanceInOrganizationParams{
		ID: id, OrganizationID: string(organizationID),
	})
	if err != nil {
		return false, fmt.Errorf("check active LiteLLM instance ownership: %w", err)
	}
	return active, nil
}

func (a *LiteLLMInstanceResourceAdapter) Derive(ctx context.Context, organizationID killswitches.OrganizationID, source any) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	src, ok := source.(LiteLLMInstanceSource)
	if !ok || organizationID == "" || src.ProjectID == uuid.Nil || src.APIKeyID == uuid.Nil || a == nil || a.db == nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, errors.New("authoritative LiteLLM instance source is required")
	}
	instanceID, err := litellmrepo.New(a.db).GetActiveLiteLLMInstanceIDByAPIKey(ctx, litellmrepo.GetActiveLiteLLMInstanceIDByAPIKeyParams{
		OrganizationID: string(organizationID),
		ProjectID:      src.ProjectID,
		ApiKeyID:       src.APIKeyID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey](), nil
	}
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("resolve active managed LiteLLM instance: %w", err)
	}
	return a.Canonicalize(organizationID, instanceID.String())
}

func litellmResourceFixtures() []killswitches.ResourceCanonicalizationFixture {
	return []killswitches.ResourceCanonicalizationFixture{
		{OrganizationID: "fixture-org", Input: " 018F5F59-13AC-7A82-B3D6-E241722C675D ", Expected: supportedKey(killswitches.ResourceKey("018f5f59-13ac-7a82-b3d6-e241722c675d"))},
		{OrganizationID: "fixture-org", Input: "not-a-uuid", Expected: killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey]()},
	}
}
