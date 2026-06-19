package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"slices"
)

// githubTriggerConfig is the instance config for the GitHub trigger. It
// exposes the shared webhook filter knobs — a CEL filter expression and an
// event-type allowlist that narrows the default-deny supportedGitHubEventTypes
// set. No vendor-specific configuration is required: the GitHub webhook shape
// is fixed, so everything realistically expressible is reachable through CEL
// over the typed event.
type githubTriggerConfig struct {
	FilterExpr string   `json:"filter,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`

	compiledFilter cel.Program
}

func (c githubTriggerConfig) Filter(event any) (bool, error) {
	githubEvent, ok := event.(githubTriggerEvent)
	if !ok {
		return false, fmt.Errorf("expected githubTriggerEvent, got %T", event)
	}
	return evalWebhookFilter(c.compiledFilter, c.EventTypes, event, githubEvent.EventType, supportedGitHubEventTypes)
}

// githubWebhookPayload is the shape GitHub delivers for every event. The
// fields surfaced here are the ones the trigger uses to derive event_type,
// correlation id, and routing metadata; the rest of the payload is carried
// verbatim as RawPayload on the event so CEL filters and the assistant can
// navigate vendor-specific shapes.
type githubWebhookPayload struct {
	Action      string             `json:"action,omitempty"`
	Number      int                `json:"number,omitempty"`
	Issue       *githubIssue       `json:"issue,omitempty"`
	PullRequest *githubPullRequest `json:"pull_request,omitempty"`
	Comment     *githubComment     `json:"comment,omitempty"`
	Review      *githubReview      `json:"review,omitempty"`
	Ref         string             `json:"ref,omitempty"`
	Repository  githubRepository   `json:"repository"`
}

type githubIssue struct {
	Number int `json:"number"`
	// PullRequest is present on the issue object only when the issue is
	// actually a pull request. GitHub attaches it to issue_comment payloads so
	// a PR comment can be told apart from a plain issue comment; presence is
	// the only signal we need, so the body is intentionally empty.
	PullRequest *githubIssuePullRequestRef `json:"pull_request,omitempty"`
}

type githubIssuePullRequestRef struct{}

type githubPullRequest struct {
	Number int `json:"number"`
}

type githubComment struct {
	CommitID string `json:"commit_id,omitempty"`
}

type githubReview struct {
	// Reviews on a PR don't repeat the PR number at the top level; the
	// pull_request sibling field carries it for review events.
}

type githubRepository struct {
	FullName string `json:"full_name"`
}

// githubTriggerEvent is the normalized event surfaced to CEL filters and
// downstream consumers. EventType is the value of the X-GitHub-Event header
// (e.g. push, pull_request, issues); Action is the verb GitHub attaches to
// action-bearing events (opened, closed, …). Repo carries the full_name of
// the repository the event originated from, and Ref/Number surface the
// event-specific routing key so correlation can route to the right assistant
// conversation without CEL re-parsing the raw payload.
type githubTriggerEvent struct {
	EventType  string          `json:"event_type" cel:"event_type"`
	Action     string          `json:"action,omitempty" cel:"action"`
	Repo       string          `json:"repo" cel:"repo"`
	Ref        string          `json:"ref,omitempty" cel:"ref"`
	Number     int             `json:"number,omitempty" cel:"number"`
	Payload    json.RawMessage `json:"payload,omitempty" cel:"payload"`
	ReceivedAt string          `json:"received_at,omitempty" cel:"received_at"`
}

