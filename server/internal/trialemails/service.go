// Package trialemails synchronizes enterprise trial lifecycle state to Loops.
package trialemails

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

const trialStartedEventName = "trial_started"

// Service synchronizes active trial administrators with Loops workflows.
type Service struct {
	db        *pgxpool.Pool
	workflows loops.WorkflowClient
	logger    *slog.Logger
	siteURL   string
}

var _ Notifier = (*Service)(nil)

// NewService constructs a trial lifecycle email synchronizer.
func NewService(db *pgxpool.Pool, workflows loops.WorkflowClient, logger *slog.Logger, siteURL string) *Service {
	return &Service{
		db:        db,
		workflows: workflows,
		logger:    logger,
		siteURL:   strings.TrimRight(siteURL, "/"),
	}
}

// TrialStarted enters every active organization administrator into the trial workflow.
func (s *Service) TrialStarted(ctx context.Context, organizationID string) error {
	trial, err := trialsrepo.New(s.db).GetActiveTrial(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get active trial: %w", err)
	}

	queries := orgrepo.New(s.db)
	organization, err := queries.GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("get organization metadata: %w", err)
	}
	admins, err := accessrepo.New(s.db).ListActiveOrganizationAdmins(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("list active organization administrators: %w", err)
	}

	properties := map[string]any{
		"organizationName": organization.Name,
		"dashboardUrl":     s.siteURL + "/" + organization.Slug,
		"trialEndsAt":      trial.EndsAt.Time.UTC().Format(time.RFC3339),
		"trialActive":      true,
	}
	return s.syncTrialAdmins(ctx, organizationID, admins, properties, trial.CreatedAt.Time)
}

// AdminAdded enters a newly added administrator into an active trial workflow.
func (s *Service) AdminAdded(ctx context.Context, organizationID, userID string) error {
	trial, err := trialsrepo.New(s.db).GetActiveTrial(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get active trial: %w", err)
	}

	queries := orgrepo.New(s.db)
	organization, err := queries.GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("get organization metadata: %w", err)
	}
	admin, err := accessrepo.New(s.db).GetActiveOrganizationAdmin(ctx, accessrepo.GetActiveOrganizationAdminParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("administrator not found in active organization administrators")
	}
	if err != nil {
		return fmt.Errorf("get active organization administrator: %w", err)
	}

	return s.syncTrialAdmins(ctx, organizationID, []accessrepo.ListActiveOrganizationAdminsRow{{
		ID:          admin.ID,
		DisplayName: admin.DisplayName,
		Email:       admin.Email,
	}}, map[string]any{
		"organizationName": organization.Name,
		"dashboardUrl":     s.siteURL + "/" + organization.Slug,
		"trialEndsAt":      trial.EndsAt.Time.UTC().Format(time.RFC3339),
		"trialActive":      true,
	}, trial.CreatedAt.Time)
}

// TrialInactive prevents active administrators from receiving pending trial reminders.
func (s *Service) TrialInactive(ctx context.Context, organizationID string) error {
	admins, err := accessrepo.New(s.db).ListActiveOrganizationAdmins(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("list active organization administrators: %w", err)
	}

	var syncErrors []error
	for _, admin := range admins {
		if _, err := s.updateContact(ctx, admin, map[string]any{"trialActive": false}); err != nil {
			s.logger.ErrorContext(ctx, "sync inactive trial contact", attr.SlogOrganizationID(organizationID), attr.SlogUserID(admin.ID), attr.SlogError(err))
			syncErrors = append(syncErrors, err)
		}
	}
	return errors.Join(syncErrors...)
}

func (s *Service) syncTrialAdmins(ctx context.Context, organizationID string, admins []accessrepo.ListActiveOrganizationAdminsRow, properties map[string]any, trialCreatedAt time.Time) error {
	var syncErrors []error
	for _, admin := range admins {
		contact, err := s.updateContact(ctx, admin, properties)
		if err != nil {
			s.logger.ErrorContext(ctx, "sync trial contact", attr.SlogOrganizationID(organizationID), attr.SlogUserID(admin.ID), attr.SlogError(err))
			syncErrors = append(syncErrors, err)
			continue
		}
		if contact != nil && !contact.Subscribed {
			continue
		}
		if err := s.workflows.SendEvent(ctx, loops.SendEventInput{
			Email:           admin.Email,
			UserID:          admin.ID,
			EventName:       trialStartedEventName,
			EventProperties: properties,
			IdempotencyKey:  trialStartedIdempotencyKey(organizationID, admin.ID, trialCreatedAt),
		}); err != nil {
			s.logger.ErrorContext(ctx, "send trial workflow event", attr.SlogOrganizationID(organizationID), attr.SlogUserID(admin.ID), attr.SlogError(err))
			syncErrors = append(syncErrors, err)
		}
	}
	return errors.Join(syncErrors...)
}

func (s *Service) updateContact(ctx context.Context, admin accessrepo.ListActiveOrganizationAdminsRow, properties map[string]any) (*loops.Contact, error) {
	contact, err := s.findContact(ctx, admin)
	if err != nil {
		return nil, fmt.Errorf("find contact: %w", err)
	}

	var firstName *string
	if fields := strings.Fields(admin.DisplayName); len(fields) > 0 {
		firstName = &fields[0]
	}
	if err := s.workflows.UpdateContact(ctx, loops.UpdateContactInput{
		Email:            admin.Email,
		FirstName:        firstName,
		UserID:           admin.ID,
		CustomProperties: properties,
	}); err != nil {
		return nil, fmt.Errorf("update contact: %w", err)
	}
	return contact, nil
}

func (s *Service) findContact(ctx context.Context, admin accessrepo.ListActiveOrganizationAdminsRow) (*loops.Contact, error) {
	if admin.ID != "" {
		contact, err := s.workflows.FindContact(ctx, loops.FindContactInput{Email: "", UserID: admin.ID})
		if err != nil {
			return nil, fmt.Errorf("find contact by user ID: %w", err)
		}
		if contact != nil {
			return contact, nil
		}
	}
	contact, err := s.workflows.FindContact(ctx, loops.FindContactInput{Email: admin.Email, UserID: ""})
	if err != nil {
		return nil, fmt.Errorf("find contact by email: %w", err)
	}
	return contact, nil
}

func trialStartedIdempotencyKey(organizationID, userID string, trialCreatedAt time.Time) string {
	input := strings.Join([]string{
		trialStartedEventName,
		organizationID,
		userID,
		trialCreatedAt.UTC().Format(time.RFC3339Nano),
	}, ":")
	checksum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%s:%x", trialStartedEventName, checksum)
}
