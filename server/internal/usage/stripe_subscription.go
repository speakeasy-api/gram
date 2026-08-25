package usage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type stripeBillingAction int

const (
	stripeBillingActionCreatePortal stripeBillingAction = iota
	stripeBillingActionCancelSubscription
	stripeBillingActionResumeSubscription
)

type StripeSubscription struct {
	Status             string
	CurrentPeriodStart string
	CurrentPeriodEnd   string
	TrialStart         *string
	TrialEnd           *string
	CancelAtPeriodEnd  bool
	CancelAt           *string
	CanceledAt         *string
	PaymentFailed      bool
}

type BillingActor struct {
	Principal   urn.Principal
	DisplayName *string
}

func (s *Service) GetStripeSubscription(ctx context.Context, _ *gen.GetStripeSubscriptionPayload) (*gen.StripeSubscription, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	result, err := s.GetStripeSubscriptionForOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	return stripeSubscriptionGenView(result), nil
}

func (s *Service) CreateStripePortalSession(ctx context.Context, _ *gen.CreateStripePortalSessionPayload) (string, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return "", err
	}
	if s.siteURL == nil {
		return "", oops.E(oops.CodeUnavailable, nil, "self-serve billing is temporarily unavailable").LogWarn(ctx, s.logger)
	}

	metadata, state, err := s.getStripeBillingState(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return "", err
	}
	portal, err := s.stripeClient.CreatePortalSession(ctx, stripeclient.CreatePortalSessionInput{
		CustomerID: state.CustomerID,
		ReturnURL:  s.siteURL.JoinPath(authCtx.OrganizationSlug, "billing").String(),
	})
	if err != nil {
		return "", oops.E(oops.CodeUnavailable, err, "failed to create Stripe billing portal session").LogWarn(ctx, s.logger)
	}
	if portal == nil || portal.CustomerID != state.CustomerID || portal.URL == "" {
		return "", oops.E(oops.CodeUnavailable, nil, "Stripe billing portal state does not match billing metadata").LogWarn(ctx, s.logger)
	}
	if err := s.recordStripeBillingAction(ctx, authCtx.ActiveOrganizationID, BillingActor{
		Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: authCtx.Email,
	}, metadata, stripeBillingActionCreatePortal); err != nil {
		return "", err
	}
	return portal.URL, nil
}

func (s *Service) CancelStripeSubscription(ctx context.Context, _ *gen.CancelStripeSubscriptionPayload) (*gen.StripeSubscription, error) {
	return s.setStripeSubscriptionCancelAtPeriodEnd(ctx, true)
}

func (s *Service) ResumeStripeSubscription(ctx context.Context, _ *gen.ResumeStripeSubscriptionPayload) (*gen.StripeSubscription, error) {
	return s.setStripeSubscriptionCancelAtPeriodEnd(ctx, false)
}

