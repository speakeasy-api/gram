package slackrequests

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

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/slackdm"
)

// clickTimeout bounds deciding a click and replying to it. The click is
// acknowledged first, and Slack takes replies through response_url for 30
// minutes, so this only has to stop a stuck click from running forever.
const clickTimeout = time.Minute

// Decider decides access requests. access.Service re-checks that the decider
// is an organization administrator and not the requester.
type Decider interface {
	DecideAccessRequestAs(ctx context.Context, input access.DecideAccessRequestInput) (*gen.AccessRequest, error)
}

// Interactions handles clicks on access request DMs.
type Interactions struct {
	logger        *slog.Logger
	db            *pgxpool.Pool
	cards         *Cards
	decider       Decider
	responder     slackdm.Responder
	signingSecret string
	now           func() time.Time
}

// NewInteractions builds the click handler. signingSecret is the Slack app's
// signing secret; every request must carry its signature.
func NewInteractions(logger *slog.Logger, cards *Cards, decider Decider, responder slackdm.Responder, signingSecret string) *Interactions {
	return &Interactions{
		logger:        logger.With(attr.SlogComponent("access_request_slack_interactions")),
		db:            cards.db,
		cards:         cards,
		decider:       decider,
		responder:     responder,
		signingSecret: signingSecret,
		now:           time.Now,
	}
}

