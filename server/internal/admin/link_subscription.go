package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// linkStripeSubscription commits billing identity and any new access together.
// Existing PAYG organizations retain their access policy: this is also the
// historical missing-subscription repair operation, not a whitelist override.
func (s *Service) linkStripeSubscription(ctx context.Context, payload *gen.SetStripeSubscriptionPayload) (*gen.AdminOrganization, error) {
	organization, state, err := s.verifiedStripeSubscription(ctx, payload.OrganizationID, payload.StripeSubscriptionID)
	if err != nil {
		return nil, err
	}
	id := organization.ID
	features := append([]productfeatures.Feature{productfeatures.FeaturePlatformMCP, productfeatures.FeatureSkills}, productfeatures.EnterpriseAccessBundle...)
	features = append(features, productfeatures.TrialRuntimeFeatures...)
	slices.Sort(features)
	features = slices.Compact(features)
	conn, release, err := s.productFeatures.AcquireFeatureCacheLocks(ctx, id, features)
	if err != nil {
		return nil, fmt.Errorf("lock subscription activation features: %w", err)
	}
	defer release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin subscription activation transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	q, bq := repo.New(tx), usagerepo.New(tx)
	if err := q.AdminAcquireStripeSubscriptionLock(ctx, state.ID); err != nil {
		return nil, fmt.Errorf("lock Stripe subscription assignment: %w", err)
	}
	_, err = trialsrepo.New(tx).LockTrialLifecycle(ctx, id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lock subscription activation trial lifecycle: %w", err)
	}
	hasTrial := err == nil
	var trial trialsrepo.Trial
	if hasTrial {
		trial, err = trialsrepo.New(tx).GetTrial(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("read locked subscription activation trial: %w", err)
		}
	}
	for _, kt := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyBillingTransactionLock(ctx, tx, id, kt); err != nil {
			return nil, fmt.Errorf("lock provider key billing for subscription activation: %w", err)
		}
	}
	for _, kt := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyProvisioningTransactionLock(ctx, tx, id, kt); err != nil {
			return nil, fmt.Errorf("lock provider key provisioning for subscription activation: %w", err)
		}
	}
	if err := bq.LockBillingMetadataOrganization(ctx, id); err != nil {
		return nil, fmt.Errorf("lock subscription billing metadata organization: %w", err)
	}
	if _, err := q.LockOrganizationMetadata(ctx, id); err != nil {
		return nil, fmt.Errorf("lock subscription activation organization: %w", err)
	}
	if _, err := bq.LockBillingMetadata(ctx, id); err != nil {
		return nil, fmt.Errorf("lock subscription billing metadata: %w", err)
	}
	organization, err = q.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: id, AllowSlug: false})
	if err != nil {
		return nil, fmt.Errorf("read locked subscription activation organization: %w", err)
	}
	// SELECT FOR UPDATE cannot lock an absent trial. A concurrent first trial
	// may have committed while we waited for key locks. Never acquire its row
	// lock here (keys-before-trial would invert lifecycle ordering); retry from
	// the beginning so conversion uses the new trial under its canonical lock.
	if !hasTrial && organization.TrialTier.Valid {
		return nil, oops.E(oops.CodeConflict, nil, "trial lifecycle changed during subscription activation; retry the operation")
	}
	if err := requireStripeSubscriptionAssignment(organization, state.ID); err != nil {
		return nil, err
	}
	state, err = s.liveStripeSubscriptionForOrganization(ctx, organization, state.ID, stripeclient.RequireAdoptionEligibility)
	if err != nil {
		return nil, err
	}
	if err := requireStripeActivationStatus(state); err != nil {
		return nil, err
	}
	owners, err := bq.ListStripeSubscriptionOwners(ctx, conv.ToPGText(state.ID))
	if err != nil {
		return nil, fmt.Errorf("check Stripe subscription ownership: %w", err)
	}
	if len(owners) > 1 || (len(owners) == 1 && owners[0] != id) {
		return nil, oops.E(oops.CodeConflict, nil, "Stripe subscription already belongs to another organization")
	}
	// A same-identity retry never rewrites retained anchors or repeats audits.
	if !organization.StripeSubscriptionID.Valid || organization.StripeSubscriptionID.String == "" {
		if organization.AccountType != string(billing.TierPayg) {
			if err := s.activateManuallyLinkedPaygTx(ctx, tx, organization, trial, hasTrial); err != nil {
				return nil, err
			}
		}
		_, err = q.AdminSetStripeSubscription(ctx, repo.AdminSetStripeSubscriptionParams{
			StripeSubscriptionID: state.ID, StripeBillingCycleAnchor: conv.ToPGTimestamptz(state.BillingCycleAnchor.UTC()),
			BillingCycleAnchorDay: conv.SafeInt32(state.BillingCycleAnchor.UTC().Day()), OrganizationID: id,
			StripeCustomerID: organization.StripeCustomerID.String,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, nil, "organization cannot accept this Stripe subscription")
		}
		if err != nil {
			return nil, fmt.Errorf("persist Stripe subscription and billing anchor: %w", err)
		}
	}
	updated, err := q.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: id, AllowSlug: false})
	if err != nil {
		return nil, fmt.Errorf("read activated subscription organization: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit subscription activation: %w", err)
	}
	// Durable account-type/conversion outbox events reconcile provider keys.
	// A retry can finish cache/notification cleanup without repeating activation.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, feature := range features {
		if err := s.productFeatures.UpdateFeatureCacheUnderLock(cleanupCtx, conn, id, feature); err != nil {
			return nil, fmt.Errorf("subscription committed; refresh feature cache: %w", err)
		}
	}
	if s.trial != nil && (!hasTrial || updated.TrialConvertedAt.Valid) {
		if err := s.trial.TrialInactive(cleanupCtx, id); err != nil {
			return nil, fmt.Errorf("subscription committed; stop trial notifications: %w", err)
		}
	}
	return s.adminOrganizationFromGetRow(updated), nil
}

