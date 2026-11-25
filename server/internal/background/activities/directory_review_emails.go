package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directoryreview/repo"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/orghost"
)

const (
	// DirectoryReviewEmailQuietPeriod is how long an organization's newest
	// unannounced item waits before the announcement, so a burst of directory
	// changes becomes one email.
	DirectoryReviewEmailQuietPeriod = 15 * time.Minute
	// DirectoryReviewReminderInterval is the gap between reminders while items
	// stay pending.
	DirectoryReviewReminderInterval = 7 * 24 * time.Hour
	// DirectoryReviewMaxReminders caps reminders after each announcement.
	DirectoryReviewMaxReminders = 4
)

// SendDirectoryReviewEmailsArgs carries the run time, the workflow's
// workflow.Now, so the decisions and idempotency keys are stable across
// activity retries.
type SendDirectoryReviewEmailsArgs struct {
	Now time.Time
}

// DirectoryReviewEmails emails organization admins about directory changes
// waiting for a decision. Each run reads the pending items from the review
// table, so a run that fails before or during sending is repeated by the next
// one rather than lost. Sending never changes an item's decision state.
type DirectoryReviewEmails struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	emails   *email.Service
	orgHosts *orghost.Resolver
}

func NewDirectoryReviewEmails(logger *slog.Logger, db *pgxpool.Pool, emails *email.Service, orgHosts *orghost.Resolver) *DirectoryReviewEmails {
	return &DirectoryReviewEmails{
		logger:   logger.With(attr.SlogComponent("directory_review_emails")),
		db:       db,
		emails:   emails,
		orgHosts: orgHosts,
	}
}

type directoryReviewEmailKind string

const (
	directoryReviewEmailNone         directoryReviewEmailKind = ""
	directoryReviewEmailAnnouncement directoryReviewEmailKind = "announcement"
	directoryReviewEmailReminder     directoryReviewEmailKind = "reminder"
)

// decideDirectoryReviewEmail picks what, if anything, an organization is due.
// New items are announced once the newest has waited out the quiet period;
// otherwise a reminder is due a week after the last email, up to the cap.
func decideDirectoryReviewEmail(candidate repo.ListDirectoryReviewEmailCandidatesRow, now time.Time) directoryReviewEmailKind {
	if candidate.UnannouncedCount > 0 {
		if candidate.NewestUnannouncedAt.Valid && !now.Before(candidate.NewestUnannouncedAt.Time.Add(DirectoryReviewEmailQuietPeriod)) {
			return directoryReviewEmailAnnouncement
		}
		return directoryReviewEmailNone
	}
	if candidate.RemindersSent >= DirectoryReviewMaxReminders || !candidate.LastEmailedAt.Valid {
		return directoryReviewEmailNone
	}
	if now.Before(candidate.LastEmailedAt.Time.Add(DirectoryReviewReminderInterval)) {
		return directoryReviewEmailNone
	}
	return directoryReviewEmailReminder
}

// Send emails every organization that is due an announcement or reminder.
// Each organization is handled on its own, so one failure doesn't hold back
// the others; their errors are returned joined for a retry.
func (d *DirectoryReviewEmails) Send(ctx context.Context, args SendDirectoryReviewEmailsArgs) error {
	candidates, err := repo.New(d.db).ListDirectoryReviewEmailCandidates(ctx)
	if err != nil {
		return fmt.Errorf("list directory review email candidates: %w", err)
	}

	var errs []error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		kind := decideDirectoryReviewEmail(candidate, args.Now)
		if kind == directoryReviewEmailNone {
			continue
		}
		if err := d.sendOne(ctx, candidate, kind, args.Now); err != nil {
			errs = append(errs, fmt.Errorf("organization %s: %w", candidate.OrganizationID, err))
		}
	}
	return errors.Join(errs...)
}