// supportedGitHubEventTypes is the default-deny allowlist of X-GitHub-Event
// values the GitHub trigger accepts when an instance does not narrow
// `event_types`. It is the full set GitHub can deliver to a repository or
// organization webhook (see
// https://docs.github.com/webhooks/webhook-events-and-payloads) so any event a
// user subscribes to can be opted into; CEL filters narrow further on
// action/repo/number.
var supportedGitHubEventTypes = []string{
	"branch_protection_configuration",
	"branch_protection_rule",
	"check_run",
	"check_suite",
	"code_scanning_alert",
	"commit_comment",
	"create",
	"custom_property",
	"custom_property_values",
	"delete",
	"dependabot_alert",
	"deploy_key",
	"deployment",
	"deployment_protection_rule",
	"deployment_review",
	"deployment_status",
	"discussion",
	"discussion_comment",
	"fork",
	"github_app_authorization",
	"gollum",
	"installation",
	"installation_repositories",
	"installation_target",
	"issue_comment",
	"issue_dependencies",
	"issues",
	"label",
	"marketplace_purchase",
	"member",
	"membership",
	"merge_group",
	"meta",
	"milestone",
	"org_block",
	"organization",
	"package",
	"page_build",
	"personal_access_token_request",
	"ping",
	"project",
	"project_card",
	"project_column",
	"projects_v2",
	"projects_v2_item",
	"projects_v2_status_update",
	"public",
	"pull_request",
	"pull_request_review",
	"pull_request_review_comment",
	"pull_request_review_thread",
	"push",
	"registry_package",
	"release",
	"repository",
	"repository_advisory",
	"repository_dispatch",
	"repository_import",
	"repository_ruleset",
	"repository_vulnerability_alert",
	"secret_scanning_alert",
	"secret_scanning_alert_location",
	"secret_scanning_scan",
	"security_advisory",
	"security_and_analysis",
	"sponsorship",
	"star",
	"status",
	"sub_issues",
	"team",
	"team_add",
	"watch",
	"workflow_dispatch",
	"workflow_job",
	"workflow_run",
}

// githubWebhookSecretEnv is the environment variable holding the GitHub
// webhook secret. Declared as a constant so the name is referenced rather
// than inlined — gosec's G101 flags inline string literals containing
// "SECRET" as potential hardcoded credentials.
const githubWebhookSecretEnv = "GITHUB_WEBHOOK_SECRET" //nolint:gosec // env var name, not a credential

func newGitHubDefinition() Definition {
	schema := buildInputSchema[githubTriggerConfig](
		withArrayItemsEnum("event_types", toAnySlice(supportedGitHubEventTypes)...),
	)
	compiled := mustCompileSchema(schema)
	vendor := WebhookVendor{
		Slug:            DefinitionSlugGithub,
		Title:           "GitHub",
		Description:     "Receive GitHub webhooks and map them to Gram trigger events.",
		EventType:       reflect.TypeFor[githubTriggerEvent](),
		EnvRequirements: []EnvRequirement{{Name: githubWebhookSecretEnv, Description: "GitHub webhook secret used to verify webhook signatures.", Required: true}},
		SecretEnv:       githubWebhookSecretEnv,
		Signature: HMACScheme{
			NewHash:         func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
			Header:          "X-Hub-Signature-256",
			Encoding:        "hex",
			Prefix:          "sha256=",
			Template:        "{body}",
			TimestampHeader: "",
			TimestampSkew:   0,
		},
		SupportedEventTypes: supportedGitHubEventTypes,
		PreVerify:           nil,
		Ingest:              githubIngest,
	}
	return NewWebhookDefinition(vendor, schema, compiled, func(raw map[string]any) (Config, error) {
		cfg, err := decodeConfig[githubTriggerConfig](raw, compiled)
		if err != nil {
			return nil, err
		}
		for _, eventType := range cfg.EventTypes {
			if !slices.Contains(supportedGitHubEventTypes, eventType) {
				return nil, fmt.Errorf("unsupported github event type %q", eventType)
			}
		}
		prog, err := compileCELFilter(reflect.TypeFor[githubTriggerEvent](), cfg.FilterExpr)
		if err != nil {
			return nil, err
		}
		cfg.compiledFilter = prog
		return cfg, nil
	})
}

