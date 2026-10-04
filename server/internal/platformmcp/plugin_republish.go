package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
)

const operationRepublishPlugin = "republish_plugin"

// Republish outcomes.
const (
	// PluginRepublishEnqueued means a publish of the project's packages was
	// requested. It has not necessarily landed yet.
	PluginRepublishEnqueued = "enqueued"
	// PluginRepublishAlreadyCurrent means the plugin's stored package inputs
	// already matched, so nothing was requested.
	PluginRepublishAlreadyCurrent = "already_current"
)

// pluginRepublishNote is returned with every successful result so an agent
// reports the asynchronous publish honestly.
const pluginRepublishNote = "Publishing runs in the background, so a requested publish is not yet a published package. Call get_plugin again later: publication_evidence.fresh=true confirms the package caught up. Installed clients are not inspected."

var (
	ErrPluginRepublishInvalid       = errors.New("invalid platform mcp plugin republish")
	ErrPluginRepublishUnavailable   = errors.New("platform mcp plugin republish unavailable")
	ErrPluginRepublishNotConfigured = errors.New("platform mcp plugin publishing not configured")
	ErrPluginRepublishConflict      = errors.New("platform mcp plugin republish conflict")
)

// PluginRepublishError is a bounded refusal from republish_plugin.
type PluginRepublishError struct {
	// Code is the stable refusal code returned to the caller.
	Code string

	// Message tells the agent what happened and what to do next.
	Message string

	// DashboardURL is where the administrator can finish the job, when known.
	DashboardURL string

	// Cause is the sentinel or internal error behind the refusal.
	Cause error
}

func (e *PluginRepublishError) Error() string { return e.Message }
func (e *PluginRepublishError) Unwrap() error { return e.Cause }

type RepublishPluginInput struct {
	ProjectID      string `json:"project_id" jsonschema:"explicit project ID that owns the plugin"`
	Plugin         string `json:"plugin" jsonschema:"exact plugin ID, slug, or name returned by list_plugins"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms republishing every plugin in this project"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated idempotency key, at most 128 characters; reuse only to retry this exact request"`
}

// RepublishedPlugin echoes the plugin the request resolved to.
type RepublishedPlugin struct {
	// ID is the resolved plugin ID.
	ID string `json:"id"`

	// Name is the administrator-facing plugin name.
	Name string `json:"name"`

	// Slug is the plugin's project-unique slug.
	Slug string `json:"slug"`

	// IsDefault marks the project's fallback plugin.
	IsDefault bool `json:"is_default"`
}

