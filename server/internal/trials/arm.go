package trials

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// ErrNotStartable is returned by ArmEnterpriseTrialTx when the organization
// does not exist or already holds a trial that is running, demoted, or
// converted. Those states belong to extend, re-arm, and the paid contract.
var ErrNotStartable = errors.New("organization has no startable enterprise trial")

// BundleSeeder enables the entitlements an enterprise trial grants, inside the
// caller's transaction. It travels as a function because the productfeatures
// package that implements it sits above this one in the import graph.
type BundleSeeder func(ctx context.Context, tx pgx.Tx, organizationID string) error

// ArmParams describes the trial ArmEnterpriseTrialTx grants.
type ArmParams struct {
	// OrganizationID is the organization the trial is granted to.
	OrganizationID string

	// Days is the trial's runway, counted from the moment the row is written.
	Days int32

	// Seeder enables the entitlement bundle after the trial row and the
	// organization tier are in place.
	Seeder BundleSeeder
}

// ArmedTrial reports the trial ArmEnterpriseTrialTx wrote, for the audit entry
// and cache work the caller performs afterwards.
type ArmedTrial struct {
	// Tier is the account type the trial grants.
	Tier string

	// EndsAt is when the trial's runway runs out.
	EndsAt time.Time

	// OrganizationName is the organization's display name at arming time.
	OrganizationName string

	// OrganizationSlug is the organization's slug at arming time.
	OrganizationSlug string
}

// ArmEnterpriseTrialTx is the single path that turns an organization into an
// enterprise trial: self-signup, invite acceptance, and the admin start action
// all run it. Every write joins the caller's transaction. It inserts the trial
// row, or resets an expired one to a fresh runway, moves the organization onto
// the enterprise tier with the whitelist gate cleared, and then seeds the
// entitlement bundle.
//
// Callers own everything around it: lifecycle and key locking, model provider
// key revival, the audit entry, and any post-commit cache refresh.
//
// A trial sits on the real enterprise tier rather than a tier of its own, so
// every account-type lookup downstream, including the OpenRouter credit
// ceiling, resolves against a value it already knows.
func ArmEnterpriseTrialTx(ctx context.Context, tx pgx.Tx, params ArmParams) (ArmedTrial, error) {
	q := repo.New(tx)

	started, err := q.StartTrial(ctx, repo.StartTrialParams{
		OrganizationID: params.OrganizationID,
		Tier:           string(billing.TierEnterprise),
		StartForDays:   params.Days,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ArmedTrial{}, ErrNotStartable
	case err != nil:
		return ArmedTrial{}, fmt.Errorf("start enterprise trial: %w", err)
	}

	organization, err := q.RestoreOrganizationFromTrial(ctx, repo.RestoreOrganizationFromTrialParams{
		OrganizationID: params.OrganizationID,
		AccountType:    started.Tier,
	})
	if err != nil {
		return ArmedTrial{}, fmt.Errorf("set enterprise trial account type: %w", err)
	}

	if err := params.Seeder(ctx, tx, params.OrganizationID); err != nil {
		return ArmedTrial{}, fmt.Errorf("seed enterprise trial entitlements: %w", err)
	}

	return ArmedTrial{
		Tier:             started.Tier,
		EndsAt:           started.EndsAt.Time,
		OrganizationName: organization.Name,
		OrganizationSlug: organization.Slug,
	}, nil
}