func githubIngest(body []byte, headers http.Header) (*WebhookIngest, error) {
	eventType := headers.Get("X-GitHub-Event")
	if eventType == "" {
		return nil, fmt.Errorf("missing X-GitHub-Event header")
	}

	// A webhook configured with the application/x-www-form-urlencoded content
	// type delivers the JSON under a `payload` form field rather than as the
	// raw body. Unwrap it so both GitHub content types ingest. The signature is
	// computed over the raw body in either case, so authentication has already
	// run against the untouched body.
	if strings.HasPrefix(strings.ToLower(headers.Get("Content-Type")), "application/x-www-form-urlencoded") {
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, fmt.Errorf("decode github form body: %w", err)
		}
		body = []byte(values.Get("payload"))
	}

	// GitHub ping events carry an empty body or a zen-only body; still
	// surface them so a ping can flow through CEL if desired, but treat a
	// missing body as an empty payload rather than failing the ingest.
	var payload githubWebhookPayload
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode github payload: %w", err)
		}
	}

	// X-GitHub-Delivery is the per-delivery UUID GitHub assigns; use it for
	// dedup so redeliveries of the same delivery collapse.
	eventID := headers.Get("X-GitHub-Delivery")

	repo := payload.Repository.FullName
	correlationID := githubCorrelationID(eventType, payload, repo)
	number := githubEventNumber(eventType, payload)
	ref := githubEventRef(payload)

	return &WebhookIngest{
		Response:      nil,
		EventID:       eventID,
		CorrelationID: correlationID,
		Event: githubTriggerEvent{
			EventType:  eventType,
			Action:     payload.Action,
			Repo:       repo,
			Ref:        ref,
			Number:     number,
			Payload:    body,
			ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}, nil
}

// githubEventNumber returns the integer routing key for the event when one
// applies (issue/PR/review number). 0 when the event has no number.
func githubEventNumber(eventType string, payload githubWebhookPayload) int {
	switch eventType {
	case "pull_request", "pull_request_review", "pull_request_review_comment", "pull_request_review_thread":
		if payload.PullRequest != nil {
			return payload.PullRequest.Number
		}
	case "issues", "issue_comment":
		if payload.Issue != nil {
			return payload.Issue.Number
		}
	}
	return payload.Number
}

// githubEventRef returns the branch ref for push events (e.g. "main" from
// "refs/heads/main") so correlation can route pushes to a per-branch
// conversation. Empty for non-push events.
func githubEventRef(payload githubWebhookPayload) string {
	if payload.Ref == "" {
		return ""
	}
	return strings.TrimPrefix(payload.Ref, "refs/heads/")
}

// githubCorrelationID routes each GitHub event to the assistant conversation
// that should own it. PRs fold all reviews/review comments onto the PR so the
// assistant sees the whole review thread as one context; issues fold comments
// onto the issue; pushes key on repo + branch so each branch is its own
// conversation; everything else keys on the repo so team-level events
// (stars, forks, members) don't fragment across per-event conversations.
func githubCorrelationID(eventType string, payload githubWebhookPayload, repo string) string {
	if repo == "" {
		repo = "unknown"
	}
	switch eventType {
	case "pull_request", "pull_request_review", "pull_request_review_comment", "pull_request_review_thread":
		if payload.PullRequest != nil {
			return "github:" + repo + "/pr:" + strconv.Itoa(payload.PullRequest.Number)
		}
	case "issues", "issue_comment":
		if payload.Issue != nil {
			// issue_comment fires for comments on both issues and pull requests.
			// When the issue carries a pull_request ref the comment is on a PR,
			// so fold it onto the PR's conversation instead of opening a separate
			// /issue: thread for the same number.
			if payload.Issue.PullRequest != nil {
				return "github:" + repo + "/pr:" + strconv.Itoa(payload.Issue.Number)
			}
			return "github:" + repo + "/issue:" + strconv.Itoa(payload.Issue.Number)
		}
	case "push":
		branch := githubEventRef(payload)
		if branch != "" {
			return "github:" + repo + "/branch:" + branch
		}
	case "commit_comment":
		if payload.Comment != nil && payload.Comment.CommitID != "" {
			return "github:" + repo + "/commit:" + payload.Comment.CommitID
		}
	}
	return "github:" + repo
}
