package userblock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	signinsrepo "github.com/speakeasy-api/gram/server/internal/signins/repo"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/urn"
	blockrepo "github.com/speakeasy-api/gram/server/internal/userblock/repo"
)

// ErrUserNotFound reports that no user row exists for the requested user ID.
var ErrUserNotFound = errors.New("user not found")

// LogEventBlacklisted names the structured log event emitted after a user is
// blacklisted and their credentials and sole-member organizations are cascaded.
const LogEventBlacklisted = "user_blacklisted"

// Sign-in revocation constants mirroring package signins without importing it
// (signins tests depend on testenv -> auth/sessions -> userblock).
const (
	signInRevokeReasonAdmin         = "admin"
	signInDeliveryKindJTI           = "jti"
	signInDeliveryKindWorkOSSession = "workos_session"
)

// Actor identifies who initiated a user blacklist for audit attribution.
type Actor struct {
	Principal   urn.Principal
	DisplayName *string
}

// BlacklistParams configures a transactional user blacklist and revocation cascade.
type BlacklistParams struct {
	UserID            string
	Actor             Actor
	Reason            string
	StaffOrganization staff.Organization
	AuditLogger       *audit.Logger
	Logger            *slog.Logger
}

// DisabledOrganization records an organization disabled because the blocked
// user was its sole remaining active non-staff member.
type DisabledOrganization struct {
	ID         string
	Name       string
	Slug       string
	DisabledAt time.Time
}

// CascadeResult summarizes the user block state and every artifact disabled or
// revoked by the cascade.
type CascadeResult struct {
	UserID                         string
	Email                          string
	DisplayName                    string
	WorkOSUserID                   string
	IsAdmin                        bool
	AlreadyBlacklisted             bool
	BlacklistedAt                  time.Time
	DisabledOrganizations          []DisabledOrganization
	RevokedAPIKeyIDs               []uuid.UUID
	RevokedSignInCount             int
	RevokedUserSessionIDs          []uuid.UUID
	RevokedUserSessionConsentIDs   []uuid.UUID
	RevokedUserSessionClientIDs    []uuid.UUID
	RevokedPlatformMCPConnectionID []uuid.UUID
	RevokedPlatformMCPSessionIDs   []uuid.UUID
	RevokedPlatformMCPGrantIDs     []uuid.UUID
	RevokedPlatformMCPOAuthClients []uuid.UUID
	RevokedChannelsOAuthGrantIDs   []uuid.UUID
	RevokedChannelsOAuthClients    []string
	RevokedJTIs                    []string
	RevocationCutoffAt             time.Time
	DeliveryOrganizations          []string
	DeliveryIDs                    []uuid.UUID
}

type orgCascadeCounts struct {
	organizationDisabled           bool
	revokedAPIKeys                 int
	revokedSignIns                 int
	revokedUserSessions            int
	revokedUserSessionConsents     int
	revokedUserSessionClients      int
	revokedPlatformMCPConnections  int
	revokedPlatformMCPSessions     int
	revokedPlatformMCPOAuthClients int
	revokedChannelsOAuthGrants     int
	revokedChannelsOAuthClients    int
}

func (c orgCascadeCounts) anyChanges() bool {
	return c.organizationDisabled ||
		c.revokedAPIKeys > 0 ||
		c.revokedSignIns > 0 ||
		c.revokedUserSessions > 0 ||
		c.revokedUserSessionConsents > 0 ||
		c.revokedUserSessionClients > 0 ||
		c.revokedPlatformMCPConnections > 0 ||
		c.revokedPlatformMCPSessions > 0 ||
		c.revokedPlatformMCPOAuthClients > 0 ||
		c.revokedChannelsOAuthGrants > 0 ||
		c.revokedChannelsOAuthClients > 0
}

// Blacklist marks the user blacklisted and atomically cascades all database
// revocations in one transaction, then invalidates this process's in-memory
// block snapshot immediately after commit.
func Blacklist(ctx context.Context, db *pgxpool.Pool, params BlacklistParams) (CascadeResult, error) {
	var zero CascadeResult
	if db == nil {
		return zero, fmt.Errorf("blacklist user: database pool is nil")
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin user blacklist transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	result, err := BlacklistTx(ctx, tx, params)
	if err != nil {
		return zero, err
	}

	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit user blacklist transaction: %w", err)
	}

	Forget(db)
	return result, nil
}

