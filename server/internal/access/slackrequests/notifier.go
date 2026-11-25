package slackrequests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dashboardlinks"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/slackscopes"
	"github.com/speakeasy-api/gram/server/internal/slackdm"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

const (
	// providerSlack and the kinds label the messages this package records.
	providerSlack = "slack"
	// kindDM is a card asking an administrator to decide.
	kindDM = "dm"
	// kindOutcome is the DM telling the requester how their request ended.
	kindOutcome = "outcome"

	// maxRecipients bounds the DMs one request sends, like approval DMs.
	maxRecipients = 50
)

// Target names one access request.
type Target struct {
	// OrganizationID is the organization the request belongs to.
	OrganizationID string

	// RequestID is the request.
	RequestID uuid.UUID
}

// NotifyResult says what Notify left behind.
type NotifyResult struct {
	// Sent counts administrators with a DM for the request, including DMs an
	// earlier attempt sent.
	Sent int

	// ExpiresAt is when the request lapses. Zero when nothing was sent.
	ExpiresAt time.Time
}

// WorkspaceSource lists the workspaces the app can post to.
type WorkspaceSource interface {
	MessagingWorkspaces(ctx context.Context, organizationID, scope string) ([]slackdirectoryconnections.MessagingWorkspace, error)
}

// Cards reads what an access request card shows. The worker uses it to send
// and update DMs, and the server to answer clicks.
type Cards struct {
	db    *pgxpool.Pool
	hosts *orghost.Resolver
}

// NewCards builds Cards. hosts resolves each organization's dashboard origin
// for the link on the cards; nil leaves the link out.
func NewCards(db *pgxpool.Pool, hosts *orghost.Resolver) *Cards {
	return &Cards{db: db, hosts: hosts}
}

// Notifier sends and updates access request DMs.
type Notifier struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	cards      *Cards
	workspaces WorkspaceSource
	slack      *slackdm.Messenger
	now        func() time.Time
}

// NewNotifier builds a Notifier that posts through slack to the workspaces
// workspaces lists.
func NewNotifier(logger *slog.Logger, cards *Cards, workspaces WorkspaceSource, slack *slackapi.Client) *Notifier {
	return &Notifier{
		logger:     logger.With(attr.SlogComponent("access_request_slack")),
		db:         cards.db,
		cards:      cards,
		workspaces: workspaces,
		slack:      slackdm.NewMessenger(cards.db, slack),
		now:        time.Now,
	}
}

