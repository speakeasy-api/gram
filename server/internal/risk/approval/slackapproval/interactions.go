package slackapproval

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
)

const (
	// InteractionsPath is the Slack app's interactivity request URL.
	InteractionsPath = "/slack/interactions"

	// maxInteractionBytes bounds a click payload. Slack's block_actions
	// payloads carry the message they came from and stay well under this.
	maxInteractionBytes = 1 << 20 // 1 MiB

	// slackResponseHost is the only host a response_url may point at.
	slackResponseHost = "hooks.slack.com"

	// responseTimeout bounds the reply to a click.
	responseTimeout = 10 * time.Second

	// clickTimeout bounds deciding a click and replying to it. The click is
	// acknowledged first, and Slack takes replies through response_url for 30
	// minutes, so this only has to stop a stuck click from running forever.
	clickTimeout = time.Minute
)

// slackSignature is how Slack signs requests: v0=HMAC-SHA256 over
// "v0:{timestamp}:{body}", with timestamps more than 5 minutes off rejected.
var slackSignature = triggers.HMACScheme{
	NewHash:         func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
	Header:          "X-Slack-Signature",
	Encoding:        "hex",
	Prefix:          "v0=",
	Template:        "v0:{timestamp}:{body}",
	TimestampHeader: "X-Slack-Request-Timestamp",
	TimestampSkew:   5 * time.Minute,
}

// Responder replies to a click through its response_url.
type Responder interface {
	Respond(ctx context.Context, responseURL string, message ResponseMessage) error
}

// ResponseMessage is a reply to a click. It either replaces the message the
// click came from or adds an ephemeral note only the clicker sees.
type ResponseMessage struct {
	// ResponseType is ephemeral for a note only the clicker sees.
	ResponseType string `json:"response_type,omitempty"`

	// ReplaceOriginal replaces the message the click came from.
	ReplaceOriginal bool `json:"replace_original"`

	// Text is the reply's text, and the fallback when it has blocks.
	Text string `json:"text"`

	// Blocks replace the original message's blocks.
	Blocks []block `json:"blocks,omitempty"`
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxInteractionBytes+1))
	if err != nil || len(body) > maxInteractionBytes {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	if h.signingSecret == "" {
		http.Error(w, "Slack interactions are not configured", http.StatusNotFound)
		return
	}
	if err := slackSignature.Verify(body, r.Header, h.signingSecret); err != nil {
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
	message, err := repo.New(h.db).GetApprovalMessageBySlackMessage(ctx, repo.GetApprovalMessageBySlackMessageParams{
		SlackTeamID:    conv.ToPGText(payload.Team.ID),
		SlackChannelID: conv.ToPGText(payload.Container.ChannelID),
		SlackMessageTs: conv.ToPGText(payload.Container.MessageTS),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ephemeral("This approval request is no longer available.")
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "look up approval DM", attr.SlogError(err))
		return ephemeral("Something went wrong. Try again, or decide in Speakeasy.")
	}
	// The DM went to one approver, so a click is theirs only when it comes
	// from that Slack user and names the request the DM was sent for.
	if message.SlackUserID.String != payload.User.ID || message.ApprovalID.String() != value {
		h.logger.WarnContext(ctx, "reject Slack click from another user", attr.SlogSlackTeamID(payload.Team.ID), attr.SlogRiskPolicyApprovalID(message.ApprovalID.String()))
		return ephemeral("Only the approver this message was sent to can decide here.")
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
		return ephemeral("You made this call, so someone else has to approve it.")
	case errors.Is(err, approval.ErrNotApprover):
		return ephemeral("You are no longer an approver for this policy, so you cannot decide this request.")
	case errors.Is(err, approval.ErrNotFound):
		return ephemeral("This approval request no longer exists. Its policy may have been deleted.")
	case errors.Is(err, approval.ErrNotPending):
		return h.replaceWithCurrent(ctx, logger, message.OrganizationID, message.ProjectID, message.ApprovalID)
	case err != nil:
		logger.ErrorContext(ctx, "decide approval from Slack", attr.SlogError(err))
		return ephemeral("Something went wrong. Try again, or decide in Speakeasy.")
	}
	final, err := h.cards.decided(ctx, decided)
	if err != nil {
		logger.ErrorContext(ctx, "build decided approval card", attr.SlogError(err))
		return ephemeral("Your decision was recorded.")
	}
	return replace(final)
}

// replaceWithCurrent shows a click on a card that is out of date what
// happened to the request instead.
func (h *Interactions) replaceWithCurrent(ctx context.Context, logger *slog.Logger, organizationID string, projectID, approvalID uuid.UUID) ResponseMessage {
	row, err := h.approvals.Get(ctx, organizationID, projectID, approvalID)
	if err != nil {
		logger.ErrorContext(ctx, "reload approval after a late Slack click", attr.SlogError(err))
		return ephemeral("This request was already decided or has expired.")
	}
	final, err := h.cards.decided(ctx, row)
	if err != nil {
		logger.ErrorContext(ctx, "build decided approval card", attr.SlogError(err))
		return ephemeral("This request was already decided or has expired.")
	}
	return replace(final)
}

func ephemeral(text string) ResponseMessage {
	return ResponseMessage{ResponseType: "ephemeral", ReplaceOriginal: false, Text: text, Blocks: nil}
}

func replace(content card) ResponseMessage {
	return ResponseMessage{ResponseType: "", ReplaceOriginal: true, Text: content.Text, Blocks: content.Blocks}
}

// HTTPResponder posts replies to Slack's response_url.
type HTTPResponder struct {
	client *guardian.HTTPClient
}

// NewHTTPResponder builds a Responder that posts with client.
func NewHTTPResponder(client *guardian.HTTPClient) *HTTPResponder {
	return &HTTPResponder{client: client}
}

// Respond posts message to responseURL, which must be a Slack response URL:
// the payload is signed, but the URL is still never trusted to point
// anywhere else.
func (r *HTTPResponder) Respond(ctx context.Context, responseURL string, message ResponseMessage) error {
	target, err := url.Parse(responseURL)
	if err != nil || target.Scheme != "https" || target.Host != slackResponseHost {
		return errors.New("response_url is not a Slack response URL")
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode Slack reply: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, responseTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Slack reply: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Slack reply: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("send Slack reply: status %d", resp.StatusCode)
	}
	return nil
}
