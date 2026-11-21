package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

const (
	// OrganizationEncryptionFinalizeConfirmation is the phrase an administrator
	// types to finalize. It names what finalize destroys.
	OrganizationEncryptionFinalizeConfirmation = "destroy platform-key copies"

	// organizationEncryptionMinimumDualWrite is how long a use case must soak
	// in dual_write before an administrator can finalize it: a day covers
	// daily traffic and several hourly backfills and verifications.
	organizationEncryptionMinimumDualWrite = orgencryption.DefaultMinimumDualWrite

	// organizationEncryptionDetailMaxLen bounds the error detail echoed to
	// the dashboard, which can carry a provider message.
	organizationEncryptionDetailMaxLen = 300
)

// OrganizationEncryptionRunStarter starts an on-demand run for one
// organization's use case: a backfill and verification in dual_write, or
// finishing the clear after finalize in org_kms. It joins a run already in
// progress.
type OrganizationEncryptionRunStarter interface {
	StartOrganizationEncryptionRun(ctx context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error
}

// OrganizationEncryption is what the admin API needs to report and change
// organization encryption transitions.
type OrganizationEncryption struct {
	// Transitions reads and changes transition state. It must have a
	// provisioner and an audit logger.
	Transitions *orgencryption.Transitions

	// UseCases lists every use case that moves from the platform key to
	// organization keys, with the copy stores of every value it holds.
	UseCases []orgencryption.UseCaseStores

	// Runs starts on-demand runs. Nil when this server has no Temporal
	// connection: runs are then unavailable, and finalize leaves clearing to
	// the hourly background run.
	Runs OrganizationEncryptionRunStarter
}

// SetOrganizationEncryption enables the organization encryption transition
// methods. Without it, as on an admin server without organization
// encryption, they report unavailable.
func (s *Service) SetOrganizationEncryption(orgEncryption OrganizationEncryption) {
	s.orgEncryption = &orgEncryption
}

func (s *Service) GetOrganizationEncryption(ctx context.Context, payload *gen.GetOrganizationEncryptionPayload) (*gen.AdminOrganizationEncryption, error) {
	if err := s.requireOrganizationEncryption(ctx); err != nil {
		return nil, err
	}

	organizationID, logger, err := s.resolveEncryptionOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	var config *gen.AdminOrganizationEncryptionConfig
	row, err := s.encryptionConfig(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading organization encryption").LogError(ctx, logger)
	}
	if row != nil {
		config = encryptionConfigView(*row)
	}

	useCases := make([]*gen.AdminOrganizationEncryptionUseCase, 0, len(s.orgEncryption.UseCases))
	for _, move := range s.orgEncryption.UseCases {
		view, err := s.encryptionUseCaseView(ctx, organizationID, move)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error loading organization encryption transitions").LogError(ctx, logger)
		}
		useCases = append(useCases, view)
	}

	return &gen.AdminOrganizationEncryption{
		OrganizationID:          organizationID,
		Config:                  config,
		UseCases:                useCases,
		MinimumDualWriteSeconds: int64(organizationEncryptionMinimumDualWrite / time.Second),
		FinalizeConfirmation:    OrganizationEncryptionFinalizeConfirmation,
		RunsAvailable:           s.orgEncryption.Runs != nil,
	}, nil
}