// GetStripeSubscriptionForOrganization reads live Stripe state for an already
// authorized, canonical organization ID.
func (s *Service) GetStripeSubscriptionForOrganization(ctx context.Context, organizationID string) (*StripeSubscription, error) {
	if organizationID == "" {
		return nil, oops.C(oops.CodeNotFound)
	}
	_, state, err := s.getStripeBillingState(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return buildStripeSubscriptionView(state), nil
}

func (s *Service) setStripeSubscriptionCancelAtPeriodEnd(ctx context.Context, cancelAtPeriodEnd bool) (*gen.StripeSubscription, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	result, err := s.SetStripeSubscriptionCancelAtPeriodEndForOrganization(ctx, authCtx.ActiveOrganizationID, BillingActor{
		Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: authCtx.Email,
	}, cancelAtPeriodEnd)
	if err != nil {
		return nil, err
	}
	return stripeSubscriptionGenView(result), nil
}

// SetStripeSubscriptionCancelAtPeriodEndForOrganization changes live Stripe
// state for an already authorized, canonical organization ID and records actor.
func (s *Service) SetStripeSubscriptionCancelAtPeriodEndForOrganization(ctx context.Context, organizationID string, actor BillingActor, cancelAtPeriodEnd bool) (*StripeSubscription, error) {
	if organizationID == "" {
		return nil, oops.C(oops.CodeNotFound)
	}
	metadata, state, err := s.getStripeBillingState(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if !canManageStripeSubscriptionLifecycle(state.Status) {
		return nil, oops.E(oops.CodeConflict, nil, "the Stripe subscription is not active and cannot be changed")
	}

	updated, err := s.stripeClient.SetSubscriptionCancelAtPeriodEnd(ctx, stripeclient.SetSubscriptionCancelAtPeriodEndInput{
		SubscriptionID:    state.ID,
		CancelAtPeriodEnd: cancelAtPeriodEnd,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "failed to update Stripe subscription").LogWarn(ctx, s.logger)
	}
	if err := validateStripeBillingIdentity(metadata, updated); err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "updated Stripe subscription does not match billing metadata").LogWarn(ctx, s.logger)
	}
	if updated.CancelAtPeriodEnd != cancelAtPeriodEnd {
		return nil, oops.E(oops.CodeUnavailable, nil, "Stripe did not apply the subscription lifecycle change").LogWarn(ctx, s.logger)
	}

	action := stripeBillingActionResumeSubscription
	if cancelAtPeriodEnd {
		action = stripeBillingActionCancelSubscription
	}
	if err := s.recordStripeBillingAction(ctx, organizationID, actor, metadata, action); err != nil {
		return nil, err
	}
	return buildStripeSubscriptionView(updated), nil
}

func canManageStripeSubscriptionLifecycle(status string) bool {
	switch status {
	case "trialing", "active", "past_due":
		return true
	default:
		return false
	}
}

func (s *Service) getStripeBillingState(ctx context.Context, organizationID string) (repo.BillingMetadatum, *stripeclient.SubscriptionState, error) {
	metadata, err := repo.New(s.db).GetBillingMetadata(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.BillingMetadatum{}, nil, oops.E(oops.CodeNotFound, err, "the organization does not have a Stripe subscription").LogWarn(ctx, s.logger)
	case err != nil:
		return repo.BillingMetadatum{}, nil, oops.E(oops.CodeUnexpected, err, "failed to get billing metadata").LogError(ctx, s.logger)
	case !metadata.StripeCustomerID.Valid || !metadata.StripeSubscriptionID.Valid:
		return repo.BillingMetadatum{}, nil, oops.E(oops.CodeNotFound, nil, "the organization does not have a Stripe subscription").LogWarn(ctx, s.logger)
	}
	if s.stripeClient == nil {
		return repo.BillingMetadatum{}, nil, oops.E(oops.CodeUnavailable, nil, "self-serve billing is temporarily unavailable").LogWarn(ctx, s.logger)
	}

	state, err := s.stripeClient.GetSubscription(ctx, metadata.StripeSubscriptionID.String)
	if err != nil {
		return repo.BillingMetadatum{}, nil, oops.E(oops.CodeUnavailable, err, "failed to get Stripe subscription").LogWarn(ctx, s.logger)
	}
	if err := validateStripeBillingIdentity(metadata, state); err != nil {
		return repo.BillingMetadatum{}, nil, oops.E(oops.CodeUnavailable, err, "Stripe subscription does not match billing metadata").LogWarn(ctx, s.logger)
	}
	return metadata, state, nil
}

func validateStripeBillingIdentity(metadata repo.BillingMetadatum, state *stripeclient.SubscriptionState) error {
	if state == nil {
		return errors.New("stripe returned an empty subscription")
	}
	if !metadata.StripeCustomerID.Valid || state.CustomerID != metadata.StripeCustomerID.String {
		return errors.New("stripe customer identity mismatch")
	}
	if !metadata.StripeSubscriptionID.Valid || state.ID != metadata.StripeSubscriptionID.String {
		return errors.New("stripe subscription identity mismatch")
	}
	return nil
}