// Notify DMs each active organization administrator, other than the
// requester, who has not been sent the request yet. It is safe to repeat:
// administrators with a recorded DM are skipped. A Slack failure worth
// retrying is returned after every other administrator has been tried, and
// every return reports the recorded DMs.
func (n *Notifier) Notify(ctx context.Context, target Target) (NotifyResult, error) {
	none := NotifyResult{Sent: 0, ExpiresAt: time.Time{}}
	row, err := n.cards.request(ctx, target)
	if errors.Is(err, access.ErrAccessRequestNotFound) {
		return none, nil
	}
	if err != nil {
		return none, err
	}
	queries := repo.New(n.db)
	sentRows, err := queries.ListAccessRequestMessages(ctx, repo.ListAccessRequestMessagesParams{OrganizationID: target.OrganizationID, AccessRequestID: target.RequestID})
	if err != nil {
		return none, fmt.Errorf("list access request messages: %w", err)
	}
	sent := make(map[string]bool, len(sentRows))
	for _, message := range sentRows {
		if message.Provider == providerSlack && message.Kind == kindDM {
			sent[message.RecipientUserID] = true
		}
	}
	recorded := func() NotifyResult { return NotifyResult{Sent: len(sent), ExpiresAt: row.AccessRequest.ExpiresAt.Time} }

	if access.EffectiveAccessRequestStatus(row.AccessRequest, n.now()) != access.AccessRequestStatusPending {
		return recorded(), nil
	}
	logger := n.logger.With(attr.SlogOrganizationID(target.OrganizationID))

	// Workspaces that cannot be used right now come back as a retryable error
	// next to the ones that can. The DMs go out through those, and the retry
	// reaches the rest.
	workspaces, unavailable := n.workspaces.MessagingWorkspaces(ctx, target.OrganizationID, slackscopes.ChatWrite)
	if len(workspaces) == 0 && unavailable != nil {
		return recorded(), fmt.Errorf("list Slack workspaces: %w", unavailable)
	}
	if len(workspaces) == 0 {
		logger.InfoContext(ctx, "no Slack workspace can send access request DMs")
		return recorded(), nil
	}

	recipients, err := n.recipients(ctx, row.AccessRequest)
	if err != nil {
		return recorded(), err
	}
	content, err := n.cards.input(ctx, row)
	if err != nil {
		return recorded(), err
	}
	requesterZone := n.requesterZone(ctx, row.AccessRequest, workspaces)

	var retry error
	for _, person := range recipients {
		if sent[person.ID] {
			continue
		}
		workspace, slackUserID, err := n.slack.FindUser(ctx, target.OrganizationID, slackdm.Person{UserID: person.ID, Email: person.Email}, workspaces)
		if err != nil {
			retry = errors.Join(retry, err)
			continue
		}
		if workspace == nil {
			logger.InfoContext(ctx, "administrator has no Slack account in a connected workspace", attr.SlogUserID(person.ID))
			continue
		}
		zone := requesterZone
		if zone == nil {
			zone = n.zone(ctx, workspace.Token, slackUserID)
		}
		if zone == nil {
			zone = time.UTC
		}
		posted, err := n.slack.Post(ctx, workspace.Token, slackUserID, pendingCard(content, cardEndOfDay(content, n.now(), zone)))
		if err != nil {
			if slackdm.Retryable(err) {
				retry = errors.Join(retry, err)
				continue
			}
			logger.WarnContext(ctx, "send access request DM", attr.SlogUserID(person.ID), attr.SlogSlackTeamID(workspace.TeamID), attr.SlogError(err))
			continue
		}
		if err := n.record(ctx, target, kindDM, person.ID, workspace.TeamID, slackUserID, posted); err != nil {
			return recorded(), err
		}
		sent[person.ID] = true
	}
	// A workspace that could not be used matters only while an administrator
	// still has no DM: they may be in it.
	if unavailable != nil && slices.ContainsFunc(recipients, func(person repo.ListActiveOrganizationAdminsRow) bool { return !sent[person.ID] }) {
		retry = errors.Join(retry, unavailable)
	}
	if retry != nil {
		return recorded(), fmt.Errorf("send access request DMs: %w", retry)
	}
	return recorded(), nil
}

// Finish replaces every administrator's DM of a request that is no longer
// pending with the outcome, without buttons, and tells the requester how it
// ended. A request still pending is left alone.
func (n *Notifier) Finish(ctx context.Context, target Target) error {
	row, err := n.cards.request(ctx, target)
	if errors.Is(err, access.ErrAccessRequestNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if access.EffectiveAccessRequestStatus(row.AccessRequest, n.now()) == access.AccessRequestStatusPending {
		return nil
	}
	messages, err := repo.New(n.db).ListAccessRequestMessages(ctx, repo.ListAccessRequestMessagesParams{OrganizationID: target.OrganizationID, AccessRequestID: target.RequestID})
	if err != nil {
		return fmt.Errorf("list access request messages: %w", err)
	}
	content, err := n.cards.input(ctx, row)
	if err != nil {
		return err
	}
	final := decidedCard(content)
	workspaces, unavailable := n.workspaces.MessagingWorkspaces(ctx, target.OrganizationID, slackscopes.ChatWrite)
	tokens := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		tokens[workspace.TeamID] = workspace.Token
	}

	var retry error
	skipped := false
	toldRequester := false
	for _, message := range messages {
		if message.Kind == kindOutcome {
			toldRequester = true
			continue
		}
		if message.Provider != providerSlack || !message.SlackChannelID.Valid || !message.SlackMessageTs.Valid {
			continue
		}
		token := tokens[message.SlackTeamID.String]
		if token == "" {
			skipped = true
			continue
		}
		if err := n.slack.Update(ctx, token, message.SlackChannelID.String, message.SlackMessageTs.String, final); err != nil {
			if slackdm.Retryable(err) {
				retry = errors.Join(retry, err)
				continue
			}
			n.logger.WarnContext(ctx, "update access request DM", attr.SlogOrganizationID(target.OrganizationID), attr.SlogSlackTeamID(message.SlackTeamID.String), attr.SlogError(err))
		}
	}
	if !toldRequester && len(messages) > 0 {
		if err := n.tellRequester(ctx, target, row.AccessRequest, content, workspaces); err != nil {
			retry = errors.Join(retry, err)
		}
	}
	if skipped {
		retry = errors.Join(retry, unavailable)
	}
	if retry != nil {
		return fmt.Errorf("finish access request DMs: %w", retry)
	}
	return nil
}