type RepublishPluginOutput struct {
	// ProjectID echoes the project whose packages were republished.
	ProjectID string `json:"project_id"`

	// Plugin is the plugin the request named.
	Plugin RepublishedPlugin `json:"plugin"`

	// Outcome is "enqueued" or "already_current".
	Outcome string `json:"outcome"`

	// PublicationEvidence is read after the request committed. Right after an
	// enqueue it normally still reports fresh=false.
	PublicationEvidence *PluginPublicationEvidence `json:"publication_evidence"`

	// Note states that the publish is asynchronous.
	Note string `json:"note"`

	// Receipt identifies the idempotent request.
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

// pluginRepublishReceipt is the replayable result stored with the receipt.
type pluginRepublishReceipt struct {
	Plugin      RepublishedPlugin `json:"plugin"`
	Outcome     string            `json:"outcome"`
	Publication string            `json:"publication_request"`
}

type normalizedRepublishPlugin struct {
	ProjectID string `json:"project_id"`
	PluginID  string `json:"plugin_id"`
}

// WithRepublish enables republish_plugin. Publication requests are the durable
// outbox path; the signaler is the debounced fallback used when emission is
// disabled. Either one is enough to request a publish.
func (s *PluginsService) WithRepublish(publication plugindelivery.PublicationRequests, publisher plugindelivery.PluginPublishSignaler, budget OperationBudget) *PluginsService {
	if s != nil {
		s.publication = publication
		s.publisher = publisher
		s.republishBudget = budget
	}
	return s
}

func (s *PluginsService) republishValid() bool {
	return s.valid() && s.publicationEvidence != nil && s.republishBudget.valid() && (s.publication.Enabled || s.publisher != nil)
}

// RepublishPlugin requests a publish of the project's plugin packages when the
// named plugin's package is stale. The request goes through the same
// fingerprint-gated, per-project publish the rollout sweep uses, so it
// regenerates every plugin in the project and a package whose inputs did not
// change is left as it is.
func (s *PluginsService) RepublishPlugin(ctx context.Context, principal Principal, input RepublishPluginInput) (RepublishPluginOutput, error) {
	if !s.republishValid() {
		return RepublishPluginOutput{}, pluginRepublishUnavailable(nil)
	}
	if !input.Confirmed {
		return RepublishPluginOutput{}, &PluginRepublishError{
			Code:         "confirmation_required",
			Message:      "Tell the user that this republishes every plugin in the project, not only the one named, ask them to explicitly confirm it, then call this tool again with confirmed: true.",
			DashboardURL: "",
			Cause:        ErrPluginRepublishInvalid,
		}
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if principal.UserID == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return RepublishPluginOutput{}, pluginRepublishInvalid("Supply an idempotency key of at most 128 characters.")
	}
	q := platformrepo.New(s.db)
	project, err := s.resolveProject(ctx, q, principal, input.ProjectID)
	if err != nil {
		return RepublishPluginOutput{}, err
	}
	target, err := s.resolve(ctx, q, principal, project.ID, input.Plugin)
	if err != nil {
		return RepublishPluginOutput{}, err
	}
	// Charged after the target is resolved and before anything is written, so
	// a refused call does not spend the allowance.
	if err := s.republishBudget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		return RepublishPluginOutput{}, err
	}

	payload, err := json.Marshal(normalizedRepublishPlugin{ProjectID: project.ID.String(), PluginID: target.ID.String()})
	if err != nil {
		return RepublishPluginOutput{}, pluginRepublishInvalid("The republish request could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-plugin-republish-v1\x00"), payload...))
	plugin := RepublishedPlugin{ID: target.ID.String(), Name: target.Name, Slug: target.Slug, IsDefault: target.IsDefault}

	receipt, err := executeMutationReceipt(ctx, mutationReceiptExecution[pluginRepublishReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operationRepublishPlugin,
		IdempotencyKey: input.IdempotencyKey, InputHash: hex.EncodeToString(digest[:]), Label: "plugin republish",
		Invalid: func(error) error { return pluginRepublishInvalid("The republish request is invalid.") },
		Conflict: func(message string) error {
			return &PluginRepublishError{Code: "conflict", Message: message, DashboardURL: "", Cause: ErrPluginRepublishConflict}
		},
		Unavailable: pluginRepublishUnavailable,
		ValidateReplay: func(stored []byte) bool {
			var result pluginRepublishReceipt
			return json.Unmarshal(stored, &result) == nil && (result.Outcome == PluginRepublishEnqueued || result.Outcome == PluginRepublishAlreadyCurrent)
		},
		EncodeResult: func(result pluginRepublishReceipt) ([]byte, error) {
			encoded, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("encode plugin republish receipt: %w", err)
			}
			return encoded, nil
		},
		Mutate: func(ctx context.Context, tx pgx.Tx) (pluginRepublishReceipt, error) {
			// Read inside the receipt transaction so a retry of a request that
			// already committed replays its stored outcome instead of
			// re-deciding from evidence that the publish itself has since moved.
			evidence := s.readPublicationEvidence(ctx, principal, project.ID, target.Slug)
			if evidence.NotConfigured {
				return pluginRepublishReceipt{}, s.pluginRepublishNotConfigured(ctx, principal, project)
			}
			if evidence.Fresh != nil && *evidence.Fresh {
				return pluginRepublishReceipt{Plugin: plugin, Outcome: PluginRepublishAlreadyCurrent, Publication: ""}, nil
			}
			// Unknown or unavailable evidence still requests a publish: the
			// publish is fingerprint-gated, so an unnecessary request is a no-op.
			outcome, err := s.publication.ProjectWithOutcome(ctx, tx, principal.OrganizationID, project.ID, principal.UserID)
			if err != nil {
				return pluginRepublishReceipt{}, fmt.Errorf("request plugin republish: %w", err)
			}
			if outcome == plugindelivery.ProjectPublicationNotConfigured {
				return pluginRepublishReceipt{}, s.pluginRepublishNotConfigured(ctx, principal, project)
			}
			if outcome == plugindelivery.ProjectPublicationEmissionDisabled && s.publisher == nil {
				return pluginRepublishReceipt{}, pluginRepublishUnavailable(errors.New("no publish path is composed"))
			}
			return pluginRepublishReceipt{Plugin: plugin, Outcome: PluginRepublishEnqueued, Publication: string(outcome)}, nil
		},
	})
	if err != nil {
		return RepublishPluginOutput{}, err
	}
	var stored pluginRepublishReceipt
	if err := json.Unmarshal(receipt.ResultPayload, &stored); err != nil {
		return RepublishPluginOutput{}, pluginRepublishUnavailable(err)
	}
	// A durable outbox request already covers the publish. Otherwise signal
	// the debounced publish now that the receipt committed; a replay signals
	// again, which the debounce collapses, so a retry after a failed signal
	// recovers.
	if stored.Outcome == PluginRepublishEnqueued {
		if err := plugindelivery.SignalPluginPublishAfterRequest(ctx, s.publisher, plugindelivery.ProjectPublicationRequestOutcome(stored.Publication), project.ID, principal.UserID); err != nil {
			return RepublishPluginOutput{}, pluginRepublishUnavailable(err)
		}
	}
	return RepublishPluginOutput{
		ProjectID:           project.ID.String(),
		Plugin:              stored.Plugin,
		Outcome:             stored.Outcome,
		PublicationEvidence: s.readPublicationEvidence(ctx, principal, project.ID, stored.Plugin.Slug),
		Note:                pluginRepublishNote,
		Receipt:             riskMutationToolReceipt(receipt),
	}, nil
}

