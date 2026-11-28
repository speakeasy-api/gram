package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/userblock"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	blockStepSucceeded     = "succeeded"
	blockStepPartial       = "partial"
	blockStepSkipped       = "skipped"
	blockStepFailed        = "failed"
	blockStepNotConfigured = "not_configured"

	blockUserCacheTimeout      = 5 * time.Second
	blockUserWorkOSStepTimeout = 10 * time.Second

	logEventAdminUserBlocked = "admin_user_blocked"
)

// WorkOSUserBlocker is the narrow WorkOS surface used by BlockUser to mark
// WorkOS user metadata, list and revoke WorkOS AuthKit sessions, and add the
// user's email to the WorkOS Radar email blocklist.
type WorkOSUserBlocker interface {
	SetUserBlacklistedMetadata(ctx context.Context, workosUserID string, blacklisted bool) error
	ListUserSessionIDs(ctx context.Context, workosUserID string) ([]string, error)
	RevokeSession(ctx context.Context, sessionID string) error
	AddRadarEmailBlocklistEntry(ctx context.Context, email string) error
}

// SetWorkOSUserBlocker overrides the WorkOS user-blocking client. When nil,
// BlockUser still commits the local database blacklist and cascade and reports
// WorkOS steps as not_configured.
func (s *Service) SetWorkOSUserBlocker(blocker WorkOSUserBlocker) {
	s.workosUserBlocker = blocker
}

