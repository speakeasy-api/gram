package mcpconnectionattempts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// FixedBy is what fixed a failed attempt.
type FixedBy string

const (
	// FixedByNothing is a failure nothing fixed, or an attempt that did not fail.
	FixedByNothing FixedBy = ""

	// FixedByAdminChange is a fix after the customer changed something the attempts
	// used: a person, an API key, or one of the customer's agents or workloads.
	FixedByAdminChange FixedBy = "admin_change"

	// FixedBySystemChange is a fix after Speakeasy changed something the attempts used.
	FixedBySystemChange FixedBy = "system_change"

	// FixedByReconnect is a fix where the person signed in again.
	FixedByReconnect FixedBy = "reconnect"

	// FixedByRetry is a fix by trying again with nothing else changed.
	FixedByRetry FixedBy = "retry"

	// FixedByUnattributed is a fix of a failure only an admin can fix, where no
	// change we can see explains it and trying again could not have.
	FixedByUnattributed FixedBy = "unattributed"
)

// fixedByValues are every FixedBy value.
var fixedByValues = []FixedBy{FixedByNothing, FixedByAdminChange, FixedBySystemChange, FixedByReconnect, FixedByRetry, FixedByUnattributed}

// fixKey is the person, app and connection a success must share with the failure it
// fixes. An attempt that reached no server is told apart by its project and provider
// connection, so one provider's refresh never fixes another's.
type fixKey struct {
	organizationID string
	subjectURN     string
	appKey         string
	serverKind     string
	serverID       string
	projectID      string
	remoteClientID string
}

func fixKeyOf(a AttemptSummary) fixKey {
	key := fixKey{organizationID: a.OrganizationID, subjectURN: a.SubjectURN, appKey: a.AppKey, serverKind: a.ServerKind, serverID: a.ServerID, projectID: "", remoteClientID: ""}
	if a.ServerID == "" {
		key.projectID, key.remoteClientID = a.ProjectID.String(), a.RemoteSessionClientID
	}
	return key
}

// Successes are connected attempts grouped by person, app and server, for pairing failures.
type Successes struct {
	byKey map[fixKey][]AttemptSummary
	now   time.Time
}

// IndexSuccesses keeps the candidates that connected, as decided at now.
func IndexSuccesses(candidates []AttemptSummary, now time.Time) Successes {
	index := Successes{byKey: map[fixKey][]AttemptSummary{}, now: now}
	for _, c := range candidates {
		if c.SubjectURN != "" && c.End(now).Outcome == OutcomeConnected {
			index.byKey[fixKeyOf(c)] = append(index.byKey[fixKeyOf(c)], c)
		}
	}
	return index
}

// Fix is a failure and the success that followed it.
type Fix struct {
	// Failure is the failed attempt.
	Failure AttemptSummary

	// Failed is how the failure ended.
	Failed Ending

	// Success is the earliest connected attempt after it.
	Success AttemptSummary
}

// Next pairs a failure with the earliest success by the same person, app and connection
// that started after the failing row, within FixLookahead of the failure's start. A
// failure with no person has none.
func (s Successes) Next(failure AttemptSummary) (Fix, bool) {
	fix := Fix{Failure: failure, Failed: failure.End(s.now), Success: AttemptSummary{}} //nolint:exhaustruct // filled when found
	if failure.SubjectURN == "" {
		return fix, false
	}
	limit := failure.StartedAt.Add(FixLookahead)
	found := false
	for _, c := range s.byKey[fixKeyOf(failure)] {
		if c.AttemptID == failure.AttemptID || !c.StartedAt.After(fix.Failed.At) || !c.StartedAt.Before(limit) {
			continue
		}
		if !found || cmp.Or(c.StartedAt.Compare(fix.Success.StartedAt), slices.Compare(c.AttemptID[:], fix.Success.AttemptID[:])) < 0 {
			fix.Success, found = c, true
		}
	}
	return fix, found
}

// NextSuccess pairs a failure with the earliest success among candidates, as Successes.Next does.
func NextSuccess(failure AttemptSummary, candidates []AttemptSummary, now time.Time) (Fix, bool) {
	return IndexSuccesses(candidates, now).Next(failure)
}

// Targets is everything a change could have fixed: what either attempt touched.
func (f Fix) Targets() []string {
	ids := slices.Concat(f.Failure.Touched, f.Success.Touched)
	slices.Sort(ids)
	return slices.Compact(ids)
}