func (d *DirectoryReviewEmails) sendOne(ctx context.Context, candidate repo.ListDirectoryReviewEmailCandidatesRow, kind directoryReviewEmailKind, now time.Time) error {
	organizationID := candidate.OrganizationID
	total := candidate.NewGroupCount + candidate.NewAttributeValueCount + candidate.SourceMissingCount

	reviewURL := ""
	if siteURL := d.orgHosts.SiteURL(candidate.DefaultHost); siteURL != nil {
		reviewURL = siteURL.JoinPath(candidate.OrganizationSlug, "identity").String()
	}
	tmpl := email.DirectoryReviewPending{
		OrganizationName:       conv.Default(candidate.OrganizationName, "your organization"),
		Summary:                directoryReviewEmailSummary(total),
		NewGroupCount:          strconv.FormatInt(candidate.NewGroupCount, 10),
		NewAttributeValueCount: strconv.FormatInt(candidate.NewAttributeValueCount, 10),
		SourceMissingCount:     strconv.FormatInt(candidate.SourceMissingCount, 10),
		ReviewURL:              reviewURL,
		IsReminder:             strconv.FormatBool(kind == directoryReviewEmailReminder),
	}

	// The key names the send window: an announcement by the newest item it
	// covers and how many, a reminder by its number and the email before it.
	// A retry of the same window reuses the key, so Loops sends it once.
	window := []string{"directory-review-pending", organizationID, string(kind)}
	switch kind {
	case directoryReviewEmailAnnouncement:
		window = append(window, strconv.FormatInt(candidate.NewestUnannouncedAt.Time.UnixMicro(), 10), strconv.FormatInt(candidate.UnannouncedCount, 10))
	case directoryReviewEmailReminder:
		window = append(window, strconv.Itoa(int(candidate.RemindersSent)+1), strconv.FormatInt(candidate.LastEmailedAt.Time.UnixMicro(), 10))
	case directoryReviewEmailNone:
		return nil
	}

	// Recipients are whoever is an admin now. Admins that resolved are
	// emailed even when others failed to; the failure is then returned
	// without recording the send, so a retry reaches the rest and the
	// idempotency key keeps the first ones from a second copy. An
	// organization with no admins is logged and recorded as sent, so it isn't
	// retried every run.
	recipients, resolveErr := authz.ResolveOrganizationAdminEmails(ctx, d.db, organizationID)
	if len(recipients) == 0 && resolveErr == nil {
		d.logger.WarnContext(ctx, "no organization admins to email about directory review", attr.SlogOrganizationID(organizationID))
	}
	var sendErrs []error
	if resolveErr != nil {
		sendErrs = append(sendErrs, fmt.Errorf("resolve directory review email recipients: %w", resolveErr))
	}
	for _, recipient := range recipients {
		key := recipientEmailIdempotencyKey(recipient, window...)
		if err := d.emails.SendIdempotent(ctx, recipient, key, tmpl); err != nil {
			sendErrs = append(sendErrs, fmt.Errorf("send directory review email: %w", err))
		}
	}
	if err := errors.Join(sendErrs...); err != nil {
		return err
	}

	if err := d.recordSend(ctx, candidate, kind, now); err != nil {
		return err
	}

	d.logger.InfoContext(ctx, "emailed organization admins about directory review",
		attr.SlogOrganizationID(organizationID),
		attr.SlogEvent(string(kind)),
	)
	return nil
}

// recordSend records an email in one transaction, so a failure can't leave
// items marked announced without the announcement that restarts reminders.
func (d *DirectoryReviewEmails) recordSend(ctx context.Context, candidate repo.ListDirectoryReviewEmailCandidatesRow, kind directoryReviewEmailKind, now time.Time) error {
	organizationID := candidate.OrganizationID
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin directory review email record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := repo.New(tx)
	emailedAt := conv.ToPGTimestamptz(now)
	switch kind {
	case directoryReviewEmailAnnouncement:
		if err := q.MarkDirectoryReviewItemsAnnounced(ctx, repo.MarkDirectoryReviewItemsAnnouncedParams{
			NotifiedAt: emailedAt, OrganizationID: organizationID, Ids: candidate.UnannouncedIds,
		}); err != nil {
			return fmt.Errorf("mark directory review items announced: %w", err)
		}
		if err := q.RecordDirectoryReviewAnnouncement(ctx, repo.RecordDirectoryReviewAnnouncementParams{
			EmailedAt: emailedAt, OrganizationID: organizationID,
		}); err != nil {
			return fmt.Errorf("record directory review announcement: %w", err)
		}
	case directoryReviewEmailReminder:
		if _, err := q.RecordDirectoryReviewReminder(ctx, repo.RecordDirectoryReviewReminderParams{
			EmailedAt: emailedAt, OrganizationID: organizationID, RemindersSent: candidate.RemindersSent,
		}); err != nil {
			return fmt.Errorf("record directory review reminder: %w", err)
		}
	case directoryReviewEmailNone:
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit directory review email record: %w", err)
	}
	return nil
}

func directoryReviewEmailSummary(total int64) string {
	if total == 1 {
		return "1 directory change needs a decision"
	}
	return strconv.FormatInt(total, 10) + " directory changes need a decision"
}