// BlockUser is the staff-only one-step user blocking action. It:
//
//  1. Validates the target (refusing active platform admins, active staff-org
//     members, and self-blocks) and runs userblock.Blacklist atomically in a
//     single database transaction (marking users.blacklisted_at, disabling
//     sole-member organizations, revoking API keys, sign-ins, hosted MCP
//     sessions/consents/clients, Platform MCP connections/sessions/grants/
//     clients, and Channels OAuth grants/clients, writing audit logs, and
//     invalidating the local process's in-memory userblock snapshot).
//  2. Pushes immediate Redis dashboard session-epoch and revoked-JTI cache
//     updates (with durable retry rows already committed in Postgres).
//  3. Runs best-effort WorkOS steps (blacklisted=true metadata, listing and
//     revoking all WorkOS sessions, and adding the email to the WorkOS Radar
//     email blocklist) with structured partial-failure reporting.
//  4. Reports the registry disable step as not_configured until service-to-
//     service credentials and user-ID mapping exist.
//
// Admin MCP parity: deliberately NOT exposed as an Admin MCP tool. Like
// SetSelfServeDisabled, blocking a user is a platform-wide destructive action
// with irreversible external WorkOS side effects outside a single database
// transaction, so it should stay a deliberate staff action rather than an
// agent-invoked tool.
func (s *Service) BlockUser(ctx context.Context, payload *gen.BlockUserPayload) (*gen.AdminBlockUserResult, error) {
	operatorEmail, err := hooksRolloutOperator(ctx)
	if err != nil {
		return nil, err
	}
	actor, actorDisplayName, _ := adminActor(ctx)

	userID := strings.TrimSpace(payload.UserID)
	if userID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "user_id is required")
	}
	reason := strings.TrimSpace(conv.PtrValOr(payload.Reason, ""))
	logger := s.logger.With(
		attr.SlogAuthUserEmail(operatorEmail),
		attr.SlogUserID(userID),
	)

	target, err := usersrepo.New(s.db).GetUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "user not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load user for block").LogError(ctx, logger)
	}
	if strings.EqualFold(strings.TrimSpace(target.Email), strings.TrimSpace(operatorEmail)) {
		return nil, oops.E(oops.CodeBadRequest, nil, "cannot block your own account")
	}
	if target.Admin && !target.DeletedAt.Valid {
		return nil, oops.E(oops.CodeForbidden, nil, "cannot block an active platform administrator")
	}
	if s.staffOrg.Configured() {
		isStaff, err := orgRepo.New(s.db).HasActiveOrganizationUser(ctx, orgRepo.HasActiveOrganizationUserParams{
			OrganizationID: s.staffOrg.ID(),
			UserID:         userID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check staff organization membership").LogError(ctx, logger)
		}
		if isStaff {
			return nil, oops.E(oops.CodeForbidden, nil, "cannot block an active staff organization member")
		}
	}

	cascade, err := userblock.Blacklist(ctx, s.db, userblock.BlacklistParams{
		UserID: userID,
		Actor: userblock.Actor{
			Principal:   actor,
			DisplayName: actorDisplayName,
		},
		Reason:            reason,
		StaffOrganization: s.staffOrg,
		AuditLogger:       s.audit,
		Logger:            logger,
	})
	if errors.Is(err, userblock.ErrUserNotFound) {
		return nil, oops.E(oops.CodeNotFound, err, "user not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "blacklist user and cascade revocations").LogError(ctx, logger)
	}

	warnings := make([]string, 0)

	// Step 2: Immediate Redis cache invalidation (dashboard epoch + revoked JTIs).
	// Detached from request cancellation because the database block already
	// committed; durable retry rows are already persisted in
	// sign_in_revocation_deliveries.
	sessionCacheStep, cacheWarnings := s.pushBlockSessionCaches(ctx, cascade)
	warnings = append(warnings, cacheWarnings...)

	// Step 3: Best-effort WorkOS synchronization (metadata, session revocation,
	// Radar email blocklist). Failures never roll back the committed DB block.
	workosMetadataStep, workosSessionsStep, workosSessionsRevoked, workosRadarStep, workosWarnings := s.applyWorkOSBlockSteps(ctx, cascade)
	warnings = append(warnings, workosWarnings...)

	// Step 4: Registry user disable.
	// TODO: Wire service-to-service registry disable once a sanctioned client,
	// base URL, admin "self" workspace credential (x-api-key or admin Bearer
	// token), and WorkOS/local-user-ID to registry user UUID mapping exist in
	// this server. The registry exposes POST /v1/admin/users/{userID}/disable
	// (no request body; returns {user_id, disabled_at, revoked_api_keys,
	// revoked_publishing_tokens}).
	registryDetail := "registry service-to-service client, admin credential, and registry user UUID mapping are not configured; disable the user in the registry separately via POST /v1/admin/users/{userID}/disable"
	registryStep := &gen.AdminBlockUserStepResult{
		Status: blockStepNotConfigured,
		Detail: &registryDetail,
	}
	warnings = append(warnings, "registry disable: "+registryDetail)

	disabledOrgIDs := make([]string, 0, len(cascade.DisabledOrganizations))
	for _, org := range cascade.DisabledOrganizations {
		disabledOrgIDs = append(disabledOrgIDs, org.ID)
	}

	var workosUserIDPtr *string
	if cascade.WorkOSUserID != "" {
		id := cascade.WorkOSUserID
		workosUserIDPtr = &id
	}

	logger.WarnContext(ctx, "staff blocked user",
		attr.SlogEvent(logEventAdminUserBlocked),
		attr.SlogWorkOSUserID(cascade.WorkOSUserID),
		attr.SlogReason(reason),
	)

	return &gen.AdminBlockUserResult{
		UserID:                             cascade.UserID,
		Email:                              cascade.Email,
		WorkosUserID:                       workosUserIDPtr,
		AlreadyBlacklisted:                 cascade.AlreadyBlacklisted,
		BlacklistedAt:                      cascade.BlacklistedAt.UTC().Format(time.RFC3339Nano),
		DisabledOrganizationIds:            disabledOrgIDs,
		RevokedAPIKeyCount:                 len(cascade.RevokedAPIKeyIDs),
		RevokedSignInCount:                 cascade.RevokedSignInCount,
		RevokedUserSessionCount:            len(cascade.RevokedUserSessionIDs),
		RevokedUserSessionConsentCount:     len(cascade.RevokedUserSessionConsentIDs),
		RevokedUserSessionClientCount:      len(cascade.RevokedUserSessionClientIDs),
		RevokedPlatformMcpConnectionCount:  len(cascade.RevokedPlatformMCPConnectionID),
		RevokedPlatformMcpSessionCount:     len(cascade.RevokedPlatformMCPSessionIDs),
		RevokedPlatformMcpOauthClientCount: len(cascade.RevokedPlatformMCPOAuthClients),
		RevokedChannelsOauthGrantCount:     len(cascade.RevokedChannelsOAuthGrantIDs),
		RevokedChannelsOauthClientCount:    len(cascade.RevokedChannelsOAuthClients),
		SessionCache:                       sessionCacheStep,
		WorkosMetadata:                     workosMetadataStep,
		WorkosSessions:                     workosSessionsStep,
		WorkosSessionsRevokedCount:         workosSessionsRevoked,
		WorkosRadar:                        workosRadarStep,
		RegistryDisable:                    registryStep,
		Warnings:                           warnings,
	}, nil
}

func (s *Service) pushBlockSessionCaches(ctx context.Context, cascade userblock.CascadeResult) (*gen.AdminBlockUserStepResult, []string) {
	if s.epochRaiser == nil && s.tokenRevoker == nil {
		detail := "session cache revokers are not configured; durable deliveries remain queued for background sweep"
		return &gen.AdminBlockUserStepResult{Status: blockStepNotConfigured, Detail: &detail}, []string{"session cache: " + detail}
	}

	pushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), blockUserCacheTimeout)
	defer cancel()

	var warnings []string
	succeeded := 0
	failed := 0

	if s.epochRaiser != nil {
		epochAt := cascade.RevocationCutoffAt
		if epochAt.IsZero() {
			epochAt = cascade.BlacklistedAt
		}
		if err := s.epochRaiser.RaiseUserEpoch(pushCtx, cascade.UserID, epochAt); err != nil {
			failed++
			warnings = append(warnings, fmt.Sprintf("session cache: raise dashboard user epoch failed (queued for retry): %v", err))
		} else {
			succeeded++
		}
	}

	if s.tokenRevoker != nil {
		jtiFailures := 0
		var firstErr error
		for _, jti := range cascade.RevokedJTIs {
			if pushCtx.Err() != nil {
				if firstErr == nil {
					firstErr = pushCtx.Err()
				}
				jtiFailures++
				continue
			}
			if err := s.tokenRevoker.RevokeToken(pushCtx, jti); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				jtiFailures++
				continue
			}
			succeeded++
		}
		if jtiFailures > 0 {
			failed += jtiFailures
			warnings = append(warnings, fmt.Sprintf("session cache: failed to push %d of %d revoked JTIs (queued for retry): %v", jtiFailures, len(cascade.RevokedJTIs), firstErr))
		}
	}

	switch {
	case failed == 0:
		return &gen.AdminBlockUserStepResult{Status: blockStepSucceeded, Detail: nil}, nil
	case succeeded > 0:
		detail := strings.Join(warnings, "; ")
		return &gen.AdminBlockUserStepResult{Status: blockStepPartial, Detail: &detail}, warnings
	default:
		detail := strings.Join(warnings, "; ")
		return &gen.AdminBlockUserStepResult{Status: blockStepFailed, Detail: &detail}, warnings
	}
}

