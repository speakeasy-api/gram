package slackapproval

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdm"
)

// InteractionsPath is the Slack app's interactivity request URL.
const InteractionsPath = slackdm.InteractionsPath

// clickTimeout bounds deciding a click and replying to it. The click is
// acknowledged first, and Slack takes replies through response_url for 30
// minutes, so this only has to stop a stuck click from running forever.
const clickTimeout = time.Minute

// Responder replies to a click through its response_url.
type Responder = slackdm.Responder

// ResponseMessage is a reply to a click.
type ResponseMessage = slackdm.ResponseMessage

// NewHTTPResponder builds a Responder that posts to Slack's response_url
// with client.
func NewHTTPResponder(client *guardian.HTTPClient) *slackdm.HTTPResponder {
	return slackdm.NewHTTPResponder(client)
}

// Interactions handles clicks on approval request DMs.
type Interactions struct {
	logger        *slog.Logger
	db            *pgxpool.Pool
	approvals     *approval.Service
	cards         *Cards
	responder     Responder
	signingSecret string
}

// NewInteractions builds the click handler. signingSecret is the Slack app's
// signing secret; every request must carry its signature.
func NewInteractions(logger *slog.Logger, cards *Cards, responder Responder, signingSecret string) *Interactions {
	return &Interactions{
		logger:        logger.With(attr.SlogComponent("risk_approval_slack_interactions")),
		db:            cards.db,
		approvals:     cards.approvals,
		cards:         cards,
		responder:     responder,
		signingSecret: signingSecret,
	}
}