func (s *Service) ListOrganizationEncryptionTransitions(ctx context.Context, payload *gen.ListOrganizationEncryptionTransitionsPayload) (*gen.AdminOrganizationEncryptionTransitionList, error) {
	if err := s.requireOrganizationEncryption(ctx); err != nil {
		return nil, err
	}

	if len(s.orgEncryption.UseCases) == 0 {
		return nil, oops.E(oops.CodeUnavailable, nil, "no use case moves to organization keys on this server").LogWarn(ctx, s.logger)
	}
	move := s.orgEncryption.UseCases[0]
	if payload.UseCase != nil {
		var err error
		if move, err = s.encryptionUseCase(ctx, s.logger, *payload.UseCase); err != nil {
			return nil, err
		}
	}
	logger := s.logger.With(attr.SlogOrgEncryptionUseCase(move.UseCase))

	q := repo.New(s.db)
	rows, err := q.AdminListOrganizationEncryptionTransitions(ctx, repo.AdminListOrganizationEncryptionTransitionsParams{
		UseCase:     string(move.UseCase),
		State:       conv.PtrToPGText(payload.State),
		LimitValue:  conv.SafeInt32(payload.Limit + 1),
		OffsetValue: conv.SafeInt32(payload.Offset),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing organization encryption transitions").LogError(ctx, logger)
	}

	hasMore := len(rows) > payload.Limit
	if hasMore {
		rows = rows[:payload.Limit]
	}

	organizations := make([]*gen.AdminOrganizationEncryptionTransitionRow, 0, len(rows))
	for _, row := range rows {
		organizations = append(organizations, &gen.AdminOrganizationEncryptionTransitionRow{
			OrganizationID:          row.OrganizationID,
			Name:                    row.Name,
			Slug:                    row.Slug,
			Disabled:                row.Disabled,
			State:                   row.State,
			StateChangedAt:          encryptionTimeView(row.StateChangedAt),
			PlatformCopiesClearedAt: encryptionTimeView(row.PlatformCopiesClearedAt),
			LastBackfillStartedAt:   encryptionTimeView(row.LastBackfillStartedAt),
			LastBackfillCompletedAt: encryptionTimeView(row.LastBackfillCompletedAt),
			LastBackfillRemaining:   conv.FromPGInt8(row.LastBackfillRemainingCount),
			LastBackfillUnreadable:  conv.FromPGInt8(row.LastBackfillUnreadableCount),
			LastBackfillError:       conv.FromPGText[string](row.LastBackfillError),
			LastVerifiedAt:          encryptionTimeView(row.LastVerifiedAt),
		})
	}

	counts, err := q.AdminCountOrganizationEncryptionTransitionStates(ctx, string(move.UseCase))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error counting organization encryption transitions").LogError(ctx, logger)
	}

	stateCounts := make([]*gen.AdminOrganizationEncryptionStateCount, 0, len(counts))
	for _, count := range counts {
		stateCounts = append(stateCounts, &gen.AdminOrganizationEncryptionStateCount{State: count.State, Organizations: count.Organizations})
	}

	return &gen.AdminOrganizationEncryptionTransitionList{
		UseCase:       string(move.UseCase),
		UseCases:      s.encryptionUseCaseNames(),
		Organizations: organizations,
		StateCounts:   stateCounts,
		HasMore:       hasMore,
	}, nil
}

func (s *Service) EnrollOrganizationEncryption(ctx context.Context, payload *gen.EnrollOrganizationEncryptionPayload) (*gen.AdminOrganizationEncryptionUseCase, error) {
	organizationID, move, logger, err := s.encryptionTarget(ctx, payload.OrganizationID, payload.UseCase)
	if err != nil {
		return nil, err
	}

	if _, err := s.orgEncryption.Transitions.Enroll(ctx, organizationID, move.UseCase, encryptionActor(ctx)); err != nil {
		return nil, mapOrganizationEncryptionError(ctx, logger, err, "enroll")
	}

	return s.encryptionUseCaseAfterChange(ctx, logger, organizationID, move)
}

func (s *Service) RollbackOrganizationEncryption(ctx context.Context, payload *gen.RollbackOrganizationEncryptionPayload) (*gen.AdminOrganizationEncryptionUseCase, error) {
	organizationID, move, logger, err := s.encryptionTarget(ctx, payload.OrganizationID, payload.UseCase)
	if err != nil {
		return nil, err
	}

	if _, err := s.orgEncryption.Transitions.Rollback(ctx, organizationID, move.UseCase, encryptionActor(ctx)); err != nil {
		return nil, mapOrganizationEncryptionError(ctx, logger, err, "roll back")
	}

	return s.encryptionUseCaseAfterChange(ctx, logger, organizationID, move)
}

