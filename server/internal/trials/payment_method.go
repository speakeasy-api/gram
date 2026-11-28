package trials

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformswitches"
	"github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// SelfServeEligible reports whether login may grant this organization the
// same enterprise trial a zero-organization signup receives. Prospects created
// in the admin app stay behind the book-a-demo gate, and so does any
// organization that is already on a paid tier or already inside the product.
func SelfServeEligible(org orgrepo.OrganizationMetadatum) bool {
	if org.ID == "" || org.Whitelisted || org.DisabledAt.Valid {
		return false
	}
	if billing.Tier(strings.ToLower(strings.TrimSpace(org.GramAccountType))) != billing.TierBase {
		return false
	}
	return conv.FromPGTextOrEmpty[string](org.CreationSource) != orgprovision.SourcePlatformAdmin
}

// AwaitingPaymentMethod reports whether the organization can start a trial by
// adding a validated card: it has no trial lifecycle yet, and it is either
// eligible for a self-serve trial or a prospect a Speakeasy staff member
// invited people into. Partner signups never qualify, and nothing qualifies
// while staff have turned self-serve off. The rollout flag is the caller's to check; this only
// answers whether a validated card would start a trial.
func AwaitingPaymentMethod(ctx context.Context, db repo.DBTX, org orgrepo.OrganizationMetadatum) (bool, error) {
	if org.ID == "" || org.Whitelisted || org.DisabledAt.Valid {
		return false, nil
	}
	if billing.Tier(strings.ToLower(strings.TrimSpace(org.GramAccountType))) != billing.TierBase {
		return false, nil
	}

	// Staff turning self-serve off stops card-started trials too. A switch
	// that cannot be read allows self-serve, as signup does.
	if state, err := platformswitches.Get(ctx, db, platformswitches.KeySelfServeDisabled); err == nil && state.Engaged {
		return false, nil
	}

	q := repo.New(db)
	_, err := q.GetTrial(ctx, org.ID)
	switch {
	case err == nil:
		return false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return false, fmt.Errorf("read trial lifecycle: %w", err)
	}

	// A partner signup starts paid billing through its own Checkout, never a trial.
	if _, err := orgrepo.New(db).GetOrganizationPromotionApplication(ctx, org.ID); err == nil {
		return false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read promotion application: %w", err)
	}

	if SelfServeEligible(org) {
		return true, nil
	}
	invited, err := q.HasAcceptedPlatformAdminInvitation(ctx, org.ID)
	if err != nil {
		return false, fmt.Errorf("check staff invitation: %w", err)
	}
	return invited, nil
}

// Card rejection reasons recorded on trial_payment_methods.rejection_reason.
const (
	// CardRejectionPrepaid is a prepaid card, which cannot start a trial.
	CardRejectionPrepaid = "prepaid_card"
	// CardRejectionUsed is a card that already started another organization's trial.
	CardRejectionUsed = "card_used"
)

// CardRejection returns why a card cannot start the organization's trial, or
// "" when it can. A card that started another organization's trial is
// refused, so one card buys one free trial. Call it inside the transaction
// that records the card: it holds a lock on the card until that commits.
func CardRejection(ctx context.Context, db repo.DBTX, organizationID, fingerprint, funding string) (string, error) {
	if funding == "prepaid" {
		return CardRejectionPrepaid, nil
	}
	q := repo.New(db)
	if err := q.LockTrialCardFingerprint(ctx, fingerprint); err != nil {
		return "", fmt.Errorf("lock trial card: %w", err)
	}
	used, err := q.TrialCardStartedAnotherTrial(ctx, repo.TrialCardStartedAnotherTrialParams{
		CardFingerprint: fingerprint,
		OrganizationID:  organizationID,
	})
	if err != nil {
		return "", fmt.Errorf("check trial card reuse: %w", err)
	}
	if used {
		return CardRejectionUsed, nil
	}
	return "", nil
}
