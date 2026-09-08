package billingnotifications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const trialEndingSoonLeadTime = 72 * time.Hour

type Sender interface {
	SendIdempotent(context.Context, string, string, email.Template) error
}

type Service struct {
	logger            *slog.Logger
	db                *pgxpool.Pool
	sender            Sender
	features          feature.Provider
	siteURL           *url.URL
	resolveRecipients func(context.Context, accessrepo.DBTX, string, string, *string) ([]string, error)
}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sender Sender, features feature.Provider, siteURL *url.URL) *Service {
	return &Service{
		logger:            logger.With(attr.SlogComponent("billing-notifications")),
		db:                db,
		sender:            sender,
		features:          features,
		siteURL:           siteURL,
		resolveRecipients: ResolveRecipients,
	}
}

type TrialReminderState struct {
	Active         bool
	TrialCreatedAt time.Time
	TrialEndsAt    time.Time
	SendAt         time.Time
}

func (s *Service) ResolveTrialReminder(ctx context.Context, organizationID string) (TrialReminderState, error) {
	trial, err := trialsrepo.New(s.db).GetActiveTrial(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrialReminderState{Active: false, TrialCreatedAt: time.Time{}, TrialEndsAt: time.Time{}, SendAt: time.Time{}}, nil
	}
	if err != nil {
		return TrialReminderState{}, fmt.Errorf("get active trial: %w", err)
	}

	metadata, err := usagerepo.New(s.db).GetBillingMetadata(ctx, organizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return TrialReminderState{}, fmt.Errorf("get billing metadata: %w", err)
	}
	if err == nil && metadata.StripeSubscriptionID.Valid {
		return TrialReminderState{Active: false, TrialCreatedAt: time.Time{}, TrialEndsAt: time.Time{}, SendAt: time.Time{}}, nil
	}

	return TrialReminderState{
		Active:         true,
		TrialCreatedAt: trial.CreatedAt.Time,
		TrialEndsAt:    trial.EndsAt.Time,
		SendAt:         trial.EndsAt.Time.Add(-trialEndingSoonLeadTime),
	}, nil
}

type SendTrialEndingSoonInput struct {
	OrganizationID string
	TrialCreatedAt time.Time
	TrialEndsAt    time.Time
}

type SendTrialEndingSoonResult struct {
	Reschedule bool
}

func (s *Service) SendTrialEndingSoon(ctx context.Context, input SendTrialEndingSoonInput) (SendTrialEndingSoonResult, error) {
	state, err := s.ResolveTrialReminder(ctx, input.OrganizationID)
	if err != nil || !state.Active {
		return SendTrialEndingSoonResult{}, err
	}
	if !state.TrialCreatedAt.Equal(input.TrialCreatedAt) || !state.TrialEndsAt.Equal(input.TrialEndsAt) {
		return SendTrialEndingSoonResult{Reschedule: true}, nil
	}

	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, input.OrganizationID)
	if err != nil {
		return SendTrialEndingSoonResult{}, fmt.Errorf("get organization: %w", err)
	}
	configuredEmail, err := s.configuredEmail(ctx, input.OrganizationID)
	if err != nil {
		return SendTrialEndingSoonResult{}, err
	}
	recipients, resolutionErr := s.resolveRecipients(ctx, s.db, input.OrganizationID, string(billing.TierPayg), configuredEmail)

	template := email.TrialEndingSoon{
		OrganizationName: organization.Name,
		TrialEndDate:     state.TrialEndsAt.UTC().Format("January 2, 2006"),
		ActionURL:        s.siteURL.JoinPath(organization.Slug, "billing").String(),
	}
	return SendTrialEndingSoonResult{}, s.deliver(ctx, input.OrganizationID, "", recipients, resolutionErr, template,
		"trial-ending-soon", input.OrganizationID, state.TrialCreatedAt.UTC().Format(time.RFC3339Nano), state.TrialEndsAt.UTC().Format(time.RFC3339Nano))
}

type AccessPausedKind string

const (
	AccessPausedSubscriptionLoss AccessPausedKind = "subscription-loss"
	AccessPausedTrialDemotion    AccessPausedKind = "trial-demotion"
)

type SendAccessPausedInput struct {
	EventID        string
	OrganizationID string
	Kind           AccessPausedKind
}