func (s *Service) RunOrganizationEncryption(ctx context.Context, payload *gen.RunOrganizationEncryptionPayload) (*gen.AdminOrganizationEncryptionUseCase, error) {
	organizationID, move, logger, err := s.encryptionTarget(ctx, payload.OrganizationID, payload.UseCase)
	if err != nil {
		return nil, err
	}
	if s.orgEncryption.Runs == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "organization encryption runs are not available on this server").LogWarn(ctx, logger)
	}

	transition, err := s.orgEncryption.Transitions.GetFresh(ctx, organizationID, move.UseCase)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading organization encryption transition").LogError(ctx, logger)
	}
	switch {
	case transition.State == orgencryption.TransitionStatePlatformKey:
		return nil, oops.E(oops.CodeConflict, nil, "the organization is not enrolled for this use case; enroll it first").LogInfo(ctx, logger)
	case transition.State == orgencryption.TransitionStateOrgKMS && transition.PlatformCopiesClearedAt != nil:
		return nil, oops.E(oops.CodeConflict, nil, "the platform-key copies are already cleared; there is nothing left to run").LogInfo(ctx, logger)
	}

	if err := s.orgEncryption.Runs.StartOrganizationEncryptionRun(ctx, organizationID, move.UseCase, encryptionActor(ctx)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting the organization encryption run").LogError(ctx, logger)
	}

	return s.encryptionUseCaseAfterChange(ctx, logger, organizationID, move)
}

func (s *Service) FinalizeOrganizationEncryption(ctx context.Context, payload *gen.FinalizeOrganizationEncryptionPayload) (*gen.AdminOrganizationEncryptionFinalizeResult, error) {
	organizationID, move, logger, err := s.encryptionTarget(ctx, payload.OrganizationID, payload.UseCase)
	if err != nil {
		return nil, err
	}
	if payload.Confirmation != OrganizationEncryptionFinalizeConfirmation {
		return nil, oops.E(oops.CodeBadRequest, nil, "type %q to confirm finalize", OrganizationEncryptionFinalizeConfirmation).LogWarn(ctx, logger)
	}
	if payload.AcknowledgedUnreadableValues < 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "the acknowledged unreadable value count cannot be negative").LogWarn(ctx, logger)
	}

	actor := encryptionActor(ctx)
	finalized, err := s.orgEncryption.Transitions.Finalize(ctx, organizationID, move.UseCase, orgencryption.FinalizeRequest{
		MinimumDualWrite:   organizationEncryptionMinimumDualWrite,
		Stores:             move.Stores,
		ExpectedUnreadable: new(payload.AcknowledgedUnreadableValues),
	}, actor)
	if err != nil {
		return nil, mapOrganizationEncryptionError(ctx, logger, err, "finalize")
	}

	_, _, operatorEmail := adminActor(ctx)
	logger.InfoContext(ctx, "finalized organization encryption", attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")))

	// The use case is in org_kms now, so a failure from here on must not read
	// as a failed finalize. The hourly background run finishes any clear
	// this does not start.
	clearingStarted := false
	if s.orgEncryption.Runs != nil {
		if err := s.orgEncryption.Runs.StartOrganizationEncryptionRun(ctx, organizationID, move.UseCase, actor); err != nil {
			logger.ErrorContext(ctx, "start clearing platform-key copies after finalize", attr.SlogError(err))
		} else {
			clearingStarted = true
		}
	}

	view, err := s.encryptionUseCaseView(ctx, organizationID, move)
	if err != nil {
		// Reporting an error would read as a failed finalize, and a retry
		// would be refused from org_kms. Return what Finalize recorded; the
		// dashboard reloads the counts.
		logger.ErrorContext(ctx, "read organization encryption after finalize", attr.SlogError(err))
		view = transitionView(finalized, orgencryption.CopyCounts{Waiting: 0, Unreadable: 0, Unattributed: 0, PlatformCopies: 0}, nil)
	}

	return &gen.AdminOrganizationEncryptionFinalizeResult{UseCase: view, ClearingStarted: clearingStarted}, nil
}

func (s *Service) requireOrganizationEncryption(ctx context.Context) error {
	if s.orgEncryption == nil {
		return oops.E(oops.CodeUnavailable, nil, "organization encryption is not available on this server").LogWarn(ctx, s.logger)
	}

	return nil
}

// resolveEncryptionOrganization resolves the organization a request names by
// id or slug.
func (s *Service) resolveEncryptionOrganization(ctx context.Context, idOrSlug string) (string, *slog.Logger, error) {
	organizationID, err := repo.New(s.db).AdminResolveOrganizationID(ctx, idOrSlug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return "", nil, oops.E(oops.CodeUnexpected, err, "error resolving organization").LogError(ctx, s.logger)
	}

	return organizationID, s.logger.With(attr.SlogOrganizationID(organizationID)), nil
}

