package slackdirectoryconnections

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func readMember(ctx context.Context, q *repo.Queries, org string, id uuid.UUID) (*gen.SlackDirectoryMember, error) {
	rows, err := q.ListSlackDirectoryMembers(ctx, repo.ListSlackDirectoryMembersParams{MappedUserID: "", OrganizationID: org, MemberID: uuid.NullUUID{UUID: id, Valid: true}, ConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Search: "", MappingStatus: "", PageSize: 1})
	if err != nil {
		return nil, fmt.Errorf("read Slack member: %w", err)
	}
	if len(rows) == 0 {
		return nil, pgx.ErrNoRows
	}
	return mv.BuildSlackDirectoryMemberView(rows[0]), nil
}

func (s *Service) GetMember(ctx context.Context, p *gen.GetMemberPayload) (*gen.SlackDirectoryMember, error) {
	ac, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	result, err := readMember(ctx, repo.New(s.db), ac.ActiveOrganizationID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not read Slack member").LogError(ctx, s.logger)
	}
	return result, nil
}

func (s *Service) SetMapping(ctx context.Context, p *gen.SetMappingPayload) (*gen.SlackDirectoryMember, error) {
	ac, err := s.authorizeMutation(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(p.ID)
	if err != nil || p.MappingRevision < 0 || p.ObservationToken == "" || (p.UserID != nil && *p.UserID == "") {
		return nil, oops.C(oops.CodeBadRequest)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin Slack mapping").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	member, err := q.LockSlackDirectoryMembership(ctx, repo.LockSlackDirectoryMembershipParams{OrganizationID: ac.ActiveOrganizationID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock Slack member").LogError(ctx, s.logger)
	}
	before, err := readMember(ctx, q, ac.ActiveOrganizationID, id)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read Slack mapping").LogError(ctx, s.logger)
	}
	if member.MappingRevision != p.MappingRevision || before.ObservationToken != p.ObservationToken {
		return nil, oops.E(oops.CodeConflict, nil, "This Slack member changed. Reload the member and review your selection again.")
	}
	action := audit.ActionSlackIdentityMappingUnmap
	if p.UserID != nil {
		if member.MemberType == "bot" {
			return nil, oops.E(oops.CodeBadRequest, nil, "Bots and apps cannot be assigned to a person.")
		}
		if _, err := orgrepo.New(tx).LockActiveOrganizationUser(ctx, orgrepo.LockActiveOrganizationUserParams{OrganizationID: ac.ActiveOrganizationID, UserID: conv.ToPGText(*p.UserID)}); errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeBadRequest, nil, "Choose an active person in this organization.")
		} else if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "validate Slack mapping person").LogError(ctx, s.logger)
		}
		action = audit.ActionSlackIdentityMappingConfirm
		if before.Mapping != nil {
			action = audit.ActionSlackIdentityMappingReassign
			if before.Mapping.UserID == *p.UserID {
				action = audit.ActionSlackIdentityMappingReconfirm
			}
		}
	} else if before.Mapping == nil {
		return nil, oops.E(oops.CodeConflict, nil, "This Slack member is already unmapped. Reload the member.")
	}
	if action != audit.ActionSlackIdentityMappingReconfirm {
		if err := q.RevokeSlackIdentityMapping(ctx, repo.RevokeSlackIdentityMappingParams{OrganizationID: ac.ActiveOrganizationID, SlackTeamID: member.SlackTeamID, SlackUserID: member.SlackUserID}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "revoke Slack mapping").LogError(ctx, s.logger)
		}
		if p.UserID != nil {
			if err := q.ConfirmSlackIdentityMapping(ctx, repo.ConfirmSlackIdentityMappingParams{OrganizationID: ac.ActiveOrganizationID, SlackTeamID: member.SlackTeamID, SlackUserID: member.SlackUserID, UserID: *p.UserID}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "confirm Slack mapping").LogError(ctx, s.logger)
			}
		}
	}
	if err := q.AdvanceSlackMappingRevision(ctx, repo.AdvanceSlackMappingRevisionParams{OrganizationID: ac.ActiveOrganizationID, ID: id}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "advance Slack mapping revision").LogError(ctx, s.logger)
	}
	after, err := readMember(ctx, q, ac.ActiveOrganizationID, id)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read confirmed Slack mapping").LogError(ctx, s.logger)
	}
	if err := s.audit.LogSlackIdentityMapping(ctx, tx, action, audit.LogSlackIdentityMappingEvent{OrganizationID: ac.ActiveOrganizationID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, MembershipURN: urn.NewSlackDirectoryMembership(id), MembershipSnapshotBefore: before, MembershipSnapshotAfter: after}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit Slack mapping").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit Slack mapping").LogError(ctx, s.logger)
	}
	return after, nil
}