// BlacklistTx marks the user blacklisted and cascades all database revocations
// inside the caller's transaction:
//
//   - sets users.blacklisted_at (preserving any earlier timestamp);
//   - disables organizations where the user is the sole active non-staff member;
//   - soft-revokes API keys created by the user (setting deleted_at) and deletes
//     ordinary key policy grants;
//   - revokes recorded sign-ins, raises sign-in revocation cutoffs, enqueues
//     durable dashboard user-epoch and JTI deliveries, revokes user sessions
//     (including agent sessions the user authorized), withdraws remembered
//     consents and their resource rows, and soft-deletes user-owned hosted OAuth
//     clients;
//   - revokes user-owned Platform MCP OAuth clients (never CIMD or shared
//     clients), Platform MCP connections, pending authorization grants, and
//     Platform MCP sessions;
//   - revokes Channels OAuth grants and sole-user Channels OAuth clients;
//   - writes tenant audit log entries for the block and every cascaded tenant
//     artifact.
//
// Callers MUST call Forget(db) after their transaction commits so the local
// process's in-memory gate observes the block immediately.
func BlacklistTx(ctx context.Context, tx pgx.Tx, params BlacklistParams) (CascadeResult, error) {
	var result CascadeResult
	if params.UserID == "" {
		return result, fmt.Errorf("blacklist user: missing user ID")
	}

	actor := params.Actor
	if actor.Principal.IsZero() {
		actor.Principal = urn.NewSystemPrincipal("user-block")
	}
	auditLogger := params.AuditLogger
	if auditLogger == nil {
		auditLogger = audit.NewLogger()
	}
	logger := params.Logger

	q := blockrepo.New(tx)

	locked, err := q.LockUserForBlacklist(ctx, params.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrUserNotFound
	}
	if err != nil {
		return result, fmt.Errorf("lock user for blacklist: %w", err)
	}

	marked, err := q.MarkUserBlacklisted(ctx, params.UserID)
	if err != nil {
		return result, fmt.Errorf("mark user blacklisted: %w", err)
	}

	result.UserID = marked.ID
	result.Email = marked.Email
	result.DisplayName = marked.DisplayName
	result.WorkOSUserID = marked.WorkosID.String
	result.IsAdmin = marked.Admin
	result.AlreadyBlacklisted = locked.BlacklistedAt.Valid
	if marked.BlacklistedAt.Valid {
		result.BlacklistedAt = marked.BlacklistedAt.Time
	}

	sq := signinsrepo.New(tx)

	// Lock user-wide sign-ins before collecting organizations so racing
	// sign-ins either commit before we list or block until after our cutoff.
	if err := sq.LockAllSignInsForUser(ctx, params.UserID); err != nil {
		return result, fmt.Errorf("lock user sign-ins for blacklist: %w", err)
	}

	memberOrgIDs, err := q.ListOrganizationsForBlockedUser(ctx, blockrepo.ListOrganizationsForBlockedUserParams{
		UserID:       conv.ToPGText(params.UserID),
		WorkosUserID: marked.WorkosID,
	})
	if err != nil {
		return result, fmt.Errorf("list organizations for blocked user: %w", err)
	}

	signInOrgIDs, err := sq.ListSignInOrganizationsForUser(ctx, params.UserID)
	if err != nil {
		return result, fmt.Errorf("list sign-in organizations for blocked user: %w", err)
	}

	allSignInOrgs := uniqueSortedStrings(slices.Concat(memberOrgIDs, signInOrgIDs))
	countsByOrg := make(map[string]*orgCascadeCounts, len(allSignInOrgs))
	touchOrg := func(orgID string) *orgCascadeCounts {
		if orgID == "" {
			return nil
		}
		counts, ok := countsByOrg[orgID]
		if !ok {
			counts = &orgCascadeCounts{
				organizationDisabled:           false,
				revokedAPIKeys:                 0,
				revokedSignIns:                 0,
				revokedUserSessions:            0,
				revokedUserSessionConsents:     0,
				revokedUserSessionClients:      0,
				revokedPlatformMCPConnections:  0,
				revokedPlatformMCPSessions:     0,
				revokedPlatformMCPOAuthClients: 0,
				revokedChannelsOAuthGrants:     0,
				revokedChannelsOAuthClients:    0,
			}
			countsByOrg[orgID] = counts
		}
		return counts
	}
	for _, orgID := range allSignInOrgs {
		touchOrg(orgID)
	}

	// 1. Disable organizations where the blocked user is the sole active
	// non-staff member.
	disabledRows, err := q.DisableSoleNonStaffOrganizationsForUser(ctx, blockrepo.DisableSoleNonStaffOrganizationsForUserParams{
		DemoOrganizationID:  constants.DemoOrganizationID,
		StaffOrganizationID: conv.ToPGTextEmpty(params.StaffOrganization.ID()),
		UserID:              conv.ToPGText(params.UserID),
		WorkosUserID:        marked.WorkosID,
	})
	if err != nil {
		return result, fmt.Errorf("disable sole-member organizations for blocked user: %w", err)
	}
	disabledOrgIDs := make([]string, 0, len(disabledRows))
	result.DisabledOrganizations = make([]DisabledOrganization, 0, len(disabledRows))
	for _, row := range disabledRows {
		disabledAt := row.DisabledAt.Time
		disabledOrgIDs = append(disabledOrgIDs, row.ID)
		result.DisabledOrganizations = append(result.DisabledOrganizations, DisabledOrganization{
			ID:         row.ID,
			Name:       row.Name,
			Slug:       row.Slug,
			DisabledAt: disabledAt,
		})
		touchOrg(row.ID).organizationDisabled = true
		if err := auditLogger.LogOrganizationDisabled(ctx, tx, audit.LogOrganizationAccessEvent{
			OrganizationID:             row.ID,
			Actor:                      actor.Principal,
			ActorDisplayName:           actor.DisplayName,
			ActorSlug:                  nil,
			OrganizationName:           row.Name,
			OrganizationSlug:           row.Slug,
			OrganizationSnapshotBefore: &audit.OrganizationAccessSnapshot{DisabledAt: nil},
			OrganizationSnapshotAfter:  &audit.OrganizationAccessSnapshot{DisabledAt: &disabledAt},
		}); err != nil {
			return result, fmt.Errorf("audit disabled organization %q: %w", row.ID, err)
		}
	}

	// 2. Soft-revoke all active API keys created by the user and remove
	// per-key policy grants on ordinary keys.
	revokedKeys, err := q.RevokeAPIKeysCreatedByUser(ctx, params.UserID)
	if err != nil {
		return result, fmt.Errorf("revoke API keys for blocked user: %w", err)
	}
	result.RevokedAPIKeyIDs = make([]uuid.UUID, 0, len(revokedKeys))
	for _, key := range revokedKeys {
		result.RevokedAPIKeyIDs = append(result.RevokedAPIKeyIDs, key.ID)
		touchOrg(key.OrganizationID).revokedAPIKeys++

		var agentCred *audit.AgentKeyCredentialMetadata
		if key.SubjectUrn.Valid {
			expiresAt := ""
			if key.ExpiresAt.Valid {
				expiresAt = key.ExpiresAt.Time.UTC().Format(time.RFC3339)
			}
			agentCred = &audit.AgentKeyCredentialMetadata{
				SubjectURN:             key.SubjectUrn.String,
				DelegatedGrants:        json.RawMessage(key.DelegatedGrants),
				DelegatedGrantsVersion: key.DelegatedGrantsVersion.Int32,
				ExpiresAt:              expiresAt,
			}
		} else {
			if err := q.DeletePrincipalGrantsForAPIKey(ctx, blockrepo.DeletePrincipalGrantsForAPIKeyParams{
				OrganizationID: key.OrganizationID,
				PrincipalUrn:   urn.NewAPIKeyPrincipal(key.ID),
			}); err != nil {
				return result, fmt.Errorf("delete policy grants for revoked API key %s: %w", key.ID, err)
			}
		}

		if err := auditLogger.LogKeyRevoke(ctx, tx, audit.LogKeyRevokeEvent{
			OrganizationID:   key.OrganizationID,
			ProjectID:        key.ProjectID,
			Actor:            actor.Principal,
			ActorDisplayName: actor.DisplayName,
			ActorSlug:        nil,
			KeyURN:           urn.NewAPIKey(key.ID),
			KeyName:          key.Name,
			Scopes:           key.Scopes,
			AgentCredential:  agentCred,
			PolicyGrants:     nil,
		}); err != nil {
			return result, fmt.Errorf("audit revoked API key %s: %w", key.ID, err)
		}
	}

	// 3. Revoke sign-ins, raise revocation cutoffs, enqueue durable user-epoch
	// and JTI deliveries, revoke user sessions (direct and agent-authorized),
	// withdraw consents, and soft-delete user-owned hosted OAuth clients.
	var jtis []string
	var deliveryOrgs []string
	var deliveryIDs []uuid.UUID
	subjectURN := urn.NewUserSubject(params.UserID)

	if len(allSignInOrgs) > 0 {
		// Mirror signins.RevokeUser lock ordering: per-org user locks in sorted
		// order, then sign-in row locks in id order, before raising cutoffs and
		// revoking sign-ins and linked/unlinked sessions.
		for _, orgID := range allSignInOrgs {
			if err := sq.LockSignInsForUser(ctx, signinsrepo.LockSignInsForUserParams{
				OrganizationID: orgID,
				UserID:         params.UserID,
			}); err != nil {
				return result, fmt.Errorf("lock sign-ins for organization %q: %w", orgID, err)
			}
		}
		if _, err := sq.LockUserSignIns(ctx, signinsrepo.LockUserSignInsParams{
			OrganizationIds: allSignInOrgs,
			UserID:          params.UserID,
		}); err != nil {
			return result, fmt.Errorf("lock user sign-ins for revocation: %w", err)
		}

		cutoffs, err := sq.RaiseRevocationCutoffs(ctx, signinsrepo.RaiseRevocationCutoffsParams{
			UserID:          params.UserID,
			OrganizationIds: allSignInOrgs,
		})
		if err != nil {
			return result, fmt.Errorf("raise sign-in revocation cutoffs for blocked user: %w", err)
		}
		for _, c := range cutoffs {
			if c.Valid && c.Time.After(result.RevocationCutoffAt) {
				result.RevocationCutoffAt = c.Time
			}
		}

		revokedSignIns, err := sq.RevokeUserSignIns(ctx, signinsrepo.RevokeUserSignInsParams{
			RevokeReason:    conv.ToPGText(signInRevokeReasonAdmin),
			OrganizationIds: allSignInOrgs,
			UserID:          params.UserID,
		})
		if err != nil {
			return result, fmt.Errorf("revoke sign-ins for blocked user: %w", err)
		}

		signInsByOrg := make(map[string]int, len(allSignInOrgs))
		workosSessionsByOrg := make(map[string][]string)
		for _, s := range revokedSignIns {
			signInsByOrg[s.OrganizationID]++
			result.RevokedSignInCount++
			if s.WorkosSessionID != "" {
				workosSessionsByOrg[s.OrganizationID] = append(workosSessionsByOrg[s.OrganizationID], s.WorkosSessionID)
			}
		}

		linkedSessions, err := sq.RevokeUserSessionsForUserSignIns(ctx, signinsrepo.RevokeUserSessionsForUserSignInsParams{
			OrganizationIds: allSignInOrgs,
			UserID:          params.UserID,
		})
		if err != nil {
			return result, fmt.Errorf("revoke user sessions for blocked user sign-ins: %w", err)
		}
		unlinkedSessions, err := sq.RevokeUnlinkedUserSessions(ctx, signinsrepo.RevokeUnlinkedUserSessionsParams{
			OrganizationIds: allSignInOrgs,
			SubjectUrn:      subjectURN,
		})
		if err != nil {
			return result, fmt.Errorf("revoke unlinked user sessions for blocked user: %w", err)
		}
		for _, u := range unlinkedSessions {
			linkedSessions = append(linkedSessions, signinsrepo.RevokeUserSessionsForUserSignInsRow(u))
		}

		sessionsByOrg := make(map[string]int, len(allSignInOrgs))
		jtisByOrg := make(map[string][]string)
		for _, sess := range linkedSessions {
			orgID := sess.OrganizationID.String
			result.RevokedUserSessionIDs = append(result.RevokedUserSessionIDs, sess.ID)
			if sess.Jti != "" {
				jtis = append(jtis, sess.Jti)
				if orgID != "" {
					jtisByOrg[orgID] = append(jtisByOrg[orgID], sess.Jti)
				}
			}
			if orgID != "" {
				sessionsByOrg[orgID]++
				if c := touchOrg(orgID); c != nil {
					c.revokedUserSessions++
				}
			}
		}

		for _, orgID := range allSignInOrgs {
			if targets := uniqueSortedStrings(jtisByOrg[orgID]); len(targets) > 0 {
				ids, err := sq.EnqueueRevocationDeliveries(ctx, signinsrepo.EnqueueRevocationDeliveriesParams{
					OrganizationID: orgID,
					Kind:           signInDeliveryKindJTI,
					Targets:        targets,
				})
				if err != nil {
					return result, fmt.Errorf("enqueue JTI revocation deliveries for organization %q: %w", orgID, err)
				}
				deliveryIDs = append(deliveryIDs, ids...)
			}
			if targets := uniqueSortedStrings(workosSessionsByOrg[orgID]); len(targets) > 0 {
				ids, err := sq.EnqueueRevocationDeliveries(ctx, signinsrepo.EnqueueRevocationDeliveriesParams{
					OrganizationID: orgID,
					Kind:           signInDeliveryKindWorkOSSession,
					Targets:        targets,
				})
				if err != nil {
					return result, fmt.Errorf("enqueue WorkOS session revocation deliveries for organization %q: %w", orgID, err)
				}
				deliveryIDs = append(deliveryIDs, ids...)
			}
		}

		epochAt := result.RevocationCutoffAt
		if epochAt.IsZero() {
			epochAt = result.BlacklistedAt
		}
		epochDeliveryID, err := sq.EnqueueUserEpochDelivery(ctx, signinsrepo.EnqueueUserEpochDeliveryParams{
			OrganizationID: allSignInOrgs[0],
			UserID:         params.UserID,
			EffectiveAt:    pgtype.Timestamptz{Time: epochAt, InfinityModifier: pgtype.Finite, Valid: true},
		})
		if err != nil {
			return result, fmt.Errorf("enqueue dashboard epoch for blocked user: %w", err)
		}
		deliveryOrgs = append(deliveryOrgs, allSignInOrgs...)
		deliveryIDs = append(deliveryIDs, epochDeliveryID)

		for _, orgID := range allSignInOrgs {
			signInCount := signInsByOrg[orgID]
			sessionCount := sessionsByOrg[orgID]
			if c := touchOrg(orgID); c != nil {
				c.revokedSignIns += signInCount
			}
			if signInCount == 0 && sessionCount == 0 {
				continue
			}
			if err := auditLogger.LogAccessMemberSignInsRevoke(ctx, tx, audit.LogAccessMemberSignInsRevokeEvent{
				OrganizationID:   orgID,
				Actor:            actor.Principal,
				ActorDisplayName: actor.DisplayName,
				MemberID:         params.UserID,
				MemberEmail:      marked.Email,
				Reason:           signInRevokeReasonAdmin,
				RevokedSignIns:   signInCount,
				RevokedSessions:  sessionCount,
			}); err != nil {
				return result, fmt.Errorf("audit sign-in revocation for organization %q: %w", orgID, err)
			}
		}
		for _, sess := range linkedSessions {
			if !sess.OrganizationID.Valid || sess.OrganizationID.String == "" {
				continue
			}
			if err := auditLogger.LogUserSessionRevoke(ctx, tx, audit.LogUserSessionRevokeEvent{
				OrganizationID:   sess.OrganizationID.String,
				ProjectID:        sess.ProjectID.UUID,
				Actor:            actor.Principal,
				ActorDisplayName: actor.DisplayName,
				ActorSlug:        nil,
				UserSessionURN:   urn.NewUserSession(sess.ID),
				Principal:        sess.SubjectUrn,
				Jti:              sess.Jti,
			}); err != nil {
				return result, fmt.Errorf("audit revoked user session %s: %w", sess.ID, err)
			}
		}
	}

	remainingSessions, err := q.RevokeRemainingUserSessionsForUser(ctx, blockrepo.RevokeRemainingUserSessionsForUserParams{
		SubjectUrn: subjectURN,
		UserID:     conv.ToPGText(params.UserID),
	})
	if err != nil {
		return result, fmt.Errorf("revoke remaining user sessions for blocked user: %w", err)
	}
	extraJTIsByOrg := make(map[string][]string)
	for _, sess := range remainingSessions {
		result.RevokedUserSessionIDs = append(result.RevokedUserSessionIDs, sess.ID)
		if sess.Jti != "" {
			jtis = append(jtis, sess.Jti)
			if sess.ResolvedOrganizationID != "" {
				extraJTIsByOrg[sess.ResolvedOrganizationID] = append(extraJTIsByOrg[sess.ResolvedOrganizationID], sess.Jti)
			}
		}
		if sess.ResolvedOrganizationID != "" {
			touchOrg(sess.ResolvedOrganizationID).revokedUserSessions++
			if err := auditLogger.LogUserSessionRevoke(ctx, tx, audit.LogUserSessionRevokeEvent{
				OrganizationID:   sess.ResolvedOrganizationID,
				ProjectID:        sess.ResolvedProjectID.UUID,
				Actor:            actor.Principal,
				ActorDisplayName: actor.DisplayName,
				ActorSlug:        nil,
				UserSessionURN:   urn.NewUserSession(sess.ID),
				Principal:        sess.SubjectUrn,
				Jti:              sess.Jti,
			}); err != nil {
				return result, fmt.Errorf("audit revoked user session %s: %w", sess.ID, err)
			}
		}
	}
	if len(extraJTIsByOrg) > 0 {
		extraOrgs := make([]string, 0, len(extraJTIsByOrg))
		for orgID := range extraJTIsByOrg {
			extraOrgs = append(extraOrgs, orgID)
		}
		slices.Sort(extraOrgs)
		for _, orgID := range extraOrgs {
			targets := uniqueSortedStrings(extraJTIsByOrg[orgID])
			ids, err := sq.EnqueueRevocationDeliveries(ctx, signinsrepo.EnqueueRevocationDeliveriesParams{
				OrganizationID: orgID,
				Kind:           signInDeliveryKindJTI,
				Targets:        targets,
			})
			if err != nil {
				return result, fmt.Errorf("enqueue remaining JTI revocation deliveries for org %q: %w", orgID, err)
			}
			deliveryOrgs = append(deliveryOrgs, orgID)
			deliveryIDs = append(deliveryIDs, ids...)
		}
	}

	revokedConsents, err := q.RevokeUserSessionConsentsForUser(ctx, subjectURN)
	if err != nil {
		return result, fmt.Errorf("revoke user session consents for blocked user: %w", err)
	}
	result.RevokedUserSessionConsentIDs = make([]uuid.UUID, 0, len(revokedConsents))
	for _, consent := range revokedConsents {
		result.RevokedUserSessionConsentIDs = append(result.RevokedUserSessionConsentIDs, consent.ID)
		if consent.ResolvedOrganizationID != "" {
			touchOrg(consent.ResolvedOrganizationID).revokedUserSessionConsents++
			if err := auditLogger.LogUserSessionConsentRevoke(ctx, tx, audit.LogUserSessionConsentRevokeEvent{
				OrganizationID:        consent.ResolvedOrganizationID,
				ProjectID:             consent.ResolvedProjectID.UUID,
				Actor:                 actor.Principal,
				ActorDisplayName:      actor.DisplayName,
				ActorSlug:             nil,
				UserSessionConsentURN: urn.NewUserSessionConsent(consent.ID),
				Principal:             subjectURN,
			}); err != nil {
				return result, fmt.Errorf("audit revoked user session consent %s: %w", consent.ID, err)
			}
		}
	}

	revokedUserSessionClients, err := q.RevokeOwnedUserSessionClientsForUser(ctx, blockrepo.RevokeOwnedUserSessionClientsForUserParams{
		DisabledOrganizationIds: disabledOrgIDs,
		SubjectUrn:              subjectURN,
		UserID:                  conv.ToPGText(params.UserID),
	})
	if err != nil {
		return result, fmt.Errorf("revoke user-owned OAuth clients for blocked user: %w", err)
	}
	result.RevokedUserSessionClientIDs = make([]uuid.UUID, 0, len(revokedUserSessionClients))
	for _, cli := range revokedUserSessionClients {
		result.RevokedUserSessionClientIDs = append(result.RevokedUserSessionClientIDs, cli.ID)
		if cli.ResolvedOrganizationID != "" {
			touchOrg(cli.ResolvedOrganizationID).revokedUserSessionClients++
			if err := auditLogger.LogUserSessionClientRevoke(ctx, tx, audit.LogUserSessionClientRevokeEvent{
				OrganizationID:       cli.ResolvedOrganizationID,
				ProjectID:            cli.ResolvedProjectID.UUID,
				Actor:                actor.Principal,
				ActorDisplayName:     actor.DisplayName,
				ActorSlug:            nil,
				UserSessionClientURN: urn.NewUserSessionClient(cli.ID),
				ClientID:             cli.ClientID,
				ClientName:           cli.ClientName,
			}); err != nil {
				return result, fmt.Errorf("audit revoked user session client %s: %w", cli.ID, err)
			}
		}
	}

	// 4. Revoke Platform MCP user-owned DCR OAuth clients, connections,
	// pending authorization grants, and sessions. Clients are locked before
	// connections and sessions to match platformmcp.PostgresOAuthStore.
	platformSubject := subjectURN.String()
	revokedPlatformClients, err := q.RevokeOwnedPlatformMCPOAuthClientsForUser(ctx, platformSubject)
	if err != nil {
		return result, fmt.Errorf("revoke Platform MCP OAuth clients for blocked user: %w", err)
	}
	result.RevokedPlatformMCPOAuthClients = make([]uuid.UUID, 0, len(revokedPlatformClients))
	revokedPlatformClientSet := make(map[uuid.UUID]struct{}, len(revokedPlatformClients))
	for _, cli := range revokedPlatformClients {
		result.RevokedPlatformMCPOAuthClients = append(result.RevokedPlatformMCPOAuthClients, cli.ID)
		revokedPlatformClientSet[cli.ID] = struct{}{}
	}

	revokedPlatformConns, err := q.RevokePlatformMCPConnectionsForUser(ctx, platformSubject)
	if err != nil {
		return result, fmt.Errorf("revoke Platform MCP connections for blocked user: %w", err)
	}
	result.RevokedPlatformMCPConnectionID = make([]uuid.UUID, 0, len(revokedPlatformConns))
	for _, conn := range revokedPlatformConns {
		result.RevokedPlatformMCPConnectionID = append(result.RevokedPlatformMCPConnectionID, conn.ID)
		c := touchOrg(conn.OrganizationID)
		if c != nil {
			c.revokedPlatformMCPConnections++
			if _, ok := revokedPlatformClientSet[conn.OauthClientID]; ok {
				c.revokedPlatformMCPOAuthClients++
			}
		}
	}

	revokedPlatformGrants, err := q.RevokePlatformMCPAuthorizationGrantsForUser(ctx, platformSubject)
	if err != nil {
		return result, fmt.Errorf("revoke Platform MCP authorization grants for blocked user: %w", err)
	}
	result.RevokedPlatformMCPGrantIDs = make([]uuid.UUID, 0, len(revokedPlatformGrants))
	for _, grant := range revokedPlatformGrants {
		result.RevokedPlatformMCPGrantIDs = append(result.RevokedPlatformMCPGrantIDs, grant.ID)
		touchOrg(grant.OrganizationID)
	}

	revokedPlatformSessions, err := q.RevokePlatformMCPSessionsForUser(ctx, platformSubject)
	if err != nil {
		return result, fmt.Errorf("revoke Platform MCP sessions for blocked user: %w", err)
	}
	result.RevokedPlatformMCPSessionIDs = make([]uuid.UUID, 0, len(revokedPlatformSessions))
	for _, sess := range revokedPlatformSessions {
		result.RevokedPlatformMCPSessionIDs = append(result.RevokedPlatformMCPSessionIDs, sess.ID)
		if c := touchOrg(sess.OrganizationID); c != nil {
			c.revokedPlatformMCPSessions++
		}
		if sess.Jti != "" {
			jtis = append(jtis, sess.Jti)
		}
	}

	// 5. Revoke Channels OAuth clients (sole-user) and grants.
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, params.UserID)
	revokedChannelsClients, err := q.RevokeOwnedChannelsOAuthClientsForUser(ctx, userPrincipal.String())
	if err != nil {
		return result, fmt.Errorf("revoke Channels OAuth clients for blocked user: %w", err)
	}
	result.RevokedChannelsOAuthClients = make([]string, 0, len(revokedChannelsClients))
	revokedChannelsClientSet := make(map[string]struct{}, len(revokedChannelsClients))
	for _, cli := range revokedChannelsClients {
		result.RevokedChannelsOAuthClients = append(result.RevokedChannelsOAuthClients, cli.ClientID)
		revokedChannelsClientSet[cli.ClientID] = struct{}{}
	}

	revokedChannelsGrants, err := q.RevokeChannelsOAuthGrantsForUser(ctx, userPrincipal.String())
	if err != nil {
		return result, fmt.Errorf("revoke Channels OAuth grants for blocked user: %w", err)
	}
	result.RevokedChannelsOAuthGrantIDs = make([]uuid.UUID, 0, len(revokedChannelsGrants))
	for _, grant := range revokedChannelsGrants {
		result.RevokedChannelsOAuthGrantIDs = append(result.RevokedChannelsOAuthGrantIDs, grant.ID)
		if c := touchOrg(grant.OrganizationID); c != nil {
			c.revokedChannelsOAuthGrants++
			if _, ok := revokedChannelsClientSet[grant.ClientID]; ok {
				c.revokedChannelsOAuthClients++
			}
		}
	}

	result.RevokedJTIs = uniqueSortedStrings(jtis)
	result.DeliveryOrganizations = uniqueSortedStrings(deliveryOrgs)
	result.DeliveryIDs = deliveryIDs

	// 6. Write per-organization member-block audit entries whenever the user
	// transitions into the blocked state or new artifacts were cascaded.
	auditedOrgs := make([]string, 0, len(countsByOrg))
	for orgID := range countsByOrg {
		auditedOrgs = append(auditedOrgs, orgID)
	}
	slices.Sort(auditedOrgs)
	for _, orgID := range auditedOrgs {
		counts := countsByOrg[orgID]
		if result.AlreadyBlacklisted && !counts.anyChanges() {
			continue
		}
		if err := auditLogger.LogAccessMemberBlock(ctx, tx, audit.LogAccessMemberBlockEvent{
			OrganizationID:                 orgID,
			Actor:                          actor.Principal,
			ActorDisplayName:               actor.DisplayName,
			MemberURN:                      userPrincipal,
			MemberEmail:                    marked.Email,
			Reason:                         params.Reason,
			OrganizationDisabled:           counts.organizationDisabled,
			RevokedAPIKeys:                 counts.revokedAPIKeys,
			RevokedSignIns:                 counts.revokedSignIns,
			RevokedUserSessions:            counts.revokedUserSessions,
			RevokedUserSessionConsents:     counts.revokedUserSessionConsents,
			RevokedUserSessionClients:      counts.revokedUserSessionClients,
			RevokedPlatformMCPConnections:  counts.revokedPlatformMCPConnections,
			RevokedPlatformMCPSessions:     counts.revokedPlatformMCPSessions,
			RevokedPlatformMCPOAuthClients: counts.revokedPlatformMCPOAuthClients,
			RevokedChannelsOAuthGrants:     counts.revokedChannelsOAuthGrants,
			RevokedChannelsOAuthClients:    counts.revokedChannelsOAuthClients,
		}); err != nil {
			return result, fmt.Errorf("audit user block for organization %q: %w", orgID, err)
		}
	}

	if logger != nil {
		logger.InfoContext(ctx, "blacklisted user and cascaded revocations",
			attr.SlogEvent(LogEventBlacklisted),
			attr.SlogUserID(result.UserID),
			attr.SlogWorkOSUserID(result.WorkOSUserID),
			attr.SlogReason(params.Reason),
		)
	}

	return result, nil
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := slices.Clone(values)
	out = slices.DeleteFunc(out, func(s string) bool { return s == "" })
	slices.Sort(out)
	return slices.Compact(out)
}