func (s *Service) activateManuallyLinkedPaygTx(ctx context.Context, tx pgx.Tx, org repo.AdminGetOrganizationRow, trial trialsrepo.Trial, hasTrial bool) error {
	actor, display := enterpriseTrialConversionAuditActor(ctx)
	local := new(openrouter.OpenRouter)
	if !hasTrial {
		if _, err := productfeatures.SeedEnterpriseAccessEntitlementsTx(ctx, tx, org.ID); err != nil {
			return fmt.Errorf("seed PAYG access entitlements: %w", err)
		}
	} else {
		converted, err := usage.ConvertEnterpriseTrialToPaygTx(ctx, tx, org.ID, trial, local, s.audit, time.Now(), usage.BillingActor{Principal: actor, DisplayName: display}, "stripe_manual_link")
		if err != nil {
			return fmt.Errorf("convert enterprise trial for manual subscription activation: %w", err)
		}
		if !trial.ConvertedAt.Valid && !converted {
			if _, err := trialsrepo.New(tx).MarkTrialConverted(ctx, org.ID); err != nil {
				return fmt.Errorf("mark manually activated trial converted: %w", err)
			}
		}
		if trial.DemotedAt.Valid {
			if err := productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, org.ID, true); err != nil {
				return fmt.Errorf("restore demoted trial runtime features: %w", err)
			}
			for _, kt := range openrouter.AllKeyTypes {
				if _, err := local.PrepareAdminKeyPolicyWithDB(ctx, tx, org.ID, kt, openrouter.AdminKeyPolicy{MonthlyCredits: nil, RemoveCauses: []string{string(openrouter.DisableCauseTrialDemotion)}}); err != nil {
					return fmt.Errorf("remove trial demotion from provider key policy: %w", err)
				}
			}
		}
	}
	limit, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
	if !ok || limit <= 0 {
		return errors.New("PAYG provider credit policy is unavailable")
	}
	credits := int64(limit)
	if _, err := local.PrepareAdminKeyPolicyWithDB(ctx, tx, org.ID, openrouter.KeyTypeChat, openrouter.AdminKeyPolicy{MonthlyCredits: &credits, RemoveCauses: []string{string(openrouter.DisableCauseBillingInactive)}}); err != nil {
		return fmt.Errorf("prepare PAYG chat key policy: %w", err)
	}
	if err := usagerepo.New(tx).ActivatePaygOrganization(ctx, org.ID); err != nil {
		return fmt.Errorf("activate PAYG organization: %w", err)
	}
	if s.audit == nil {
		return errors.New("audit logger is unavailable")
	}
	if err := s.audit.LogOrganizationPaygActivated(ctx, tx, audit.LogOrganizationPaygActivatedEvent{
		OrganizationID: org.ID, Actor: actor, ActorDisplayName: display, ActorSlug: nil, OrganizationName: org.Name, OrganizationSlug: org.Slug,
		OrganizationSnapshotBefore: &audit.OrganizationPaygActivationSnapshot{AccountType: org.AccountType, Whitelisted: org.Whitelisted},
		OrganizationSnapshotAfter:  &audit.OrganizationPaygActivationSnapshot{AccountType: "payg", Whitelisted: true},
	}); err != nil {
		return fmt.Errorf("audit manual PAYG activation: %w", err)
	}
	if err := s.audit.LogOrganizationAccountTypeChanged(ctx, tx, audit.LogOrganizationAccountTypeChangedEvent{OrganizationID: org.ID, Actor: actor, ActorDisplayName: display, BeforeAccountType: org.AccountType, AccountType: "payg"}); err != nil {
		return fmt.Errorf("audit subscription activation account type change: %w", err)
	}
	return nil
}