// FixedBy names what fixed the failure, given the audit changes in its organization.
// Only changes after the failing row and before the success count. A change on
// something either attempt touched counts, and so does an admin's change anywhere in
// the organization that can fix the failure's reason. An admin's change wins over
// Speakeasy's, and a change wins over the person acting. Without a change, a failure
// only an admin can fix is unattributed unless the person can get past it alone;
// anything else is a reconnect when they signed in again, or a retry.
func (f Fix) FixedBy(changes []Change) FixedBy {
	targets := f.Targets()
	fix := adminFixes[f.Failed.Reason]
	system := false
	for _, c := range changes {
		if !c.At.After(f.Failed.At) || !c.At.Before(f.Success.StartedAt) {
			continue
		}
		if _, ok := slices.BinarySearch(targets, c.SubjectID); ok {
			if c.ByAdmin {
				return FixedByAdminChange
			}
			system = true
			continue
		}
		if c.ByAdmin && slices.ContainsFunc(fix.changes, func(a AuditAction) bool { return a.Action == c.Action }) {
			return FixedByAdminChange
		}
	}
	switch {
	case system:
		return FixedBySystemChange
	case f.adminOnly(fix):
		return FixedByUnattributed
	case f.Success.SignedIn:
		return FixedByReconnect
	default:
		return FixedByRetry
	}
}

// adminOnly reports whether only an admin could have fixed the failure, so neither a
// retry nor this success's sign-in explains it.
func (f Fix) adminOnly(fix adminFix) bool {
	meaning, ok := MeaningOf(f.Failed.Reason)
	if !ok || meaning.WhoFixes != WhoAdmin {
		return false
	}
	switch fix.personFix {
	case personRetries:
		return false
	case personSignsIn:
		return !f.Success.SignedIn
	default:
		return true
	}
}

// AuditActions are the organization-wide admin changes that can fix any of the fixes.
func AuditActions(fixes []Fix) []AuditAction {
	var actions []AuditAction
	for _, f := range fixes {
		for _, a := range adminFixes[f.Failed.Reason].changes {
			if !slices.Contains(actions, a) {
				actions = append(actions, a)
			}
		}
	}
	return actions
}

// AuditAction is an audit subject type and one of its actions.
type AuditAction struct {
	SubjectType string
	Action      string
}

func auditAction(subjectType string, action audit.Action) AuditAction {
	return AuditAction{SubjectType: subjectType, Action: string(action)}
}

// personFix is how the person can get past an admin-fixable failure alone.
type personFix int

const (
	personCannot personFix = iota
	personRetries
	personSignsIn
)

// adminFix is how an admin-fixable reason gets fixed beyond what the attempts touched:
// the organization-wide changes that can fix it, and whether the person can alone.
type adminFix struct {
	changes   []AuditAction
	personFix personFix
}

var (
	accessChanges = []AuditAction{
		auditAction("access_role", audit.ActionAccessRoleCreate),
		auditAction("access_role", audit.ActionAccessRoleUpdate),
		auditAction("access_member", audit.ActionAccessMemberRoleUpdate),
		auditAction("access_grant", audit.ActionAccessGrantCreateTemporary),
		auditAction("access_grant", audit.ActionAccessGrantRenewTemporary),
		auditAction("access_challenge", audit.ActionAccessChallengeResolve),
		auditAction("directory_role_mapping", audit.ActionDirectoryRoleMappingSet),
	}
	membershipChanges = []AuditAction{
		auditAction("organization_invitation", audit.ActionOrganizationInviteCreate),
		auditAction("organization_invitation", audit.ActionOrganizationInviteRoleUpdate),
	}
	renewedSignIn = adminFix{changes: nil, personFix: personSignsIn}
)