// interactionPayload is the part of a Slack block_actions payload a click on
// an approval DM uses.
type interactionPayload struct {
	// Type is block_actions for a button click.
	Type string `json:"type"`

	// Team is the workspace the click came from.
	Team struct {
		ID string `json:"id"`
	} `json:"team"`

	// User is who clicked.
	User struct {
		ID string `json:"id"`
	} `json:"user"`

	// Container is the message the button sits on.
	Container struct {
		ChannelID string `json:"channel_id"`
		MessageTS string `json:"message_ts"`
	} `json:"container"`

	// ResponseURL takes replies to the click for 30 minutes.
	ResponseURL string `json:"response_url"`

	// Actions are the clicked elements; a button click has one.
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

// ServeHTTP verifies Slack's signature, decides the request for the approver
// the DM was sent to, and replies to the click. Everything after the
// signature check answers 200, since Slack shows any other status to the
// clicker as a generic failure; refusals go back as ephemeral notes. A click
// is acknowledged before it is decided: Slack wants the 200 within 3 seconds,
// and the outcome goes back through response_url.
func (h *Interactions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, slackdm.MaxInteractionBytes+1))
	if err != nil || len(body) > slackdm.MaxInteractionBytes {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	if h.signingSecret == "" {
		http.Error(w, "Slack interactions are not configured", http.StatusNotFound)
		return
	}
	if err := slackdm.VerifySignature(body, r.Header, h.signingSecret); err != nil {
		h.logger.WarnContext(ctx, "reject unsigned Slack interaction", attr.SlogError(err))
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	var payload interactionPayload
	if err := json.Unmarshal([]byte(form.Get("payload")), &payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	// An empty body with its length declared makes the 200 a whole response,
	// so Slack has its answer once it is flushed, before the decision runs.
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
	if payload.Type != "block_actions" || len(payload.Actions) != 1 {
		return
	}
	action := payload.Actions[0]
	if action.ActionID != ActionApprove && action.ActionID != ActionDeny {
		return
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		h.logger.WarnContext(ctx, "acknowledge Slack interaction early", attr.SlogError(err))
	}
	// Slack may close the connection once it has the 200; the decision and
	// the reply still have to finish.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), clickTimeout)
	defer cancel()
	reply := h.handleClick(ctx, payload, action.ActionID == ActionApprove, action.Value)
	if err := h.responder.Respond(ctx, payload.ResponseURL, reply); err != nil {
		h.logger.WarnContext(ctx, "reply to Slack interaction", attr.SlogSlackTeamID(payload.Team.ID), attr.SlogError(err))
	}
}

func (h *Interactions) handleClick(ctx context.Context, payload interactionPayload, approve bool, value string) ResponseMessage {
	found, err := repo.New(h.db).GetApprovalMessageBySlackMessage(ctx, repo.GetApprovalMessageBySlackMessageParams{
		SlackTeamID:    conv.ToPGText(payload.Team.ID),
		SlackChannelID: conv.ToPGText(payload.Container.ChannelID),
		SlackMessageTs: conv.ToPGText(payload.Container.MessageTS),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return slackdm.Ephemeral("This approval request is no longer available.")
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "look up approval DM", attr.SlogError(err))
		return slackdm.Ephemeral("Something went wrong. Try again, or decide in Speakeasy.")
	}
	message := found.RiskPolicyApprovalMessage
	// The DM went to one approver, so a click is theirs only when it comes
	// from that Slack user and names the request the DM was sent for.
	if message.SlackUserID.String != payload.User.ID || message.ApprovalID.String() != value {
		h.logger.WarnContext(ctx, "reject Slack click from another user", attr.SlogSlackTeamID(payload.Team.ID), attr.SlogRiskPolicyApprovalID(message.ApprovalID.String()))
		return slackdm.Ephemeral("Only the approver this message was sent to can decide here.")
	}
	logger := h.logger.With(attr.SlogOrganizationID(message.OrganizationID), attr.SlogRiskPolicyApprovalID(message.ApprovalID.String()), attr.SlogUserID(message.RecipientUserID))

	decided, err := h.approvals.Decide(ctx, approval.DecideInput{
		OrganizationID:     message.OrganizationID,
		ProjectID:          message.ProjectID,
		ApprovalID:         message.ApprovalID,
		Approve:            approve,
		DeciderUserID:      message.RecipientUserID,
		DeciderDisplayName: h.cards.memberEmail(ctx, message.OrganizationID, message.RecipientUserID),
		Via:                approval.DecidedViaSlack,
		Note:               "",
	})
	switch {
	case errors.Is(err, approval.ErrSelfApproval):
		return slackdm.Ephemeral("You made this call, so someone else has to approve it.")
	case errors.Is(err, approval.ErrNotApprover):
		return slackdm.Ephemeral("You are no longer an approver for this policy, so you cannot decide this request.")
	case errors.Is(err, approval.ErrNotFound):
		return slackdm.Ephemeral("This approval request no longer exists. Its policy may have been deleted.")
	case errors.Is(err, approval.ErrNotPending):
		return h.replaceWithCurrent(ctx, logger, message.OrganizationID, message.ProjectID, message.ApprovalID)
	case err != nil:
		logger.ErrorContext(ctx, "decide approval from Slack", attr.SlogError(err))
		return slackdm.Ephemeral("Something went wrong. Try again, or decide in Speakeasy.")
	}
	final, err := h.cards.decided(ctx, decided)
	if err != nil {
		logger.ErrorContext(ctx, "build decided approval card", attr.SlogError(err))
		return slackdm.Ephemeral("Your decision was recorded.")
	}
	return slackdm.Replace(final)
}

// replaceWithCurrent shows a click on a card that is out of date what
// happened to the request instead.
func (h *Interactions) replaceWithCurrent(ctx context.Context, logger *slog.Logger, organizationID string, projectID, approvalID uuid.UUID) ResponseMessage {
	row, err := h.approvals.Get(ctx, organizationID, projectID, approvalID)
	if err != nil {
		logger.ErrorContext(ctx, "reload approval after a late Slack click", attr.SlogError(err))
		return slackdm.Ephemeral("This request was already decided or has expired.")
	}
	final, err := h.cards.decided(ctx, row)
	if err != nil {
		logger.ErrorContext(ctx, "build decided approval card", attr.SlogError(err))
		return slackdm.Ephemeral("This request was already decided or has expired.")
	}
	return slackdm.Replace(final)
}