// encryptionUseCase returns the use case named value when it moves to
// organization keys.
func (s *Service) encryptionUseCase(ctx context.Context, logger *slog.Logger, value string) (orgencryption.UseCaseStores, error) {
	for _, move := range s.orgEncryption.UseCases {
		if string(move.UseCase) == value {
			return move, nil
		}
	}

	return orgencryption.UseCaseStores{UseCase: "", Stores: nil}, oops.E(oops.CodeBadRequest, nil, "use case %q does not move to organization keys", value).LogWarn(ctx, logger)
}

// encryptionTarget checks that the methods are available and resolves the
// organization and use case an action applies to. Like every admin write, an
// action is keyed on the organization id alone: a slug that happens to equal
// another organization's id must never redirect a destructive change.
func (s *Service) encryptionTarget(ctx context.Context, id string, useCase string) (string, orgencryption.UseCaseStores, *slog.Logger, error) {
	empty := orgencryption.UseCaseStores{UseCase: "", Stores: nil}
	if err := s.requireOrganizationEncryption(ctx); err != nil {
		return "", empty, nil, err
	}

	organizationID, logger, err := s.resolveEncryptionOrganization(ctx, id)
	if err != nil {
		return "", empty, nil, err
	}
	if organizationID != id {
		return "", empty, nil, oops.C(oops.CodeNotFound)
	}

	move, err := s.encryptionUseCase(ctx, logger, useCase)
	if err != nil {
		return "", empty, nil, err
	}

	return organizationID, move, logger.With(attr.SlogOrgEncryptionUseCase(move.UseCase)), nil
}

func (s *Service) encryptionUseCaseNames() []string {
	names := make([]string, 0, len(s.orgEncryption.UseCases))
	for _, move := range s.orgEncryption.UseCases {
		names = append(names, string(move.UseCase))
	}

	return names
}

// encryptionUseCaseView reads one use case's transition state and live
// counts.
func (s *Service) encryptionUseCaseView(ctx context.Context, organizationID string, move orgencryption.UseCaseStores) (*gen.AdminOrganizationEncryptionUseCase, error) {
	transition, err := s.orgEncryption.Transitions.GetFresh(ctx, organizationID, move.UseCase)
	if err != nil {
		return nil, fmt.Errorf("load %s transition: %w", move.UseCase, err)
	}

	counts, err := orgencryption.CountStores(ctx, organizationID, move.Stores)
	if err != nil {
		return nil, fmt.Errorf("count %s values: %w", move.UseCase, err)
	}

	var blockers []string
	if transition.State == orgencryption.TransitionStateDualWrite {
		blockers = orgencryption.FinalizeBlockers(transition, counts, time.Now(), organizationEncryptionMinimumDualWrite)

		config, err := s.encryptionConfig(ctx, organizationID)
		if err != nil {
			return nil, err
		}
		if blocker := encryptionKeyBlocker(config); blocker != "" {
			blockers = append(blockers, blocker)
		}
	}

	return transitionView(transition, counts, blockers), nil
}

// encryptionConfig reads the organization's encryption configuration, nil
// until its key is provisioned.
func (s *Service) encryptionConfig(ctx context.Context, organizationID string) (*orgencryptionrepo.OrganizationEncryptionConfig, error) {
	row, err := orgencryptionrepo.New(s.db).GetOrganizationEncryptionConfig(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("load organization encryption config: %w", err)
	}

	return &row, nil
}

// encryptionKeyBlocker reports why the organization's key keeps finalize from
// proceeding, or "" when it is usable. Finalize checks again under its lock.
func encryptionKeyBlocker(config *orgencryptionrepo.OrganizationEncryptionConfig) string {
	switch {
	case config == nil:
		return "the organization's key is not provisioned"
	case orgencryption.ConfigState(config.State) == orgencryption.ConfigStateActive, orgencryption.ConfigState(config.State) == orgencryption.ConfigStateSwitching:
		return ""
	default:
		return fmt.Sprintf("the organization's key is %s, so its values could not be read after finalize", config.State)
	}
}