func (s *Service) recordStripeBillingAction(
	ctx context.Context,
	organizationID string,
	actor BillingActor,
	expected repo.BillingMetadatum,
	action stripeBillingAction,
) error {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record Stripe billing action").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	current, err := repo.New(dbtx).LockBillingMetadata(ctx, organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeConflict, err, "billing state changed while Stripe was being updated").LogWarn(ctx, s.logger)
		}
		return oops.E(oops.CodeUnexpected, err, "failed to lock billing metadata").LogError(ctx, s.logger)
	}
	if current.ID != expected.ID ||
		current.StripeCustomerID != expected.StripeCustomerID ||
		current.StripeSubscriptionID != expected.StripeSubscriptionID {
		return oops.E(oops.CodeConflict, nil, "billing state changed while Stripe was being updated").LogWarn(ctx, s.logger)
	}

	subject := urn.NewBillingMetadata(current.ID)
	switch action {
	case stripeBillingActionCreatePortal:
		err = s.auditLogger.LogBillingMetadataCreateStripePortal(ctx, dbtx, audit.LogBillingMetadataCreateStripePortalEvent{
			OrganizationID:     organizationID,
			Actor:              actor.Principal,
			ActorDisplayName:   actor.DisplayName,
			ActorSlug:          nil,
			BillingMetadataURN: subject,
		})
	case stripeBillingActionCancelSubscription:
		err = s.auditLogger.LogBillingMetadataCancelStripeSubscription(ctx, dbtx, audit.LogBillingMetadataCancelStripeSubscriptionEvent{
			OrganizationID:     organizationID,
			Actor:              actor.Principal,
			ActorDisplayName:   actor.DisplayName,
			ActorSlug:          nil,
			BillingMetadataURN: subject,
		})
	case stripeBillingActionResumeSubscription:
		err = s.auditLogger.LogBillingMetadataResumeStripeSubscription(ctx, dbtx, audit.LogBillingMetadataResumeStripeSubscriptionEvent{
			OrganizationID:     organizationID,
			Actor:              actor.Principal,
			ActorDisplayName:   actor.DisplayName,
			ActorSlug:          nil,
			BillingMetadataURN: subject,
		})
	default:
		err = errors.New("unknown Stripe billing audit action")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record Stripe billing audit event").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record Stripe billing action").LogError(ctx, s.logger)
	}
	return nil
}

func buildStripeSubscriptionView(state *stripeclient.SubscriptionState) *StripeSubscription {
	return &StripeSubscription{
		Status:             state.Status,
		CurrentPeriodStart: state.CurrentPeriodStart.Format(time.RFC3339),
		CurrentPeriodEnd:   state.CurrentPeriodEnd.Format(time.RFC3339),
		TrialStart:         formatOptionalStripeTime(state.TrialStart),
		TrialEnd:           formatOptionalStripeTime(state.TrialEnd),
		CancelAtPeriodEnd:  state.CancelAtPeriodEnd,
		CancelAt:           formatOptionalStripeTime(state.CancelAt),
		CanceledAt:         formatOptionalStripeTime(state.CanceledAt),
		PaymentFailed:      state.PaymentFailed,
	}
}

func stripeSubscriptionGenView(value *StripeSubscription) *gen.StripeSubscription {
	return &gen.StripeSubscription{
		Status: value.Status, CurrentPeriodStart: value.CurrentPeriodStart, CurrentPeriodEnd: value.CurrentPeriodEnd,
		TrialStart: value.TrialStart, TrialEnd: value.TrialEnd, CancelAtPeriodEnd: value.CancelAtPeriodEnd,
		CancelAt: value.CancelAt, CanceledAt: value.CanceledAt, PaymentFailed: value.PaymentFailed,
	}
}

func formatOptionalStripeTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}