func (s *Service) SendAccessPaused(ctx context.Context, input SendAccessPausedInput) error {
	switch input.Kind {
	case AccessPausedSubscriptionLoss, AccessPausedTrialDemotion:
	case "":
		return fmt.Errorf("access paused kind is required")
	default:
		return fmt.Errorf("unsupported access paused kind %q", input.Kind)
	}

	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, input.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get organization: %w", err)
	}
	if billing.Tier(organization.GramAccountType) != billing.TierBase || organization.Whitelisted {
		return nil
	}

	metadata, metadataErr := usagerepo.New(s.db).GetBillingMetadata(ctx, input.OrganizationID)
	if metadataErr != nil && !errors.Is(metadataErr, pgx.ErrNoRows) {
		return fmt.Errorf("get billing metadata: %w", metadataErr)
	}
	if metadataErr == nil && metadata.StripeSubscriptionID.Valid {
		return nil
	}

	if input.Kind == AccessPausedTrialDemotion {
		trial, trialErr := trialsrepo.New(s.db).GetTrial(ctx, input.OrganizationID)
		if errors.Is(trialErr, pgx.ErrNoRows) || (trialErr == nil && (!trial.DemotedAt.Valid || trial.ConvertedAt.Valid)) {
			return nil
		}
		if trialErr != nil {
			return fmt.Errorf("get demoted trial: %w", trialErr)
		}
		if s.features == nil {
			return nil
		}
		enabled, flagErr := s.features.IsFlagEnabled(ctx, feature.FlagPaygSelfServeBilling, input.OrganizationID, feature.OrgProjectGroups(organization.Slug, ""))
		if flagErr != nil || !enabled {
			if flagErr != nil {
				s.logger.WarnContext(ctx, "evaluate PAYG email rollout", attr.SlogOrganizationID(input.OrganizationID), attr.SlogError(flagErr))
			}
			return nil
		}
	}

	var configuredEmail *string
	if metadataErr == nil && metadata.AlertEmail.Valid {
		configuredEmail = &metadata.AlertEmail.String
	}
	recipients, resolutionErr := s.resolveRecipients(ctx, s.db, input.OrganizationID, string(billing.TierPayg), configuredEmail)
	template := email.AccessPaused{
		OrganizationName: organization.Name,
		ActionURL:        s.siteURL.JoinPath(organization.Slug).String(),
	}
	return s.deliver(ctx, input.OrganizationID, input.EventID, recipients, resolutionErr, template, "access-paused", input.EventID)
}

type SendPaygActivatedInput struct {
	EventID        string
	OrganizationID string
}

func (s *Service) SendPaygActivated(ctx context.Context, input SendPaygActivatedInput) error {
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, input.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get organization: %w", err)
	}
	if billing.Tier(organization.GramAccountType) != billing.TierPayg {
		return nil
	}

	metadata, err := usagerepo.New(s.db).GetBillingMetadata(ctx, input.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get billing metadata: %w", err)
	}
	if !metadata.StripeSubscriptionID.Valid {
		return nil
	}

	var configuredEmail *string
	if metadata.AlertEmail.Valid {
		configuredEmail = &metadata.AlertEmail.String
	}
	recipients, resolutionErr := s.resolveRecipients(ctx, s.db, input.OrganizationID, string(billing.TierPayg), configuredEmail)

	template := email.PaygActivated{
		OrganizationName:      organization.Name,
		TumPricePerMillionUsd: billing.TUMPricePerMillionUSD,
		ActionURL:             s.siteURL.JoinPath(organization.Slug, "billing").String(),
	}
	return s.deliver(ctx, input.OrganizationID, input.EventID, recipients, resolutionErr, template, "payg-activated", input.EventID)
}

// deliver sends one template to every resolved recipient. A recipient
// resolution failure never skips the addresses that did resolve: the send
// happens first and the failure joins the per-recipient errors.
func (s *Service) deliver(ctx context.Context, organizationID, eventID string, recipients []string, resolutionErr error, template email.Template, keyParts ...string) error {
	sendErrors := make([]error, 0, len(recipients)+1)
	if resolutionErr != nil {
		sendErrors = append(sendErrors, resolutionErr)
	}
	for _, recipient := range recipients {
		key := RecipientIdempotencyKey(recipient, keyParts...)
		if err := s.sender.SendIdempotent(ctx, recipient, key, template); err != nil {
			logAttrs := []any{
				attr.SlogOrganizationID(organizationID),
				attr.SlogEvent(string(template.Key())),
				attr.SlogError(err),
			}
			if eventID != "" {
				logAttrs = append(logAttrs, attr.SlogOutboxPublicID(eventID))
			}
			s.logger.ErrorContext(ctx, "send billing notification email", logAttrs...)
			sendErrors = append(sendErrors, err)
		}
	}
	return errors.Join(sendErrors...)
}

func (s *Service) configuredEmail(ctx context.Context, organizationID string) (*string, error) {
	metadata, err := usagerepo.New(s.db).GetBillingMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get billing metadata: %w", err)
	}
	if !metadata.AlertEmail.Valid {
		return nil, nil
	}
	return &metadata.AlertEmail.String, nil
}
