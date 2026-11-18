// Package slackapp serves the Speakeasy Slack app's Events API endpoint. It
// routes mentions of the app and direct messages to it to the managed
// assistant of the project the workspace answers for.
package slackapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

const (
	// EventsPath is the Speakeasy Slack app's Events API request URL.
	EventsPath = "/slack/events"

	maxEventBytes = 1 << 20 // 1 MiB

	// homeTimeout bounds loading and publishing a Home tab after the event
	// is acknowledged.
	homeTimeout = 30 * time.Second

	// managedAssistantActive is the status of an assistant that takes turns.
	managedAssistantActive = "active"

	// UnlinkedReply is posted, visible only to the sender, when a Slack user
	// who is not linked to a Speakeasy user messages the app.
	UnlinkedReply = "Your Slack account isn't linked to a Speakeasy user yet, so I can't act for you. Ask a Speakeasy organization admin to link your Slack account in Speakeasy, then try again."

	// DisabledReply is posted when the workspace's project has no active
	// project assistant.
	DisabledReply = "The project assistant isn't turned on for the Speakeasy project this workspace is connected to."
)

// Router finds the connected workspaces that route a Slack team's events.
type Router interface {
	RoutingWorkspacesForTeam(ctx context.Context, teamID string) ([]slackdirectoryconnections.RoutingWorkspace, error)
}

// Dispatcher hands a routed Slack event to the trigger pipeline.
type Dispatcher interface {
	ProcessSpeakeasySlack(ctx context.Context, instance triggerrepo.TriggerInstance, body []byte, headers http.Header, botToken string) (*triggers.WebhookIngressResult, error)
}

// Poster calls the Slack Web API with a bot token.
type Poster interface {
	CallWithToken(ctx context.Context, method string, payload map[string]any, token string) ([]byte, error)
}

// Events serves EventsPath.
type Events struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	router     Router
	dispatcher Dispatcher
	poster     Poster
	secret     string
	identities *assistantidentity.Service

	// dashboardURL is the Speakeasy dashboard the Home tab links to.
	dashboardURL string

	// home loads the Home tab's organization data. Nil publishes the static
	// welcome view.
	home *HomeSource

	// resolveSender finds the Speakeasy user a Slack sender is linked to.
	resolveSender func(context.Context, slackrepo.ResolveSlackMappingUserParams) (string, error)
}

// NewEvents returns the Events API handler. secret is the app's signing
// secret.
func NewEvents(logger *slog.Logger, db *pgxpool.Pool, router Router, dispatcher Dispatcher, poster Poster, secret string, identities *assistantidentity.Service) *Events {
	return &Events{logger: logger.With(attr.SlogComponent("slack_app_events")), db: db, router: router, dispatcher: dispatcher, poster: poster, secret: secret, identities: identities, dashboardURL: "", home: nil, resolveSender: slackrepo.New(db).ResolveSlackMappingUser}
}