func (s *Service) applyWorkOSBlockSteps(ctx context.Context, cascade userblock.CascadeResult) (
	metadataStep *gen.AdminBlockUserStepResult,
	sessionsStep *gen.AdminBlockUserStepResult,
	sessionsRevokedCount int,
	radarStep *gen.AdminBlockUserStepResult,
	warnings []string,
) {
	if s.workosUserBlocker == nil {
		detail := "WorkOS client is not configured"
		notConfigured := func() *gen.AdminBlockUserStepResult {
			d := detail
			return &gen.AdminBlockUserStepResult{Status: blockStepNotConfigured, Detail: &d}
		}
		return notConfigured(), notConfigured(), 0, notConfigured(), []string{"WorkOS: client is not configured; skipped metadata update, session revocation, and Radar email blocklist"}
	}

	baseCtx := context.WithoutCancel(ctx)

	// 1. WorkOS user metadata: blacklisted = "true". Each independent step gets
	// its own bounded timeout so a slow or failing WorkOS endpoint cannot starve
	// the remaining steps.
	if cascade.WorkOSUserID == "" {
		detail := "user has no linked WorkOS user ID"
		metadataStep = &gen.AdminBlockUserStepResult{Status: blockStepSkipped, Detail: &detail}
	} else {
		stepCtx, cancel := context.WithTimeout(baseCtx, blockUserWorkOSStepTimeout)
		err := s.workosUserBlocker.SetUserBlacklistedMetadata(stepCtx, cascade.WorkOSUserID, true)
		cancel()
		if err != nil {
			detail := fmt.Sprintf("set WorkOS blacklisted metadata: %v", err)
			metadataStep = &gen.AdminBlockUserStepResult{Status: blockStepFailed, Detail: &detail}
			warnings = append(warnings, "WorkOS metadata: "+detail)
		} else {
			metadataStep = &gen.AdminBlockUserStepResult{Status: blockStepSucceeded, Detail: nil}
		}
	}

	// 2. WorkOS sessions: list and revoke all active sessions.
	if cascade.WorkOSUserID == "" {
		detail := "user has no linked WorkOS user ID"
		sessionsStep = &gen.AdminBlockUserStepResult{Status: blockStepSkipped, Detail: &detail}
	} else {
		stepCtx, cancel := context.WithTimeout(baseCtx, blockUserWorkOSStepTimeout)
		sessionIDs, err := s.workosUserBlocker.ListUserSessionIDs(stepCtx, cascade.WorkOSUserID)
		if err != nil {
			cancel()
			detail := fmt.Sprintf("list WorkOS sessions: %v", err)
			sessionsStep = &gen.AdminBlockUserStepResult{Status: blockStepFailed, Detail: &detail}
			warnings = append(warnings, "WorkOS sessions: "+detail)
		} else {
			failedCount := 0
			var firstErr error
			for _, sessionID := range sessionIDs {
				if err := s.workosUserBlocker.RevokeSession(stepCtx, sessionID); err != nil {
					if signins.WorkOSSessionGone(err) {
						continue
					}
					if firstErr == nil {
						firstErr = err
					}
					failedCount++
					continue
				}
				sessionsRevokedCount++
			}
			cancel()
			switch {
			case failedCount == 0 && len(sessionIDs) == 0:
				detail := "no active WorkOS sessions found"
				sessionsStep = &gen.AdminBlockUserStepResult{Status: blockStepSucceeded, Detail: &detail}
			case failedCount == 0:
				sessionsStep = &gen.AdminBlockUserStepResult{Status: blockStepSucceeded, Detail: nil}
			case sessionsRevokedCount > 0:
				detail := fmt.Sprintf("revoked %d WorkOS sessions; %d failed: %v", sessionsRevokedCount, failedCount, firstErr)
				sessionsStep = &gen.AdminBlockUserStepResult{Status: blockStepPartial, Detail: &detail}
				warnings = append(warnings, "WorkOS sessions: "+detail)
			default:
				detail := fmt.Sprintf("failed to revoke %d WorkOS sessions: %v", failedCount, firstErr)
				sessionsStep = &gen.AdminBlockUserStepResult{Status: blockStepFailed, Detail: &detail}
				warnings = append(warnings, "WorkOS sessions: "+detail)
			}
		}
	}

	// 3. WorkOS Radar email blocklist.
	if strings.TrimSpace(cascade.Email) == "" {
		detail := "user has no email address"
		radarStep = &gen.AdminBlockUserStepResult{Status: blockStepSkipped, Detail: &detail}
	} else {
		stepCtx, cancel := context.WithTimeout(baseCtx, blockUserWorkOSStepTimeout)
		err := s.workosUserBlocker.AddRadarEmailBlocklistEntry(stepCtx, cascade.Email)
		cancel()
		if err != nil {
			detail := fmt.Sprintf("add email to WorkOS Radar blocklist: %v", err)
			radarStep = &gen.AdminBlockUserStepResult{Status: blockStepFailed, Detail: &detail}
			warnings = append(warnings, "WorkOS Radar: "+detail)
		} else {
			radarStep = &gen.AdminBlockUserStepResult{Status: blockStepSucceeded, Detail: nil}
		}
	}

	return metadataStep, sessionsStep, sessionsRevokedCount, radarStep, warnings
}
