package slackapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
)

const (
	// homeClickApprove and homeClickDeny are the Home tab's tool approval
	// buttons. Their value is the request's project ID and ID, joined by ":".
	homeClickApprove = "home_approval_approve"
	homeClickDeny    = "home_approval_deny"

	// maxHomeInteractionBytes bounds a click payload. Home tab payloads carry
	// the whole published view.
	maxHomeInteractionBytes = 1 << 20 // 1 MiB

	// homeClickTimeout bounds deciding a click and republishing the Home tab
	// after the click is acknowledged.
	homeClickTimeout = time.Minute
)

// HomeDecider records a decision on a tool approval request. The
// approval.Service checks the decider is still an approver.
type HomeDecider interface {
	Decide(ctx context.Context, input approval.DecideInput) (approvalrepo.RiskPolicyApproval, error)
}

// HomeInteractions handles clicks on the Home tab's approval buttons and hands
// every other interaction to next, the existing interactivity handler, with
// its body intact.
type HomeInteractions struct {
	events  *Events
	decider HomeDecider
	next    http.Handler
}

// NewHomeInteractions wraps next, which keeps serving every interaction that
// is not a Home tab approval click.
func NewHomeInteractions(events *Events, decider HomeDecider, next http.Handler) *HomeInteractions {
	return &HomeInteractions{events: events, decider: decider, next: next}
}

// homeClick is the part of a block_actions payload a Home tab click uses.
type homeClick struct {
	Type string `json:"type"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	View struct {
		Type string `json:"type"`
		// PrivateMetadata is the organization the view was published for.
		PrivateMetadata string `json:"private_metadata"`
	} `json:"view"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

func (h *HomeInteractions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHomeInteractionBytes+1))
	if err != nil || len(body) > maxHomeInteractionBytes {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	click, ok := parseHomeClick(body)
	if !ok {
		r.Body = io.NopCloser(bytes.NewReader(body))
		h.next.ServeHTTP(w, r)
		return
	}
	if h.events.secret == "" || triggers.SlackSignature.Verify(body, r.Header, h.events.secret) != nil {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	// Acknowledge before deciding: Slack wants the 200 within 3 seconds, and
	// the outcome goes back by republishing the Home tab.
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
	if err := http.NewResponseController(w).Flush(); err != nil {
		h.events.logger.WarnContext(r.Context(), "acknowledge Speakeasy Slack app home click early", attr.SlogError(err))
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), homeClickTimeout)
	defer cancel()
	h.handle(ctx, click)
}

// parseHomeClick reports whether body is a click on a Home tab approval
// button. Anything else, including a payload it cannot read, belongs to next.
func parseHomeClick(body []byte) (homeClick, bool) {
	var click homeClick
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return click, false
	}
	if err := json.Unmarshal([]byte(form.Get("payload")), &click); err != nil {
		return click, false
	}
	if click.Type != "block_actions" || click.View.Type != "home" || len(click.Actions) != 1 {
		return click, false
	}
	switch click.Actions[0].ActionID {
	case homeClickApprove, homeClickDeny:
		return click, true
	default:
		return click, false
	}
}

// handle decides the clicked request as the clicker's linked Speakeasy user
// and republishes their Home tab with the outcome. The organization comes
// from the signed view's private_metadata and is re-resolved against the
// workspace's linkage and membership; the button's IDs only name the
// request, and approval.Service re-checks the clicker is its approver.
func (h *HomeInteractions) handle(ctx context.Context, click homeClick) {
	e := h.events
	if e.home == nil || e.poster == nil || h.decider == nil {
		return
	}
	teamID, slackUserID := click.Team.ID, click.User.ID
	action := click.Actions[0]
	projectID, approvalID, ok := parseHomeApprovalValue(action.Value)
	organizationID := click.View.PrivateMetadata
	if !ok || organizationID == "" {
		e.refreshHome(ctx, teamID, slackUserID, "", "That request couldn't be read. Try again, or decide in Speakeasy.")
		return
	}

	viewer, found, err := e.resolveHomeViewer(ctx, teamID, slackUserID, organizationID)
	if err != nil {
		e.logger.ErrorContext(ctx, "resolve Speakeasy Slack app home click", attr.SlogSlackTeamID(teamID), attr.SlogError(err))
		e.refreshHome(ctx, teamID, slackUserID, "", "Something went wrong. Try again, or decide in Speakeasy.")
		return
	}
	if !found || viewer.userID == "" {
		e.refreshHome(ctx, teamID, slackUserID, "", "You can't decide this request: your Slack account isn't linked to a member of its organization.")
		return
	}

	// The Home tab may predate a rollout change; decide only while tool
	// approvals are still on for the organization.
	if !e.home.toolApprovalsEnabled(ctx, viewer.workspace.OrganizationID) {
		e.refreshHome(ctx, teamID, slackUserID, viewer.workspace.OrganizationID, "Tool approvals aren't available for your organization right now, so this request wasn't decided.")
		return
	}

	approve := action.ActionID == homeClickApprove
	decided, err := h.decider.Decide(ctx, approval.DecideInput{
		OrganizationID:     viewer.workspace.OrganizationID,
		ProjectID:          projectID,
		ApprovalID:         approvalID,
		Approve:            approve,
		DeciderUserID:      viewer.userID,
		DeciderDisplayName: e.home.memberEmail(ctx, viewer.workspace.OrganizationID, viewer.userID),
		Via:                approval.DecidedViaSlack,
		Note:               "",
	})
	notice := homeDecisionNotice(decided, approve, err)
	if err != nil && !isHomeDecisionRefusal(err) {
		e.logger.ErrorContext(ctx, "decide approval from Speakeasy Slack app home",
			attr.SlogOrganizationID(viewer.workspace.OrganizationID), attr.SlogRiskPolicyApprovalID(approvalID.String()), attr.SlogError(err))
	}
	e.refreshHome(ctx, teamID, slackUserID, viewer.workspace.OrganizationID, notice)
}

func parseHomeApprovalValue(value string) (uuid.UUID, uuid.UUID, bool) {
	rawProject, rawApproval, ok := strings.Cut(value, ":")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	projectID, err := uuid.Parse(rawProject)
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	approvalID, err := uuid.Parse(rawApproval)
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return projectID, approvalID, true
}

func isHomeDecisionRefusal(err error) bool {
	return errors.Is(err, approval.ErrSelfApproval) || errors.Is(err, approval.ErrNotApprover) ||
		errors.Is(err, approval.ErrNotFound) || errors.Is(err, approval.ErrNotPending)
}

func homeDecisionNotice(decided approvalrepo.RiskPolicyApproval, approve bool, err error) string {
	switch {
	case errors.Is(err, approval.ErrSelfApproval):
		return "You made this call, so someone else has to approve it."
	case errors.Is(err, approval.ErrNotApprover):
		return "You are not an approver for this policy, so you cannot decide this request."
	case errors.Is(err, approval.ErrNotFound):
		return "This approval request no longer exists. Its policy may have been deleted."
	case errors.Is(err, approval.ErrNotPending):
		return "This request was already decided or has expired."
	case err != nil:
		return "Something went wrong. Try again, or decide in Speakeasy."
	case approve:
		return fmt.Sprintf("Approved the call to %s.", decided.ToolName)
	default:
		return fmt.Sprintf("Denied the call to %s.", decided.ToolName)
	}
}
