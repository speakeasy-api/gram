package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/localaccounts/repo"
	"go.opentelemetry.io/otel/trace"
)

// LocalProfileReader reads the optional, local-only account profile table.
// It is deliberately separate from the production billing repositories.
type LocalProfileReader = repo.DBTX

// NewStubClientWithLocalProfiles explicitly enables local account profiles.
// The caller must restrict this constructor to local development. The table is
// owned by the local account tooling, not by application migrations or seeds.
func NewStubClientWithLocalProfiles(logger *slog.Logger, tracerProvider trace.TracerProvider, reader LocalProfileReader) *StubClient {
	client := NewStubClient(logger, tracerProvider)
	client.localProfiles = reader
	return client
}

func (s *StubClient) getLocalCustomerTier(ctx context.Context, orgID string) (*Tier, bool, error) {
	q := repo.New(s.localProfiles)
	exists, err := q.AccountProfilesExist(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("check local account profiles: %w", err)
	}
	if !exists {
		return new(TierPro), true, nil
	}

	marker, err := q.GetAccountProfile(ctx, conv.ToPGText(orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return new(TierPro), true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read local account profile: %w", err)
	}
	anchor := marker.Anchor
	if !anchor.Valid || anchor.InfinityModifier != pgtype.Finite || anchor.Time.IsZero() {
		return nil, false, fmt.Errorf("invalid local account profile anchor")
	}

	// Active fixtures use their actual lifecycle deadline, not their marker name
	// or anchor, so reconciliation cannot resurrect an elapsed/demoted fixture.
	switch marker.Profile {
	case "expired-trial":
		return new(TierBase), false, nil
	case "enterprise":
		return new(TierEnterprise), true, nil
	case "active-trial":
		active, err := q.AccountTrialActive(ctx, orgID)
		if err != nil {
			return nil, false, fmt.Errorf("read local trial deadline: %w", err)
		}
		if !active.Bool {
			return new(TierBase), false, nil
		}
		return new(TierEnterprise), true, nil
	case "payg":
		return new(TierPayg), true, nil
	default:
		return nil, false, fmt.Errorf("unknown local account profile")
	}
}