func (e *Events) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEventBytes))
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := triggers.SlackSignature.Verify(body, r.Header, e.secret); err != nil {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	summary, err := triggers.SummarizeSlackEvent(body)
	if err != nil {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	if summary.Type == "url_verification" {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, summary.Challenge)
		return
	}
	if summary.Type == "event_callback" && summary.EventType == "app_home_opened" {
		// Acknowledge before reading anything: Slack wants the 200 within 3
		// seconds, and the Home tab goes out through views.publish.
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			e.logger.WarnContext(r.Context(), "acknowledge Speakeasy Slack app home early", attr.SlogError(err))
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), homeTimeout)
		defer cancel()
		e.publishHome(ctx, summary, body)
		return
	}
	if summary.Type == "event_callback" && summary.Addressed() {
		if err := e.route(r.Context(), summary, body, r.Header); err != nil {
			e.logger.ErrorContext(r.Context(), "route Speakeasy Slack app event", attr.SlogError(err))
			// Slack retries non-2xx responses; the dispatch is deduplicated
			// on the event id, so a retry is safe.
			http.Error(w, "event not routed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// route sends an addressed event to the managed assistant of the project its
// workspace answers for, acting as the sender's linked Speakeasy user.
func (e *Events) route(ctx context.Context, summary triggers.SlackEventSummary, body []byte, headers http.Header) error {
	workspaces, err := e.router.RoutingWorkspacesForTeam(ctx, summary.TeamID)
	if err != nil {
		if errors.Is(err, slackdirectoryconnections.ErrNoRoutingWorkspace) {
			e.logger.WarnContext(ctx, "Speakeasy Slack app workspace cannot post", attr.SlogError(err))
			return nil
		}
		return fmt.Errorf("find routing workspace: %w", err)
	}
	if len(workspaces) == 0 {
		return nil
	}

	// The same Slack workspace can be connected by several organizations.
	// The event goes to the first one the sender is linked in.
	workspace, linked, err := e.linkedWorkspace(ctx, workspaces, summary)
	if err != nil {
		return err
	}
	if !linked {
		e.reply(ctx, workspace.Token, summary, UnlinkedReply)
		return nil
	}

	managed, err := assistantrepo.New(e.db).GetManagedAssistantByProject(ctx, workspace.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && managed.Status != managedAssistantActive) {
		e.reply(ctx, workspace.Token, summary, DisabledReply)
		return nil
	}
	if err != nil {
		return fmt.Errorf("get managed assistant: %w", err)
	}

	instance, err := e.triggerInstance(ctx, managed)
	if err != nil {
		return err
	}
	if _, err := e.dispatcher.ProcessSpeakeasySlack(ctx, instance, body, headers, workspace.Token); err != nil {
		return fmt.Errorf("dispatch Speakeasy Slack app event: %w", err)
	}
	return nil
}

func (e *Events) linkedWorkspace(ctx context.Context, workspaces []slackdirectoryconnections.RoutingWorkspace, summary triggers.SlackEventSummary) (slackdirectoryconnections.RoutingWorkspace, bool, error) {
	for _, workspace := range workspaces {
		userID, err := e.resolveSender(ctx, slackrepo.ResolveSlackMappingUserParams{
			OrganizationID: workspace.OrganizationID,
			SlackTeamID:    summary.TeamID,
			SlackUserID:    summary.UserID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return slackdirectoryconnections.RoutingWorkspace{}, false, fmt.Errorf("resolve Slack sender: %w", err)
		}
		if userID != "" {
			return workspace, true, nil
		}
	}
	return workspaces[0], false, nil
}

// triggerInstance returns the managed assistant's speakeasy_slack trigger,
// creating it on first use.
func (e *Events) triggerInstance(ctx context.Context, managed assistantrepo.GetManagedAssistantByProjectRow) (triggerrepo.TriggerInstance, error) {
	if e.identities == nil {
		return triggerrepo.TriggerInstance{}, errors.New("the Speakeasy Slack app trigger identities are not configured")
	}
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("begin Slack trigger transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := triggerrepo.New(tx)
	lookup := triggerrepo.GetSpeakeasySlackTriggerInstanceParams{ProjectID: managed.ProjectID, TargetKind: triggers.TargetKindAssistant, TargetRef: managed.ID.String()}
	instance, err := queries.GetSpeakeasySlackTriggerInstance(ctx, lookup)
	if errors.Is(err, pgx.ErrNoRows) {
		instance, err = queries.CreateSpeakeasySlackTriggerInstance(ctx, triggerrepo.CreateSpeakeasySlackTriggerInstanceParams{
			OrganizationID: managed.OrganizationID,
			ProjectID:      managed.ProjectID,
			Name:           "Speakeasy Slack app",
			TargetKind:     triggers.TargetKindAssistant,
			TargetRef:      managed.ID.String(),
			TargetDisplay:  managed.Name,
			ConfigJson:     triggers.SpeakeasySlackConfig,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// A concurrent event created it first.
			instance, err = queries.GetSpeakeasySlackTriggerInstance(ctx, lookup)
		}
	}
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("get or create Slack trigger: %w", err)
	}
	// Binding locks the trigger and is idempotent. Existing unbound rows
	// are repaired before dispatch, in the same transaction as creation.
	if err := e.identities.BindRootTrigger(ctx, tx, managed.ProjectID, instance.ID); err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("bind Slack trigger identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("commit Slack trigger transaction: %w", err)
	}
	return instance, nil
}

// reply tells the sender why the app will not answer. Best-effort.
func (e *Events) reply(ctx context.Context, token string, summary triggers.SlackEventSummary, text string) {
	if e.poster == nil || token == "" {
		return
	}
	payload := map[string]any{"channel": summary.ChannelID, "user": summary.UserID, "text": text}
	if summary.ThreadTS != "" {
		payload["thread_ts"] = summary.ThreadTS
	}
	if _, err := e.poster.CallWithToken(ctx, "chat.postEphemeral", payload, token); err != nil {
		e.logger.WarnContext(ctx, "reply to Speakeasy Slack app sender", attr.SlogError(err))
	}
}