// clickPayload is the part of a Slack block_actions payload a click on an
// access request DM uses.
type clickPayload struct {
	Type string `json:"type"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Container struct {
		ChannelID string `json:"channel_id"`
		MessageTS string `json:"message_ts"`
	} `json:"container"`
	ResponseURL string `json:"response_url"`
	Actions     []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
	// State holds what the message's menus show at the time of the click,
	// keyed by block ID and then action ID.
	State struct {
		Values map[string]map[string]struct {
			SelectedOption *struct {
				Value string `json:"value"`
			} `json:"selected_option"`
		} `json:"values"`
	} `json:"state"`
}

// chosenWindow is the window the card's menu shows. A menu nobody changed
// may be left out of the click's state; its initial choice, the first fixed
// window, applies then.
func (p clickPayload) chosenWindow() string {
	for _, actions := range p.State.Values {
		if menu, ok := actions[actionWindow]; ok && menu.SelectedOption != nil {
			return menu.SelectedOption.Value
		}
	}
	return approveWindows[0].key
}

// ServeHTTP verifies Slack's signature, decides the request for the
// administrator the DM was sent to, and replies to the click. Everything
// after the signature check answers 200, since Slack shows any other status
// as a generic failure; refusals go back as ephemeral notes. The click is
// acknowledged before it is decided: Slack wants the 200 within 3 seconds.
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
	var payload clickPayload
	if err := json.Unmarshal([]byte(form.Get("payload")), &payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
	if payload.Type != "block_actions" || len(payload.Actions) != 1 {
		return
	}
	action := payload.Actions[0]
	// Picking a time in the menu only changes the menu; the click is
	// acknowledged and nothing is decided until Allow or Deny.
	if action.ActionID != actionAllow && action.ActionID != actionDeny {
		return
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		h.logger.WarnContext(ctx, "acknowledge Slack interaction early", attr.SlogError(err))
	}
	// Slack may close the connection once it has the 200; the decision and
	// the reply still have to finish.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), clickTimeout)
	defer cancel()
	reply := h.handleClick(ctx, payload, action.ActionID, action.Value)
	if err := h.responder.Respond(ctx, payload.ResponseURL, reply); err != nil {
		h.logger.WarnContext(ctx, "reply to Slack interaction", attr.SlogSlackTeamID(payload.Team.ID), attr.SlogError(err))
	}
}

func (h *Interactions) handleClick(ctx context.Context, payload clickPayload, actionID, value string) slackdm.ResponseMessage {
	found, err := repo.New(h.db).GetAccessRequestMessageBySlackMessage(ctx, repo.GetAccessRequestMessageBySlackMessageParams{
		SlackTeamID:    conv.ToPGText(payload.Team.ID),
		SlackChannelID: conv.ToPGText(payload.Container.ChannelID),
		SlackMessageTs: conv.ToPGText(payload.Container.MessageTS),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return slackdm.Ephemeral("This access request is no longer available.")
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "look up access request DM", attr.SlogError(err))
		return slackdm.Ephemeral("Something went wrong. Try again, or decide in Speakeasy.")
	}
	message := found.AccessRequestMessage
	requestID, err := uuid.Parse(value)
	ok := err == nil
	window, endOfDay := "", time.Time{}
	if ok && actionID == actionAllow {
		window, endOfDay, ok = parseWindow(payload.chosenWindow())
	}
	// The DM went to one administrator, so a click is theirs only when it
	// comes from that Slack user and names the request the DM was sent for.
	if !ok || message.Kind != kindDM || message.SlackUserID.String != payload.User.ID || message.AccessRequestID != requestID {
		h.logger.WarnContext(ctx, "reject Slack click from another user", attr.SlogSlackTeamID(payload.Team.ID))
		return slackdm.Ephemeral("Only the administrator this message was sent to can decide here.")
	}
	target := Target{OrganizationID: message.OrganizationID, RequestID: message.AccessRequestID}
	logger := h.logger.With(attr.SlogOrganizationID(message.OrganizationID), attr.SlogUserID(message.RecipientUserID))

	approve := actionID == actionAllow
	deadline := time.Time{}
	switch {
	case window == endOfDayKey:
		if endOfDay.Sub(h.now()) < access.MinEndOfDayLead {
			return slackdm.Ephemeral("This request's end of day has passed or is too close. Pick a duration instead.")
		}
		deadline = endOfDay
	case approve:
		deadline = h.now().Add(windowDuration(window))
	}

	_, err = h.decider.DecideAccessRequestAs(ctx, access.DecideAccessRequestInput{
		OrganizationID: message.OrganizationID,
		RequestID:      message.AccessRequestID,
		DeciderUserID:  message.RecipientUserID,
		DeciderEmail:   h.cards.memberEmail(ctx, message.RecipientUserID),
		Approve:        approve,
		GrantExpiresAt: deadline.UTC().Format(time.RFC3339),
		Via:            access.AccessRequestDecidedViaSlack,
	})
	var shareable *oops.ShareableError
	switch {
	case errors.Is(err, access.ErrAccessRequestSelfDecision):
		return slackdm.Ephemeral("You made this request, so another administrator has to decide it.")
	case errors.Is(err, access.ErrAccessRequestNotAdmin):
		return slackdm.Ephemeral("You are no longer an organization administrator, so you cannot decide this request.")
	case errors.Is(err, access.ErrAccessRequestNotFound):
		return slackdm.Ephemeral("This access request no longer exists.")
	case errors.Is(err, access.ErrAccessRequestNotPending):
		return h.replaceWithCurrent(ctx, logger, target)
	case errors.As(err, &shareable) && shareable.Code != oops.CodeUnexpected:
		// A refusal the temporary grant rules give, such as access the
		// requester already holds: tell the clicker why.
		return slackdm.Ephemeral("This request could not be approved: " + shareable.Error() + ". Decide it in Speakeasy instead.")
	case err != nil:
		logger.ErrorContext(ctx, "decide access request from Slack", attr.SlogError(err))
		return slackdm.Ephemeral("Something went wrong. Try again, or decide in Speakeasy.")
	}
	return h.replaceWithCurrent(ctx, logger, target)
}

// replaceWithCurrent shows the request as it stands now.
func (h *Interactions) replaceWithCurrent(ctx context.Context, logger *slog.Logger, target Target) slackdm.ResponseMessage {
	row, err := h.cards.request(ctx, target)
	if err != nil {
		logger.ErrorContext(ctx, "reload access request after a Slack click", attr.SlogError(err))
		return slackdm.Ephemeral("This request was already decided or has expired.")
	}
	content, err := h.cards.input(ctx, row)
	if err != nil {
		logger.ErrorContext(ctx, "build decided access request card", attr.SlogError(err))
		return slackdm.Ephemeral("Your decision was recorded.")
	}
	return slackdm.Replace(decidedCard(content))
}

// memberEmail names a decider in the audit log the way dashboard decisions
// do, by email. Nil when the member cannot be found.
func (c *Cards) memberEmail(ctx context.Context, userID string) *string {
	email, err := c.email(ctx, userID)
	if err != nil || email == "" {
		return nil
	}
	return &email
}