func transitionView(transition orgencryption.Transition, counts orgencryption.CopyCounts, blockers []string) *gen.AdminOrganizationEncryptionUseCase {
	if blockers == nil {
		blockers = []string{}
	}

	var stateChangedAt *string
	if !transition.StateChangedAt.IsZero() {
		stateChangedAt = new(transition.StateChangedAt.Format(time.RFC3339))
	}

	backfill := transition.LastBackfill

	return &gen.AdminOrganizationEncryptionUseCase{
		UseCase:                 string(transition.UseCase),
		State:                   string(transition.State),
		Generation:              transition.Generation,
		StateChangedAt:          stateChangedAt,
		PlatformCopiesClearedAt: formatOptionalTime(transition.PlatformCopiesClearedAt),
		WaitingValues:           counts.Waiting,
		UnreadableValues:        counts.Unreadable,
		UnattributedValues:      counts.Unattributed,
		PlatformCopies:          counts.PlatformCopies,
		LastBackfillStartedAt:   formatOptionalTime(backfill.StartedAt),
		LastBackfillCompletedAt: formatOptionalTime(backfill.CompletedAt),
		LastBackfillConverted:   backfill.Converted,
		LastBackfillRemaining:   backfill.Remaining,
		LastBackfillUnreadable:  backfill.Unreadable,
		LastBackfillError:       backfill.Error,
		LastVerifiedAt:          formatOptionalTime(transition.LastVerifiedAt),
		FinalizeBlockers:        blockers,
	}
}

// encryptionUseCaseAfterChange reads the use case a change just landed on.
func (s *Service) encryptionUseCaseAfterChange(ctx context.Context, logger *slog.Logger, organizationID string, move orgencryption.UseCaseStores) (*gen.AdminOrganizationEncryptionUseCase, error) {
	view, err := s.encryptionUseCaseView(ctx, organizationID, move)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "the change was applied but the transition could not be read").LogError(ctx, logger)
	}

	return view, nil
}

// encryptionActor attributes a transition to the staff member behind the
// request.
func encryptionActor(ctx context.Context) orgencryption.Actor {
	principal, displayName, _ := adminActor(ctx)

	return orgencryption.Actor{Principal: principal, DisplayName: displayName}
}

func mapOrganizationEncryptionError(ctx context.Context, logger *slog.Logger, err error, action string) error {
	detail := conv.TruncateDetail(err.Error(), organizationEncryptionDetailMaxLen)

	switch {
	case errors.Is(err, orgencryption.ErrUnknownUseCase):
		return oops.E(oops.CodeBadRequest, err, "%s", detail).LogWarn(ctx, logger)
	case errors.Is(err, orgencryption.ErrTransitionNotAllowed),
		errors.Is(err, orgencryption.ErrFinalizeBlocked),
		errors.Is(err, orgencryption.ErrUnreadableCountChanged),
		errors.Is(err, orgencryption.ErrConcurrentChange):
		return oops.E(oops.CodeConflict, err, "cannot %s: %s", action, detail).LogInfo(ctx, logger)
	case orgencryption.IsKeyProblem(err):
		return oops.E(oops.CodeGatewayError, err, "cannot %s, the organization's key could not be used: %s", action, detail).LogWarn(ctx, logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "error trying to %s organization encryption", action).LogError(ctx, logger)
	}
}

func encryptionConfigView(row orgencryptionrepo.OrganizationEncryptionConfig) *gen.AdminOrganizationEncryptionConfig {
	return &gen.AdminOrganizationEncryptionConfig{
		Mode:            row.Mode,
		State:           row.State,
		SwitchStartedAt: encryptionTimeView(row.SwitchStartedAt),
		SuspendedAt:     encryptionTimeView(row.SuspendedAt),
		SuspendedReason: conv.FromPGText[string](row.SuspendedReason),
		LastCheckAt:     encryptionTimeView(row.LastKekCheckAt),
		LastCheckError:  conv.FromPGText[string](row.LastKekCheckError),
	}
}

func encryptionTimeView(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}

	return new(ts.Time.Format(time.RFC3339))
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}

	return new(t.Format(time.RFC3339))
}
