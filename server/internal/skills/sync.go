package skills

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func (s *Service) requireSyncAccess(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.OrgWidePluginHooksKey {
		return nil, oops.E(oops.CodeForbidden, nil, "shared organization hooks keys cannot synchronize user skills")
	}
	activeMember, err := orgrepo.New(s.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check active organization membership").LogError(ctx, s.logger)
	}
	if !activeMember {
		return nil, oops.E(oops.CodeForbidden, nil, "user is not an active organization member")
	}

	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check skills feature").LogError(ctx, s.logger)
	}
	if !enabled {
		return nil, oops.E(oops.CodeForbidden, nil, "skills are not enabled for this organization")
	}

	return authCtx, nil
}

func validateSyncPayload(payload *gen.SyncPayload) error {
	if payload == nil {
		return oops.E(oops.CodeBadRequest, nil, "sync payload is required")
	}
	if payload.Hostname == "" || strings.TrimSpace(payload.Hostname) != payload.Hostname || utf8.RuneCountInString(payload.Hostname) > 255 {
		return oops.E(oops.CodeBadRequest, nil, "hostname must be between 1 and 255 characters without surrounding whitespace")
	}
	if payload.Provider != "claude" {
		return oops.E(oops.CodeBadRequest, nil, "provider must be claude")
	}
	if len(payload.Installed) > 200 || len(payload.Exceptions) > 200 {
		return oops.E(oops.CodeBadRequest, nil, "installed and exceptions may each contain at most 200 entries")
	}

	installedNames := make(map[string]struct{}, len(payload.Installed))
	for _, installed := range payload.Installed {
		if installed == nil || !validSpecName(installed.Name) || !validRawSHA256(installed.RawSha256) {
			return oops.E(oops.CodeBadRequest, nil, "installed entries require a valid normalized name and lowercase SHA-256 hash")
		}
		if _, duplicate := installedNames[installed.Name]; duplicate {
			return oops.E(oops.CodeBadRequest, nil, "duplicate installed skill name %q", installed.Name)
		}
		installedNames[installed.Name] = struct{}{}
	}

	exceptionNames := make(map[string]struct{}, len(payload.Exceptions))
	for _, exception := range payload.Exceptions {
		if exception == nil || !validSpecName(exception.Name) {
			return oops.E(oops.CodeBadRequest, nil, "exceptions require a valid normalized name")
		}
		switch SyncReceiptStatus(exception.Status) {
		case SyncReceiptStatusConflictSkipped, SyncReceiptStatusFSReadonly:
		default:
			return oops.E(oops.CodeBadRequest, nil, "invalid exception status for skill %q", exception.Name)
		}
		if _, duplicate := exceptionNames[exception.Name]; duplicate {
			return oops.E(oops.CodeBadRequest, nil, "duplicate exception skill name %q", exception.Name)
		}
		if _, overlap := installedNames[exception.Name]; overlap {
			return oops.E(oops.CodeBadRequest, nil, "skill %q cannot be both installed and excepted", exception.Name)
		}
		exceptionNames[exception.Name] = struct{}{}
	}

	return nil
}