func (s *PluginsService) pluginRepublishNotConfigured(ctx context.Context, principal Principal, project ResolvedProject) error {
	refusal := &PluginRepublishError{
		Code:         "not_configured",
		Message:      "This project has no package repository connected, so none of its plugins can be published. Connect one from the project's Plugins page in the AICP dashboard; nothing was requested.",
		DashboardURL: "",
		Cause:        ErrPluginRepublishNotConfigured,
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if s.dashboardURL != nil && ok && authCtx != nil && authCtx.ActiveOrganizationID == principal.OrganizationID && strings.TrimSpace(authCtx.OrganizationSlug) != "" {
		refusal.DashboardURL = requestDashboardURL(ctx, s.dashboardURL, s.serverURL).JoinPath(authCtx.OrganizationSlug, "projects", project.Slug, "plugins").String()
	}
	return refusal
}

func pluginRepublishInvalid(message string) error {
	return &PluginRepublishError{Code: "invalid_request", Message: message, DashboardURL: "", Cause: ErrPluginRepublishInvalid}
}

func pluginRepublishUnavailable(cause error) error {
	return &PluginRepublishError{
		Code:         unavailableCode,
		Message:      "A publish could not be requested from here right now. Nothing was published; try again shortly, or publish from the project's Plugins page in the AICP dashboard.",
		DashboardURL: "",
		Cause:        errors.Join(ErrPluginRepublishUnavailable, cause),
	}
}