// autoMapByEmail maps members whose Slack email matches exactly one active person.
// Members with any mapping history are skipped, so an administrator's removal sticks.
func autoMapByEmail(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, org, team string) error {
	q := repo.New(tx)
	candidates, err := q.ListSlackEmailMappingCandidates(ctx, repo.ListSlackEmailMappingCandidatesParams{OrganizationID: org, SlackTeamID: team})
	if err != nil {
		return fmt.Errorf("list Slack email mapping candidates: %w", err)
	}
	for _, candidate := range candidates {
		member, err := q.LockSlackDirectoryMembership(ctx, repo.LockSlackDirectoryMembershipParams{OrganizationID: org, ID: candidate.MembershipID})
		if err != nil {
			return fmt.Errorf("lock Slack member for email mapping: %w", err)
		}
		before, err := readMember(ctx, q, org, candidate.MembershipID)
		if err != nil {
			return err
		}
		if err := q.ConfirmSlackIdentityMapping(ctx, repo.ConfirmSlackIdentityMappingParams{OrganizationID: org, SlackTeamID: member.SlackTeamID, SlackUserID: member.SlackUserID, UserID: candidate.UserID}); err != nil {
			return fmt.Errorf("map Slack member by email: %w", err)
		}
		if err := q.AdvanceSlackMappingRevision(ctx, repo.AdvanceSlackMappingRevisionParams{OrganizationID: org, ID: candidate.MembershipID}); err != nil {
			return fmt.Errorf("advance Slack mapping revision: %w", err)
		}
		after, err := readMember(ctx, q, org, candidate.MembershipID)
		if err != nil {
			return err
		}
		if err := auditLogger.LogSlackIdentityMapping(ctx, tx, audit.ActionSlackIdentityMappingConfirm, audit.LogSlackIdentityMappingEvent{OrganizationID: org, Actor: urn.NewSystemPrincipal("slack-directory-email-match"), ActorDisplayName: nil, MembershipURN: urn.NewSlackDirectoryMembership(candidate.MembershipID), MembershipSnapshotBefore: before, MembershipSnapshotAfter: after}); err != nil {
			return fmt.Errorf("audit Slack email mapping: %w", err)
		}
	}
	return nil
}

// forgetMappings removes every mapping for a disconnected workspace, auditing each live one as an unmap by the admin who disconnected it.
func forgetMappings(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, ac *contextvalues.AuthContext, team string) error {
	q := repo.New(tx)
	// Lock every membership first so a concurrent SetMapping either commits before this
	// reads mappings (and is removed and audited) or waits and then finds the member gone.
	if _, err := q.LockSlackDirectoryMembershipsForPublication(ctx, repo.LockSlackDirectoryMembershipsForPublicationParams{OrganizationID: ac.ActiveOrganizationID, SlackTeamID: team}); err != nil {
		return fmt.Errorf("lock Slack members to forget: %w", err)
	}
	ids, err := q.ListMappedSlackMembershipIDs(ctx, repo.ListMappedSlackMembershipIDsParams{OrganizationID: ac.ActiveOrganizationID, SlackTeamID: team})
	if err != nil {
		return fmt.Errorf("list Slack mappings to forget: %w", err)
	}
	for _, id := range ids {
		before, err := readMember(ctx, q, ac.ActiveOrganizationID, id)
		if err != nil {
			return err
		}
		member, err := q.LockSlackDirectoryMembership(ctx, repo.LockSlackDirectoryMembershipParams{OrganizationID: ac.ActiveOrganizationID, ID: id})
		if err != nil {
			return fmt.Errorf("lock Slack member to forget: %w", err)
		}
		if err := q.RevokeSlackIdentityMapping(ctx, repo.RevokeSlackIdentityMappingParams{OrganizationID: ac.ActiveOrganizationID, SlackTeamID: member.SlackTeamID, SlackUserID: member.SlackUserID}); err != nil {
			return fmt.Errorf("revoke Slack mapping on disconnect: %w", err)
		}
		after, err := readMember(ctx, q, ac.ActiveOrganizationID, id)
		if err != nil {
			return err
		}
		if err := auditLogger.LogSlackIdentityMapping(ctx, tx, audit.ActionSlackIdentityMappingUnmap, audit.LogSlackIdentityMappingEvent{OrganizationID: ac.ActiveOrganizationID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, MembershipURN: urn.NewSlackDirectoryMembership(id), MembershipSnapshotBefore: before, MembershipSnapshotAfter: after}); err != nil {
			return fmt.Errorf("audit Slack mapping removal on disconnect: %w", err)
		}
	}
	if err := q.DeleteSlackIdentityMappings(ctx, repo.DeleteSlackIdentityMappingsParams{OrganizationID: ac.ActiveOrganizationID, SlackTeamID: team}); err != nil {
		return fmt.Errorf("delete Slack mappings on disconnect: %w", err)
	}
	return nil
}