// adminFixes maps admin-fixable reasons to how they get fixed. A reason fixed only on
// what the attempts touched, such as a server's or auth client's setup, is left out.
var adminFixes = map[string]adminFix{
	CodeNoServerAccess.String():        {changes: accessChanges, personFix: personCannot},
	CodeToolPermissionDenied.String():  {changes: accessChanges, personFix: personCannot},
	CodeResourceNotAvailable.String():  {changes: accessChanges, personFix: personCannot},
	CodeApprovalNeedsAdmin.String():    {changes: accessChanges, personFix: personCannot},
	CodeNotOrgMember.String():          {changes: membershipChanges, personFix: personCannot},
	CodeSubjectNotProvisioned.String(): {changes: membershipChanges, personFix: personCannot},
	CodeAIToolBlocked.String(): {changes: []AuditAction{
		auditAction("ai_tool_decision", audit.ActionAIToolDecisionSet),
	}, personFix: personCannot},
	CodePolicyBlocked.String(): {changes: []AuditAction{
		auditAction("risk_policy", audit.ActionRiskPolicyUpdate),
		auditAction("risk_policy", audit.ActionRiskPolicyDelete),
		auditAction("risk_policy", audit.ActionRiskPolicyApprovalApprove),
		auditAction("risk_policy", audit.ActionRiskPolicyBypassRequestApprove),
		auditAction("risk_exclusion", audit.ActionRiskExclusionCreate),
		auditAction("risk_exclusion", audit.ActionRiskExclusionUpdate),
	}, personFix: personCannot},
	CodeMCPToolCallsPaused.String(): {changes: []AuditAction{
		auditAction("killswitch_prescription", audit.ActionKillswitchChange),
		auditAction("killswitch_prescription", audit.ActionKillswitchDeactivate),
		auditAction("killswitch_prescription", audit.ActionKillswitchExpire),
	}, personFix: personCannot},
	CodeUsageLimitReached.String(): {changes: []AuditAction{
		auditAction("organization", audit.ActionOrganizationPaygActivated),
		auditAction("organization", audit.ActionOrganizationAccountTypeChanged),
		auditAction("organization", audit.ActionOrganizationEnterpriseTrialStarted),
		auditAction("organization", audit.ActionOrganizationEnterpriseTrialExtended),
		auditAction("organization", audit.ActionOrganizationEnterpriseTrialRearmed),
		auditAction("organization", audit.ActionOrganizationEnterpriseTrialConverted),
	}, personFix: personCannot},
	CodeSubjectNotAdmitted.String(): {changes: []AuditAction{
		auditAction(audit.SubjectTypeWorkloadAdmission, audit.ActionWorkloadAdmissionAdmit),
		auditAction(audit.SubjectTypeWorkloadAdmission, audit.ActionWorkloadAdmissionUpdate),
		auditAction("workload_issuer", audit.ActionWorkloadIssuerCreate),
		auditAction("workload_issuer", audit.ActionWorkloadIssuerUpdate),
	}, personFix: personCannot},
	CodeCIMDAdmissionDenied.String(): {changes: []AuditAction{
		auditAction("user_session_issuer_cimd_client", audit.ActionUserSessionIssuerCimdClientAdd),
	}, personFix: personCannot},
	CodeCustomDomainNotReady.String(): {changes: []AuditAction{
		auditAction("custom_domain", audit.ActionCustomDomainsCreate),
		auditAction("custom_domain", audit.ActionCustomDomainsUpdate),
	}, personFix: personCannot},
	CodeTunnelAtCapacity.String():     {changes: nil, personFix: personRetries},
	CodeUnrenewableToken.String():     renewedSignIn,
	CodeUpstreamEMARefused.String():   renewedSignIn,
	CodeRefreshRejected.String():      renewedSignIn,
	CodeOfflineNotRequested.String():  renewedSignIn,
	CodeOfflineUnsupported.String():   renewedSignIn,
	CodeRenewalMisconfigured.String(): renewedSignIn,
	CodeSignInRevokedByAdmin.String(): renewedSignIn,
}

// Change is one audit entry on something an attempt touched, or one of AuditActions.
type Change struct {
	// SubjectID is the audited subject.
	SubjectID string

	// Action is the audited action.
	Action string

	// ByAdmin says the customer made the change, not Speakeasy.
	ByAdmin bool

	// At is when the change was made.
	At time.Time
}

// readOnlyWords mark audit actions that read rather than change. An action counts as a
// change unless a word after the colon, split on - and _, is listed, so a new write
// action counts without being added here.
var readOnlyWords = []string{"read", "view", "list", "get", "access", "export", "reveal", "unmask", "observe", "call"}

// IsConfigChange reports whether an audit action changed configuration rather than read it.
func IsConfigChange(action string) bool {
	_, tail, ok := strings.Cut(action, ":")
	if !ok || tail == "" {
		return false
	}
	return !slices.ContainsFunc(strings.FieldsFunc(tail, func(r rune) bool { return r == '-' || r == '_' }), func(word string) bool {
		return slices.Contains(readOnlyWords, word)
	})
}

// IsAdminActor reports whether an audit actor is the customer: a person, an API key,
// or the customer's agent or workload, unless Speakeasy staff acted. user:system and
// system actors are Speakeasy.
func IsAdminActor(actorType, actorID string, internal bool) bool {
	if internal {
		return false
	}
	switch actorType {
	case string(urn.PrincipalTypeUser):
		return actorID != "system"
	case string(urn.PrincipalTypeAPIKey), string(urn.PrincipalTypeAgent), string(urn.PrincipalTypeWorkload):
		return true
	default:
		return false
	}
}

// ListChanges reads one organization's configuration changes between two times, on
// the given subjects or with the given actions.
func ListChanges(ctx context.Context, db auditrepo.DBTX, organizationID string, subjects []string, actions []AuditAction, after, before time.Time) ([]Change, error) {
	if len(subjects) == 0 && len(actions) == 0 {
		return nil, nil
	}
	subjectTypes, actionNames := make([]string, 0, len(actions)), make([]string, 0, len(actions))
	for _, a := range actions {
		subjectTypes, actionNames = append(subjectTypes, a.SubjectType), append(actionNames, a.Action)
	}
	entries, err := auditrepo.New(db).ListAuditActorsOnSubjects(ctx, auditrepo.ListAuditActorsOnSubjectsParams{
		OrganizationID: organizationID,
		SubjectIds:     subjects,
		SubjectTypes:   subjectTypes,
		Actions:        actionNames,
		After:          conv.ToPGTimestamptz(after),
		Before:         conv.ToPGTimestamptz(before),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	changes := make([]Change, 0, len(entries))
	for _, entry := range entries {
		if !IsConfigChange(entry.Action) {
			continue
		}
		changes = append(changes, Change{
			SubjectID: entry.SubjectID,
			Action:    entry.Action,
			ByAdmin:   IsAdminActor(entry.ActorType, entry.ActorID, entry.IsInternal),
			At:        entry.CreatedAt.Time,
		})
	}
	return changes, nil
}