// tellRequester DMs the requester the outcome, when they have a Slack account
// in a connected workspace. A withdrawn request needs no DM: they withdrew it.
func (n *Notifier) tellRequester(ctx context.Context, target Target, row repo.AccessRequest, content cardInput, workspaces []slackdirectoryconnections.MessagingWorkspace) error {
	message, ok := requesterOutcome(content)
	if !ok || len(workspaces) == 0 {
		return nil
	}
	email, err := n.cards.email(ctx, row.RequesterUserID)
	if err != nil {
		return err
	}
	workspace, slackUserID, err := n.slack.FindUser(ctx, target.OrganizationID, slackdm.Person{UserID: row.RequesterUserID, Email: email}, workspaces)
	if err != nil {
		return fmt.Errorf("find requester in Slack: %w", err)
	}
	if workspace == nil {
		return nil
	}
	posted, err := n.slack.Post(ctx, workspace.Token, slackUserID, message)
	if err != nil {
		if slackdm.Retryable(err) {
			return fmt.Errorf("send access request outcome DM: %w", err)
		}
		n.logger.WarnContext(ctx, "send access request outcome DM", attr.SlogOrganizationID(target.OrganizationID), attr.SlogError(err))
		return nil
	}
	return n.record(ctx, target, kindOutcome, row.RequesterUserID, workspace.TeamID, slackUserID, posted)
}