func validRawSHA256(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, r := range hash {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func (s *Service) Sync(ctx context.Context, payload *gen.SyncPayload) (*gen.SyncSkillsResult, error) {
	authCtx, err := s.requireSyncAccess(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateSyncPayload(payload); err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogUserID(authCtx.UserID),
		attr.SlogHostName(payload.Hostname),
	)
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin skill sync transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	machine := repo.LockSkillSyncMachineParams{
		ProjectID: *authCtx.ProjectID,
		UserID:    authCtx.UserID,
		Hostname:  payload.Hostname,
		Provider:  payload.Provider,
	}
	if err := queries.LockSkillSyncMachine(ctx, machine); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill sync machine").LogError(ctx, logger)
	}

	// Skill rows are locked for the transaction. Distribution-row changes may still
	// produce a one-cycle stale plan, which the next serialized sync self-heals.
	visibleRows, err := queries.ListUserSyncableSkillDistributions(ctx, repo.ListUserSyncableSkillDistributionsParams{
		ProjectID: *authCtx.ProjectID,
		UserID:    conv.ToPGText(authCtx.UserID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user skill distributions").LogError(ctx, logger)
	}
	visibleByName := make(map[string]repo.ListUserSyncableSkillDistributionsRow, len(visibleRows))
	for _, row := range visibleRows {
		visibleByName[row.Name] = row
		if row.ResolvedVersionID == uuid.Nil {
			logger.WarnContext(ctx, "visible skill distribution has no resolvable valid version", attr.SlogName(row.Name))
		}
	}

	installedByName := make(map[string]*gen.SyncSkillInstalled, len(payload.Installed))
	receiptSkillIDs := make([]uuid.UUID, 0, len(payload.Installed)+len(payload.Exceptions))
	receiptVersionIDs := make([]uuid.UUID, 0, len(payload.Installed)+len(payload.Exceptions))
	receiptStatuses := make([]string, 0, len(payload.Installed)+len(payload.Exceptions))
	receiptIndexes := make(map[uuid.UUID]int, len(payload.Installed))
	mismatchedSkillIDs := make([]uuid.UUID, 0, len(payload.Installed))
	mismatchedRawSHA256s := make([]string, 0, len(payload.Installed))
	removals := make([]string, 0)
	for _, installed := range payload.Installed {
		installedByName[installed.Name] = installed
		visible, isVisible := visibleByName[installed.Name]
		if !isVisible {
			removals = append(removals, installed.Name)
			continue
		}

		versionID := uuid.Nil
		if visible.ResolvedVersionID != uuid.Nil && installed.RawSha256 == visible.RawSha256 {
			versionID = visible.ResolvedVersionID
		} else {
			mismatchedSkillIDs = append(mismatchedSkillIDs, visible.SkillID)
			mismatchedRawSHA256s = append(mismatchedRawSHA256s, installed.RawSha256)
		}
		receiptIndexes[visible.SkillID] = len(receiptSkillIDs)
		receiptSkillIDs = append(receiptSkillIDs, visible.SkillID)
		receiptVersionIDs = append(receiptVersionIDs, versionID)
		receiptStatuses = append(receiptStatuses, string(SyncReceiptStatusApplied))
	}
	if len(mismatchedSkillIDs) > 0 {
		resolvedVersions, err := queries.ResolveSkillVersionsByRawSHA(ctx, repo.ResolveSkillVersionsByRawSHAParams{
			SkillIds:   mismatchedSkillIDs,
			RawSha256s: mismatchedRawSHA256s,
			ProjectID:  *authCtx.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve installed skill versions").LogError(ctx, logger)
		}
		for _, resolved := range resolvedVersions {
			receiptVersionIDs[receiptIndexes[resolved.SkillID]] = resolved.SkillVersionID
		}
	}

	for _, exception := range payload.Exceptions {
		visible, isVisible := visibleByName[exception.Name]
		if !isVisible {
			continue
		}
		receiptSkillIDs = append(receiptSkillIDs, visible.SkillID)
		receiptVersionIDs = append(receiptVersionIDs, uuid.Nil)
		receiptStatuses = append(receiptStatuses, exception.Status)
	}

	reconciled, err := queries.ReconcileSkillSyncReceipts(ctx, repo.ReconcileSkillSyncReceiptsParams{
		SkillIds:        receiptSkillIDs,
		SkillVersionIds: receiptVersionIDs,
		Statuses:        receiptStatuses,
		ProjectID:       *authCtx.ProjectID,
		UserID:          authCtx.UserID,
		Hostname:        payload.Hostname,
		Provider:        payload.Provider,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reconcile skill sync receipts").LogError(ctx, logger)
	}
	if reconciled.UpsertedCount != int64(len(receiptSkillIDs)) {
		return nil, oops.E(oops.CodeUnexpected, nil, "not all skill sync receipts passed active skill and version validation").LogError(ctx, logger)
	}

	type pendingUpdate struct {
		name      string
		rawSHA256 string
		versionID uuid.UUID
	}
	pendingUpdates := make([]pendingUpdate, 0, len(visibleRows))
	updateVersionIDs := make([]uuid.UUID, 0, len(visibleRows))
	for _, visible := range visibleRows {
		installed, isInstalled := installedByName[visible.Name]
		if visible.ResolvedVersionID == uuid.Nil || (isInstalled && installed.RawSha256 == visible.RawSha256) {
			continue
		}
		pendingUpdates = append(pendingUpdates, pendingUpdate{
			name:      visible.Name,
			rawSHA256: visible.RawSha256,
			versionID: visible.ResolvedVersionID,
		})
		updateVersionIDs = append(updateVersionIDs, visible.ResolvedVersionID)
	}
	contentByVersionID := make(map[uuid.UUID]repo.GetSkillSyncUpdateContentsRow, len(updateVersionIDs))
	if len(updateVersionIDs) > 0 {
		contents, err := queries.GetSkillSyncUpdateContents(ctx, repo.GetSkillSyncUpdateContentsParams{
			ProjectID:       *authCtx.ProjectID,
			SkillVersionIds: updateVersionIDs,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load skill sync update contents").LogError(ctx, logger)
		}
		for _, content := range contents {
			contentByVersionID[content.SkillVersionID] = content
		}
		if len(contentByVersionID) != len(updateVersionIDs) {
			return nil, oops.E(oops.CodeUnexpected, nil, "not all skill sync update contents could be loaded").LogError(ctx, logger)
		}
	}
	updates := make([]*gen.SyncSkillUpdate, 0, len(pendingUpdates))
	for _, pending := range pendingUpdates {
		content := contentByVersionID[pending.versionID]
		updates = append(updates, &gen.SyncSkillUpdate{
			Name:        pending.name,
			RawSha256:   pending.rawSHA256,
			Content:     content.Content,
			Description: conv.FromPGText[string](content.Description),
		})
	}
	slices.SortFunc(updates, func(a, b *gen.SyncSkillUpdate) int { return strings.Compare(a.Name, b.Name) })
	slices.Sort(removals)

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit skill sync transaction").LogError(ctx, logger)
	}

	return &gen.SyncSkillsResult{Updates: updates, Removals: removals}, nil
}
