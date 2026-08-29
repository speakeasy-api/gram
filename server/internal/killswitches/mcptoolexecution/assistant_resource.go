package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

// AssistantSource is produced only from a validated assistant runtime
// principal. Caller-provided assistant identifiers must never reach Derive.
type AssistantSource struct {
	AssistantID uuid.UUID
}

// AssistantResourceAdapter validates canonical, active organization-owned
// assistants without consulting creator or owner fields.
type AssistantResourceAdapter struct {
	db *pgxpool.Pool
}

func NewAssistantResourceAdapter(db *pgxpool.Pool) *AssistantResourceAdapter {
	return &AssistantResourceAdapter{db: db}
}

var _ killswitches.ResourceAdapter = (*AssistantResourceAdapter)(nil)

func (*AssistantResourceAdapter) Kind() killswitches.ResourceKind { return ResourceKindAssistant }

func (*AssistantResourceAdapter) Canonicalize(_ killswitches.OrganizationID, input string) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	id, err := uuid.Parse(strings.TrimSpace(input))
	if err != nil || id == uuid.Nil {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey](), nil
	}
	result, err := killswitches.NewCanonicalizationResult(killswitches.ResourceKey(id.String()))
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("canonicalize assistant resource: %w", err)
	}
	return result, nil
}

func (a *AssistantResourceAdapter) ValidateCurrentOrganization(ctx context.Context, organizationID killswitches.OrganizationID, key killswitches.ResourceKey) (bool, error) {
	id, err := uuid.Parse(string(key))
	if err != nil || id == uuid.Nil || id.String() != string(key) || organizationID == "" {
		return false, nil
	}
	row, err := assistantrepo.New(a.db).GetAssistantForDispatch(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("load assistant resource: %w", err)
	}
	return row.OrganizationID == string(organizationID) && row.Status == "active", nil
}

func (a *AssistantResourceAdapter) Derive(ctx context.Context, organizationID killswitches.OrganizationID, source any) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	src, ok := source.(AssistantSource)
	if !ok {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("unsupported assistant resource source type %T", source)
	}
	if src.AssistantID == uuid.Nil || organizationID == "" {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("assistant and organization are required")
	}
	key := killswitches.ResourceKey(src.AssistantID.String())
	valid, err := a.ValidateCurrentOrganization(ctx, organizationID, key)
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, err
	}
	if !valid {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("assistant is not active in the organization")
	}
	result, err := killswitches.NewCanonicalizationResult(key)
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("derive assistant resource: %w", err)
	}
	return result, nil
}

func assistantResourceFixtures() []killswitches.ResourceCanonicalizationFixture {
	return []killswitches.ResourceCanonicalizationFixture{
		{OrganizationID: "org_fixture", Input: "0198A1B2-C3D4-7000-8000-0123456789AB", Expected: supportedKey(killswitches.ResourceKey("0198a1b2-c3d4-7000-8000-0123456789ab"))},
		{OrganizationID: "org_fixture", Input: "not-an-assistant-id", Expected: killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey]()},
	}
}
