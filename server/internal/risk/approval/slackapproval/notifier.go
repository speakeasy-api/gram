// Package slackapproval sends approval requests to approvers as Slack DMs and
// takes their decisions back from Slack. Slack only notifies: every click goes
// through approval.Service.Decide, the same check the dashboard uses.
package slackapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/slackscopes"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// providerSlack and kindDM label the messages this package records.
	providerSlack = "slack"
	kindDM        = "dm"

	// maxRecipients bounds the DMs one request sends. Approver lists are a
	// handful of people; a role that expands past this is better served by
	// the dashboard than by a burst of DMs.
	maxRecipients = 50

	// maxRoleMembers bounds the members checked when a role approver expands
	// to people. Each check resolves the member's roles, so this caps the
	// work one request can cause in a large organization.
	maxRoleMembers = 2000
)

// Target names one approval request.
type Target struct {
	// OrganizationID is the organization the request belongs to.
	OrganizationID string

	// ProjectID is the project the request belongs to.
	ProjectID uuid.UUID

	// ApprovalID is the request.
	ApprovalID uuid.UUID
}

// NotifyResult says what Notify left behind.
type NotifyResult struct {
	// Sent counts approvers with a DM for the request, including DMs an
	// earlier attempt sent.
	Sent int

	// ExpiresAt is when the request lapses. Zero when nothing was sent.
	ExpiresAt time.Time
}

// WorkspaceSource lists the workspaces the app can post to.
type WorkspaceSource interface {
	MessagingWorkspaces(ctx context.Context, organizationID, scope string) ([]slackdirectoryconnections.MessagingWorkspace, error)
}

// Cards reads what an approval request card shows. The worker uses it to send
// and update DMs, and the server to answer clicks.
type Cards struct {
	db        *pgxpool.Pool
	approvals *approval.Service
	hosts     *orghost.Resolver
}

// NewCards builds Cards. hosts resolves each organization's dashboard origin
// for the link on the cards; nil leaves the link out.
func NewCards(db *pgxpool.Pool, approvals *approval.Service, hosts *orghost.Resolver) *Cards {
	return &Cards{db: db, approvals: approvals, hosts: hosts}
}

// Notifier sends and updates approval request DMs.
type Notifier struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	approvals  *approval.Service
	cards      *Cards
	workspaces WorkspaceSource
	slack      *slackapi.Client
}

// NewNotifier builds a Notifier that posts through slack to the workspaces
// workspaces lists.
func NewNotifier(logger *slog.Logger, cards *Cards, workspaces WorkspaceSource, slack *slackapi.Client) *Notifier {
	return &Notifier{
		logger:     logger.With(attr.SlogComponent("risk_approval_slack")),
		db:         cards.db,
		approvals:  cards.approvals,
		cards:      cards,
		workspaces: workspaces,
		slack:      slack,
	}
}

// recipient is an approver the request goes to.
type recipient struct {
	userID      string
	email       string
	displayName string
}