func (n *Notifier) record(ctx context.Context, target Target, kind, recipientUserID, teamID, slackUserID string, posted slackdm.Posted) error {
	_, err := repo.New(n.db).InsertAccessRequestMessage(ctx, repo.InsertAccessRequestMessageParams{
		OrganizationID:  target.OrganizationID,
		AccessRequestID: target.RequestID,
		Provider:        providerSlack,
		Kind:            kind,
		RecipientUserID: recipientUserID,
		SlackTeamID:     conv.ToPGText(teamID),
		SlackUserID:     conv.ToPGText(slackUserID),
		SlackChannelID:  conv.ToPGText(posted.Channel),
		SlackMessageTs:  conv.ToPGText(posted.TS),
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("record access request message: %w", err)
	}
	return nil
}

// recipients are the organization's active administrators, without the
// requester, who cannot decide their own request.
func (n *Notifier) recipients(ctx context.Context, row repo.AccessRequest) ([]repo.ListActiveOrganizationAdminsRow, error) {
	admins, err := repo.New(n.db).ListActiveOrganizationAdmins(ctx, row.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization administrators: %w", err)
	}
	out := make([]repo.ListActiveOrganizationAdminsRow, 0, min(len(admins), maxRecipients))
	for _, admin := range admins {
		if admin.ID == row.RequesterUserID {
			continue
		}
		if len(out) == maxRecipients {
			n.logger.WarnContext(ctx, "access request has more administrators than DMs it sends", attr.SlogOrganizationID(row.OrganizationID))
			break
		}
		out = append(out, admin)
	}
	return out, nil
}

// requesterZone is the time zone on the requester's Slack profile, which End
// of day follows. Nil when they have no Slack account or no zone; each
// administrator's own zone stands in then.
func (n *Notifier) requesterZone(ctx context.Context, row repo.AccessRequest, workspaces []slackdirectoryconnections.MessagingWorkspace) *time.Location {
	email, err := n.cards.email(ctx, row.RequesterUserID)
	if err != nil {
		return nil
	}
	workspace, slackUserID, err := n.slack.FindUser(ctx, row.OrganizationID, slackdm.Person{UserID: row.RequesterUserID, Email: email}, workspaces)
	if err != nil || workspace == nil {
		return nil
	}
	return n.zone(ctx, workspace.Token, slackUserID)
}

// zone is the time zone on a Slack profile, or nil when it has none Go
// knows.
func (n *Notifier) zone(ctx context.Context, token, slackUserID string) *time.Location {
	name, err := n.slack.TimeZone(ctx, token, slackUserID)
	if err != nil || name == "" {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}

// request reads one request with the deadline of the grant an approval gave.
func (c *Cards) request(ctx context.Context, target Target) (repo.GetAccessRequestRow, error) {
	row, err := repo.New(c.db).GetAccessRequest(ctx, repo.GetAccessRequestParams{OrganizationID: target.OrganizationID, ID: target.RequestID})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.GetAccessRequestRow{}, access.ErrAccessRequestNotFound
	}
	if err != nil {
		return repo.GetAccessRequestRow{}, fmt.Errorf("get access request: %w", err)
	}
	return row, nil
}

func (c *Cards) email(ctx context.Context, userID string) (string, error) {
	people, err := repo.New(c.db).ListAccessRequestPeople(ctx, []string{userID})
	if err != nil {
		return "", fmt.Errorf("get requester: %w", err)
	}
	if len(people) == 0 {
		return "", nil
	}
	return people[0].Email, nil
}

func (c *Cards) input(ctx context.Context, row repo.GetAccessRequestRow) (cardInput, error) {
	request := row.AccessRequest
	ids := []string{request.RequesterUserID}
	if request.DecidedByUserID.Valid {
		ids = append(ids, request.DecidedByUserID.String)
	}
	people, err := repo.New(c.db).ListAccessRequestPeople(ctx, ids)
	if err != nil {
		return cardInput{}, fmt.Errorf("name access request people: %w", err)
	}
	names := make(map[string]string, len(people))
	for _, person := range people {
		names[person.ID] = conv.Default(person.DisplayName, person.Email)
	}
	content := cardInput{
		RequestID:         request.ID,
		Status:            access.EffectiveAccessRequestStatus(request, time.Now()),
		RequesterName:     conv.Default(names[request.RequesterUserID], "A former member"),
		PermissionLabel:   request.Scope,
		PermissionScope:   request.Scope,
		ResourceName:      request.ResourceName.String,
		Message:           request.Message.String,
		DeciderName:       "",
		ExpiresAt:         request.ExpiresAt.Time,
		GrantExpiresAt:    time.Time{},
		DashboardURL:      "",
		RequestedDuration: 0,
		RequestedUntil:    time.Time{},
	}
	if request.RequestedDurationSeconds.Valid {
		content.RequestedDuration = time.Duration(request.RequestedDurationSeconds.Int32) * time.Second
	}
	if request.RequestedUntil.Valid {
		content.RequestedUntil = request.RequestedUntil.Time
	}
	if entry, ok := authz.ScopeEntryFor(authz.Scope(request.Scope)); ok {
		content.PermissionLabel = entry.Label
	}
	if request.DecidedByUserID.Valid {
		content.DeciderName = conv.Default(names[request.DecidedByUserID.String], "a former member")
	}
	if row.GrantExpiresAt.Valid {
		content.GrantExpiresAt = row.GrantExpiresAt.Time
	}
	content.DashboardURL, err = c.dashboardURL(ctx, request)
	if err != nil {
		return cardInput{}, err
	}
	return content, nil
}

// dashboardURL opens the request on the Access page, the same link the email
// carries.
func (c *Cards) dashboardURL(ctx context.Context, request repo.AccessRequest) (string, error) {
	if c.hosts == nil {
		return "", nil
	}
	org, err := orgrepo.New(c.db).GetOrganizationMetadata(ctx, request.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get organization: %w", err)
	}
	siteURL := c.hosts.SiteURL(org.DefaultHost)
	if siteURL == nil {
		return "", nil
	}
	return dashboardlinks.AccessGrant(org.Slug, request.RequesterUserID, request.Scope, "", "", request.ID.String()).URL(siteURL), nil
}
