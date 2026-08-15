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

	"github.com/speakeasy-api/gram/server/internal/attr"
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
	logger   *slog.Logger
	db       *pgxpool.Pool
	sender   Sender
	features feature.Provider
	siteURL  *url.URL
}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sender Sender, features feature.Provider, siteURL *url.URL) *Service {
	return &Service{
		logger:   logger.With(attr.SlogComponent("billing-notifications")),
		db:       db,
		sender:   sender,
		features: features,
		siteURL:  siteURL,
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
	recipients, err := ResolveRecipients(ctx, s.db, input.OrganizationID, "payg", configuredEmail)
	if err != nil {
		return SendTrialEndingSoonResult{}, err
	}

	template := email.TrialEndingSoon{
		OrganizationName: organization.Name,
		TrialEndDate:     state.TrialEndsAt.UTC().Format("January 2, 2006"),
		ActionURL:        s.siteURL.JoinPath(organization.Slug, "billing").String(),
	}
	var sendErrors []error
	for _, recipient := range recipients {
		key := RecipientIdempotencyKey(recipient, "trial-ending-soon", input.OrganizationID, state.TrialCreatedAt.UTC().Format(time.RFC3339Nano), state.TrialEndsAt.UTC().Format(time.RFC3339Nano))
		if err := s.sender.SendIdempotent(ctx, recipient, key, template); err != nil {
			s.logger.ErrorContext(ctx, "send trial ending soon email", attr.SlogOrganizationID(input.OrganizationID), attr.SlogError(err))
			sendErrors = append(sendErrors, err)
		}
	}
	return SendTrialEndingSoonResult{}, errors.Join(sendErrors...)
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
	if organization.GramAccountType != "free" || organization.Whitelisted {
		return nil
	}

	metadata, err := usagerepo.New(s.db).GetBillingMetadata(ctx, input.OrganizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get billing metadata: %w", err)
	}
	if err == nil && metadata.StripeSubscriptionID.Valid {
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

	configuredEmail, err := s.configuredEmail(ctx, input.OrganizationID)
	if err != nil {
		return err
	}
	recipients, err := ResolveRecipients(ctx, s.db, input.OrganizationID, "payg", configuredEmail)
	if err != nil {
		return err
	}
	template := email.AccessPaused{
		OrganizationName: organization.Name,
		ActionURL:        s.siteURL.JoinPath(organization.Slug).String(),
	}
	var sendErrors []error
	for _, recipient := range recipients {
		key := RecipientIdempotencyKey(recipient, "access-paused", input.EventID)
		if err := s.sender.SendIdempotent(ctx, recipient, key, template); err != nil {
			s.logger.ErrorContext(ctx, "send access paused email", attr.SlogOrganizationID(input.OrganizationID), attr.SlogOutboxPublicID(input.EventID), attr.SlogError(err))
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