// Notify DMs each approver who has not been sent the request yet. It is safe
// to repeat: approvers with a recorded DM are skipped. A Slack failure worth
// retrying is returned after every other approver has been tried. Once the
// recorded DMs are known, every return reports them, errors included, so a
// run that stops retrying still gives those DMs the outcome.
func (n *Notifier) Notify(ctx context.Context, target Target) (NotifyResult, error) {
	none := NotifyResult{Sent: 0, ExpiresAt: time.Time{}}
	row, err := n.approvals.Get(ctx, target.OrganizationID, target.ProjectID, target.ApprovalID)
	if errors.Is(err, approval.ErrNotFound) {
		return none, nil
	}
	if err != nil {
		return none, fmt.Errorf("get approval: %w", err)
	}
	queries := repo.New(n.db)
	sentRows, err := queries.ListApprovalMessages(ctx, repo.ListApprovalMessagesParams{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, ApprovalID: row.ID})
	if err != nil {
		return none, fmt.Errorf("list approval messages: %w", err)
	}
	sent := make(map[string]bool, len(sentRows))
	for _, message := range sentRows {
		if message.Provider == providerSlack {
			sent[message.RecipientUserID] = true
		}
	}
	recorded := func() NotifyResult {
		result := NotifyResult{Sent: len(sent), ExpiresAt: time.Time{}}
		if row.ExpiresAt.Valid {
			result.ExpiresAt = row.ExpiresAt.Time
		}
		return result
	}

	if n.approvals.EffectiveStatus(row) != approval.StatusPending {
		return recorded(), nil
	}
	policy, err := n.cards.policy(ctx, row)
	if errors.Is(err, approval.ErrNotFound) || (err == nil && policy.ApprovalConfig == nil) {
		return recorded(), nil
	}
	if err != nil {
		return recorded(), err
	}
	logger := n.logger.With(attr.SlogOrganizationID(target.OrganizationID), attr.SlogRiskPolicyApprovalID(target.ApprovalID.String()))

	// Workspaces that cannot be used right now come back as a retryable error
	// next to the ones that can. The DMs go out through those, and the retry
	// reaches the rest.
	workspaces, unavailable := n.workspaces.MessagingWorkspaces(ctx, target.OrganizationID, slackscopes.ChatWrite)
	if len(workspaces) == 0 && unavailable != nil {
		return recorded(), fmt.Errorf("list Slack workspaces: %w", unavailable)
	}
	if len(workspaces) == 0 {
		logger.InfoContext(ctx, "no Slack workspace can send approval DMs")
		return recorded(), nil
	}

	recipients, err := n.recipients(ctx, row, policy.ApprovalConfig)
	if err != nil {
		return recorded(), err
	}
	content, err := n.cards.input(ctx, row, policy.Name)
	if err != nil {
		return recorded(), err
	}
	pending := pendingCard(content)

	retry := unavailable
	for _, person := range recipients {
		if sent[person.userID] {
			continue
		}
		workspace, slackUserID, err := n.findSlackUser(ctx, row.OrganizationID, person, workspaces)
		if err != nil {
			retry = errors.Join(retry, err)
			continue
		}
		if workspace == nil {
			logger.InfoContext(ctx, "approver has no Slack account in a connected workspace", attr.SlogUserID(person.userID))
			continue
		}
		posted, err := n.post(ctx, workspace.Token, slackUserID, pending)
		if err != nil {
			if retryable(err) {
				retry = errors.Join(retry, err)
				continue
			}
			logger.WarnContext(ctx, "send approval DM", attr.SlogUserID(person.userID), attr.SlogSlackTeamID(workspace.TeamID), attr.SlogError(err))
			continue
		}
		_, err = queries.InsertApprovalMessage(ctx, repo.InsertApprovalMessageParams{
			OrganizationID:  row.OrganizationID,
			ProjectID:       row.ProjectID,
			ApprovalID:      row.ID,
			Provider:        providerSlack,
			Kind:            kindDM,
			RecipientUserID: person.userID,
			SlackTeamID:     conv.ToPGText(workspace.TeamID),
			SlackUserID:     conv.ToPGText(slackUserID),
			SlackChannelID:  conv.ToPGText(posted.Channel),
			SlackMessageTs:  conv.ToPGText(posted.TS),
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return recorded(), fmt.Errorf("record approval message: %w", err)
		}
		sent[person.userID] = true
	}
	if retry != nil {
		return recorded(), fmt.Errorf("send approval DMs: %w", retry)
	}
	return recorded(), nil
}

// Finish replaces every DM of a request that is no longer pending with the
// outcome, without buttons. A request still pending is left alone.
func (n *Notifier) Finish(ctx context.Context, target Target) error {
	row, err := n.approvals.Get(ctx, target.OrganizationID, target.ProjectID, target.ApprovalID)
	if errors.Is(err, approval.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get approval: %w", err)
	}
	if n.approvals.EffectiveStatus(row) == approval.StatusPending {
		return nil
	}
	messages, err := repo.New(n.db).ListApprovalMessages(ctx, repo.ListApprovalMessagesParams{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, ApprovalID: row.ID})
	if err != nil {
		return fmt.Errorf("list approval messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}
	final, err := n.cards.decided(ctx, row)
	if err != nil {
		return err
	}
	// A message in a workspace that cannot be used right now is skipped, and
	// the retry reaches it.
	workspaces, unavailable := n.workspaces.MessagingWorkspaces(ctx, target.OrganizationID, slackscopes.ChatWrite)
	tokens := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		tokens[workspace.TeamID] = workspace.Token
	}
	retry := unavailable
	for _, message := range messages {
		token := tokens[message.SlackTeamID.String]
		if message.Provider != providerSlack || token == "" || !message.SlackChannelID.Valid || !message.SlackMessageTs.Valid {
			continue
		}
		_, err := n.slack.CallWithToken(ctx, "chat.update", map[string]any{
			"channel": message.SlackChannelID.String,
			"ts":      message.SlackMessageTs.String,
			"text":    final.Text,
			"blocks":  final.Blocks,
		}, token)
		if err != nil {
			if retryable(err) {
				retry = errors.Join(retry, err)
				continue
			}
			n.logger.WarnContext(ctx, "update approval DM", attr.SlogRiskPolicyApprovalID(row.ID.String()), attr.SlogSlackTeamID(message.SlackTeamID.String), attr.SlogError(err))
		}
	}
	if retry != nil {
		return fmt.Errorf("update approval DMs: %w", retry)
	}
	return nil
}

// decided is the card a request's DMs show once it is no longer pending.
func (c *Cards) decided(ctx context.Context, row repo.RiskPolicyApproval) (card, error) {
	policy, err := c.policy(ctx, row)
	name := "Deleted policy"
	if err == nil {
		name = policy.Name
	}
	if err != nil && !errors.Is(err, approval.ErrNotFound) {
		return card{Text: "", Blocks: nil}, err
	}
	content, err := c.input(ctx, row, name)
	if err != nil {
		return card{Text: "", Blocks: nil}, err
	}
	return decidedCard(content), nil
}

func (c *Cards) policy(ctx context.Context, row repo.RiskPolicyApproval) (policycore.Policy, error) {
	policyRow, err := riskrepo.New(c.db).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: row.RiskPolicyID, ProjectID: row.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && policyRow.OrganizationID != row.OrganizationID) {
		return policycore.Policy{}, approval.ErrNotFound
	}
	if err != nil {
		return policycore.Policy{}, fmt.Errorf("get policy: %w", err)
	}
	return policycore.Project(policyRow, nil), nil
}

// recipients are the people on the policy's approver list, with roles
// expanded through the same principal resolution Decide uses. People named on
// the list come first, so a role expanding to maxRoleMembers cannot crowd them
// out. The person who made the call is left out: they cannot decide it.
func (n *Notifier) recipients(ctx context.Context, row repo.RiskPolicyApproval, config *policycore.ApprovalConfig) ([]recipient, error) {
	userIDs := []string{}
	expandRoles := false
	for _, approver := range config.Approvers {
		if id, ok := strings.CutPrefix(approver, string(urn.PrincipalTypeUser)+":"); ok {
			userIDs = append(userIDs, id)
			continue
		}
		expandRoles = true
	}
	// One member past the cap tells a role that expands further apart from
	// one that fits exactly.
	limit := int32(maxRecipients)
	if expandRoles {
		limit = maxRoleMembers + 1
	}
	members, err := repo.New(n.db).ListApprovalRecipientMembers(ctx, repo.ListApprovalRecipientMembersParams{
		OrganizationID: row.OrganizationID,
		AllMembers:     expandRoles,
		UserIds:        userIDs,
		MaxRows:        limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list approval recipients: %w", err)
	}
	if expandRoles && len(members) > maxRoleMembers {
		n.logger.WarnContext(ctx, "approval request role approvers expand past the members checked for DMs", attr.SlogRiskPolicyApprovalID(row.ID.String()))
		members = members[:maxRoleMembers]
	}
	named := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		named[id] = true
	}
	out := make([]recipient, 0, min(len(members), maxRecipients))
	for _, member := range members {
		if len(out) == maxRecipients {
			n.logger.WarnContext(ctx, "approval request has more approvers than DMs it sends", attr.SlogRiskPolicyApprovalID(row.ID.String()))
			break
		}
		if row.RequesterUserID.Valid && row.RequesterUserID.String == member.UserID {
			continue
		}
		if !named[member.UserID] {
			principals, err := n.approvals.ApproverPrincipals(ctx, row.OrganizationID, member.UserID)
			if err != nil {
				return nil, fmt.Errorf("resolve approver: %w", err)
			}
			if !approval.CanApprove(principals, config) {
				continue
			}
		}
		out = append(out, recipient{userID: member.UserID, email: member.Email, displayName: member.DisplayName})
	}
	return out, nil
}

// findSlackUser finds the approver in a workspace the app can post to: a
// confirmed mapping first, then their email, when Slack has confirmed it the
// way directory sync requires. It returns a nil workspace when the approver
// has no Slack account there.
func (n *Notifier) findSlackUser(ctx context.Context, organizationID string, person recipient, workspaces []slackdirectoryconnections.MessagingWorkspace) (*slackdirectoryconnections.MessagingWorkspace, string, error) {
	accounts, err := slackdirectoryconnections.SlackAccounts(ctx, n.db, organizationID, person.userID)
	if err != nil {
		return nil, "", fmt.Errorf("list approver Slack accounts: %w", err)
	}
	for _, account := range accounts {
		for i := range workspaces {
			if workspaces[i].TeamID == account.TeamID {
				return &workspaces[i], account.UserID, nil
			}
		}
	}
	if person.email == "" {
		return nil, "", nil
	}
	var retry error
	for i := range workspaces {
		body, err := n.slack.CallWithToken(ctx, "users.lookupByEmail", map[string]any{"email": person.email}, workspaces[i].Token)
		if err != nil {
			if retryable(err) {
				retry = errors.Join(retry, err)
			}
			continue
		}
		var found struct {
			User struct {
				ID             string `json:"id"`
				EmailConfirmed *bool  `json:"is_email_confirmed"`
			} `json:"user"`
		}
		if err := json.Unmarshal(body, &found); err != nil || found.User.ID == "" || !conv.PtrValOr(found.User.EmailConfirmed, true) {
			continue
		}
		return &workspaces[i], found.User.ID, nil
	}
	return nil, "", retry
}

// postedMessage is where chat.postMessage put a message.
type postedMessage struct {
	// Channel is the DM channel ID.
	Channel string `json:"channel"`

	// TS is the message timestamp, its ID within the channel.
	TS string `json:"ts"`
}

// post sends a card to a Slack user. A user ID as the channel lands in the
// app's DM with them, so nobody has to invite the app anywhere.
func (n *Notifier) post(ctx context.Context, token, slackUserID string, content card) (postedMessage, error) {
	body, err := n.slack.CallWithToken(ctx, "chat.postMessage", map[string]any{
		"channel":      slackUserID,
		"text":         content.Text,
		"blocks":       content.Blocks,
		"unfurl_links": false,
	}, token)
	if err != nil {
		return postedMessage{Channel: "", TS: ""}, fmt.Errorf("post approval DM: %w", err)
	}
	var posted postedMessage
	if err := json.Unmarshal(body, &posted); err != nil || posted.Channel == "" || posted.TS == "" {
		return postedMessage{Channel: "", TS: ""}, errors.New("post approval DM: Slack returned no message")
	}
	return posted, nil
}

func (c *Cards) input(ctx context.Context, row repo.RiskPolicyApproval, policyName string) (cardInput, error) {
	names, err := c.displayNames(ctx, row)
	if err != nil {
		return cardInput{}, err
	}
	content := cardInput{
		Approval:      row,
		Status:        c.approvals.EffectiveStatus(row),
		PolicyName:    policyName,
		RequesterName: requesterName(row.RequesterUrn, names),
		ServerName:    "",
		DeciderName:   "",
		DashboardURL:  "",
	}
	content.ServerName, err = c.serverName(ctx, row)
	if err != nil {
		return cardInput{}, err
	}
	if decider, ok := strings.CutPrefix(row.DecidedByUrn.String, string(urn.PrincipalTypeUser)+":"); ok && row.DecidedByUrn.Valid {
		content.DeciderName = conv.Default(names[decider], "a former member")
	}
	content.DashboardURL, err = c.dashboardURL(ctx, row)
	if err != nil {
		return cardInput{}, err
	}
	return content, nil
}

// displayNames names the people a card mentions: a requester calling through
// their own session, and the decider.
func (c *Cards) displayNames(ctx context.Context, row repo.RiskPolicyApproval) (map[string]string, error) {
	ids := []string{}
	if id, ok := strings.CutPrefix(row.RequesterUrn, string(urn.PrincipalTypeUser)+":"); ok {
		ids = append(ids, id)
	}
	if id, ok := strings.CutPrefix(row.DecidedByUrn.String, string(urn.PrincipalTypeUser)+":"); ok && row.DecidedByUrn.Valid {
		ids = append(ids, id)
	}
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	members, err := repo.New(c.db).ListApprovalRecipientMembers(ctx, repo.ListApprovalRecipientMembersParams{OrganizationID: row.OrganizationID, AllMembers: false, UserIds: ids, MaxRows: conv.SafeInt32(len(ids))})
	if err != nil {
		return nil, fmt.Errorf("name approval people: %w", err)
	}
	for _, member := range members {
		names[member.UserID] = conv.Default(member.DisplayName, member.Email)
	}
	return names, nil
}

// memberEmail names a decider in the audit log the way dashboard decisions
// do, by email. Nil when the member cannot be found.
func (c *Cards) memberEmail(ctx context.Context, organizationID, userID string) *string {
	members, err := repo.New(c.db).ListApprovalRecipientMembers(ctx, repo.ListApprovalRecipientMembersParams{OrganizationID: organizationID, AllMembers: false, UserIds: []string{userID}, MaxRows: 1})
	if err != nil || len(members) == 0 {
		return nil
	}
	return &members[0].Email
}

// requesterName names who made the call. Agents have no display name here, so
// they read as a short ID, the way the dashboard shows them.
func requesterName(requesterURN string, names map[string]string) string {
	kind, id, ok := strings.Cut(requesterURN, ":")
	if !ok {
		return requesterURN
	}
	if kind == string(urn.PrincipalTypeUser) && names[id] != "" {
		return names[id]
	}
	if kind == "agent" {
		short, _, _ := strings.Cut(id, "-")
		return "Agent " + short
	}
	return requesterURN
}

// serverName names the MCP server the held call went to, or "" when the
// request did not record one or the server is gone.
func (c *Cards) serverName(ctx context.Context, row repo.RiskPolicyApproval) (string, error) {
	if !row.McpServerID.Valid {
		return "", nil
	}
	server, err := repo.New(c.db).GetApprovalServer(ctx, repo.GetApprovalServerParams{ID: row.McpServerID.UUID, ProjectID: row.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get approval server: %w", err)
	}
	return conv.Default(server.Name.String, server.Slug.String), nil
}

// dashboardURL opens the request on the Tool approvals tab.
func (c *Cards) dashboardURL(ctx context.Context, row repo.RiskPolicyApproval) (string, error) {
	if c.hosts == nil {
		return "", nil
	}
	slugs, err := repo.New(c.db).GetApprovalProjectSlugs(ctx, repo.GetApprovalProjectSlugsParams{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get approval project: %w", err)
	}
	siteURL := c.hosts.SiteURL(slugs.DefaultHost)
	if siteURL == nil {
		return "", nil
	}
	link := siteURL.JoinPath(slugs.OrganizationSlug, "projects", slugs.ProjectSlug, "risk-policies")
	link.RawQuery = url.Values{"tab": {"tool-approvals"}, "request": {row.ID.String()}}.Encode()
	return link.String(), nil
}

// retryable reports a Slack failure worth trying again: Slack's own errors,
// throttling and transport failures. A refusal the app's install or the
// request has to fix, such as a missing scope, is not.
func retryable(err error) bool {
	if slackErr, ok := errors.AsType[*slackapi.Error](err); ok {
		return !slackErr.ClientFault()
	}
	return true
}
