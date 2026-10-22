//nolint:exhaustruct // Refusals, results and repository literals only set fields applicable to the outcome.
package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const (
	operationCreateMCPFromFunctions = "create_mcp_from_functions"

	// maxMCPNameLength is the toolset name limit the schema enforces. The
	// server is named after its toolset, exactly as the dashboard names it, so
	// the stricter of the two limits is the one a caller is held to.
	maxMCPNameLength = 60
)

// CreateMCPFromFunctionsInput authors a new hosted MCP server whose tools are
// tools a project's functions already produced.
type CreateMCPFromFunctionsInput struct {
	ProjectID      string   `json:"project_id" jsonschema:"project ID the new MCP server belongs to"`
	Name           string   `json:"name" jsonschema:"display name for the new MCP server; at most 60 characters"`
	ToolURNs       []string `json:"tool_urns" jsonschema:"exact function tool URNs from list_project_tools; at most 50"`
	IdempotencyKey string   `json:"idempotency_key" jsonschema:"caller-chosen key that makes a retry of this exact creation safe"`
	Confirmed      bool     `json:"confirmed" jsonschema:"true only after the user confirmed this exact project, name, and tool list"`
}

// CreateMCPFromFunctionsOutput reports the server that now exists. A refusal
// is returned as an error result and never as this struct.
type CreateMCPFromFunctionsOutput struct {
	Outcome string `json:"outcome"`
	MCPID   string `json:"mcp_id"`
	MCPName string `json:"mcp_name"`
	MCPSlug string `json:"mcp_slug"`
	// Visibility is the server record's own visibility, which is private.
	Visibility string `json:"visibility"`
	// AddedToDefaultPlugin reports what the creation actually did, read from
	// the attach itself: true when the new tool list joined the project's
	// Default plugin, which happens to an organization's first server exactly
	// as it does from the dashboard. Everyone holding the Default plugin then
	// receives it. False means the server reaches nobody until it is put into
	// a plugin.
	AddedToDefaultPlugin bool `json:"added_to_default_plugin"`
	// PublicationRequested is true when a refresh of the plugins people hold
	// was actually requested — durably or best effort — for that membership
	// change. It says a refresh was asked for, not that people have it yet.
	PublicationRequested bool `json:"publication_requested"`
	// Exposure is a fresh read taken after the commit, so a caller reports the
	// committed tool list rather than the one it asked for.
	Exposure      *MCPToolExposure `json:"exposure,omitempty"`
	SnapshotScope string           `json:"snapshot_scope"`
	// PublicationRequest and PublishSignal detail how that refresh was asked
	// for; both stay at their not-requested values when nothing joined a
	// plugin.
	PublicationRequest string `json:"publication_request"`
	PublishSignal      string `json:"publish_signal"`
	// IndexSignal has the meaning it has on add_tools_to_mcp: whether the
	// tool-search index rebuild the new tool list needs was scheduled.
	IndexSignal string                  `json:"index_signal"`
	Receipt     RiskMutationToolReceipt `json:"receipt"`
}

type mcpFromFunctionsReceipt struct {
	MCPID      string   `json:"mcp_id"`
	MCPName    string   `json:"mcp_name"`
	MCPSlug    string   `json:"mcp_slug"`
	Visibility string   `json:"visibility"`
	ToolsetID  string   `json:"toolset_id"`
	ToolURNs   []string `json:"tool_urns"`
	// AddedToDefaultPlugin is the attach's own outcome, recorded so a replay
	// reports who receives the server exactly as the original did.
	AddedToDefaultPlugin bool `json:"added_to_default_plugin"`
	// Publication is recorded in the transaction that requested it, so a
	// replay reports the same outcome rather than re-deciding it.
	Publication string `json:"publication_request"`
}

type mcpFromFunctionsRequest struct {
	ProjectID string   `json:"project_id"`
	Name      string   `json:"name"`
	ToolURNs  []string `json:"tool_urns"`
}

// CreateMCPFromFunctions creates the toolset and the server record in front of
// it in one transaction, through the same two cores the dashboard's
// createToolset and createMcpServer calls run, then schedules the search index
// the new tool list needs. A retry with the same idempotency key returns the
// stored result and never creates a second server.
func (s *MCPToolExposureService) CreateMCPFromFunctions(ctx context.Context, principal Principal, input CreateMCPFromFunctionsInput) (CreateMCPFromFunctionsOutput, error) {
	output, err := s.createMCPFromFunctions(ctx, principal, input)
	if err != nil {
		return CreateMCPFromFunctionsOutput{}, s.sanitizeMCPFromFunctionsError(ctx, err)
	}
	return output, nil
}

// sanitizeMCPFromFunctionsError is the boundary every error crosses on its way
// to the MCP caller. Nothing downstream of the tool rewrites an error it does
// not recognise — the SDK returns it to the client as text — so a database or
// driver failure from any path, including the shared toolset and server cores,
// would otherwise reach the caller verbatim. Only a hand-written refusal or an
// authorization challenge passes through; anything else becomes the generic
// unavailable refusal, and the underlying cause is logged here, server-side,
// rather than the sanitized message.
func (s *MCPToolExposureService) sanitizeMCPFromFunctionsError(ctx context.Context, err error) error {
	if _, ok := errors.AsType[*ExternalAuthorizationError](err); ok {
		return err
	}
	refusal, ok := errors.AsType[*MCPToolExposureError](err)
	if !ok {
		refusal, _ = errors.AsType[*MCPToolExposureError](mcpFromFunctionsUnavailable(err))
	}
	if refusal.Code == unavailableCode && s != nil && s.logger != nil {
		s.logger.ErrorContext(ctx, "create MCP server from functions failed", attr.SlogError(err))
	}
	return refusal
}

func (s *MCPToolExposureService) createMCPFromFunctions(ctx context.Context, principal Principal, input CreateMCPFromFunctionsInput) (CreateMCPFromFunctionsOutput, error) {
	if !s.valid() {
		return CreateMCPFromFunctionsOutput{}, mcpFromFunctionsUnavailable(errors.New("tool exposure service is not composed"))
	}
	projectID, name, requested, err := validateMCPFromFunctions(input)
	if err != nil {
		return CreateMCPFromFunctionsOutput{}, err
	}
	project, err := s.resolveCreateTarget(ctx, principal, projectID)
	if err != nil {
		return CreateMCPFromFunctionsOutput{}, err
	}
	// An unconfirmed call is the preview: it reports the slugs this exact name
	// becomes, computed by the same functions the creation uses, and charges
	// and writes nothing. The slugs are never described in prose anywhere, so
	// what the user confirms is what gets created.
	if !input.Confirmed {
		// The same count the toolset core uses to decide the auto-enable.
		enabled, err := usagerepo.New(s.db).GetEnabledServerCount(ctx, principal.OrganizationID)
		if err != nil {
			return CreateMCPFromFunctionsOutput{}, mcpFromFunctionsUnavailable(err)
		}
		return CreateMCPFromFunctionsOutput{}, mcpFromFunctionsConfirmationRequired(name, enabled == 0)
	}
	organizationSlug := projectOrganizationSlug(ctx, s.db, principal.OrganizationID)
	if organizationSlug == "" {
		return CreateMCPFromFunctionsOutput{}, mcpFromFunctionsUnavailable(errors.New("organization slug unavailable"))
	}

	payload, err := json.Marshal(mcpFromFunctionsRequest{ProjectID: project.ID.String(), Name: name, ToolURNs: toolURNStrings(requested)})
	if err != nil {
		return CreateMCPFromFunctionsOutput{}, mcpFromFunctionsInvalid("The MCP server request could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-create-from-functions-v1\x00"), payload...))
	inputHash := hex.EncodeToString(digest[:])

	// A retry of a creation that already committed replays its stored result
	// without spending the allowance; see executeChargedMutationReceipt. The
	// allowance is the one the other writes that change what a server exposes
	// share, so a creation loop cannot outrun them.
	charge := func(ctx context.Context) error {
		if err := s.changes.AllowConnectionOrOrganization(ctx, principal); err != nil {
			return mcpFromFunctionsBudgetError(err)
		}
		return nil
	}
	receipt, err := executeChargedMutationReceipt(ctx, charge, mutationReceiptExecution[mcpFromFunctionsReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operationCreateMCPFromFunctions,
		IdempotencyKey: input.IdempotencyKey, InputHash: inputHash, Label: "MCP server from functions",
		Invalid: func(error) error { return mcpFromFunctionsInvalid("The MCP server request is invalid.") },
		Conflict: func(message string) error {
			return &MCPToolExposureError{Code: "conflict", Message: message, Cause: ErrMCPToolExposureConflict}
		},
		Unavailable: mcpFromFunctionsUnavailable,
		ValidateReplay: func(stored []byte) bool {
			var result mcpFromFunctionsReceipt
			return json.Unmarshal(stored, &result) == nil && result.MCPID != "" && result.ToolsetID != ""
		},
		EncodeResult: func(result mcpFromFunctionsReceipt) ([]byte, error) {
			encoded, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("encode MCP server from functions receipt: %w", err)
			}
			return encoded, nil
		},
		Mutate: func(ctx context.Context, tx pgx.Tx) (mcpFromFunctionsReceipt, error) {
			return s.createMCPFromFunctionsInTransaction(ctx, tx, principal, project, organizationSlug, name, requested)
		},
	})
	if err != nil {
		return CreateMCPFromFunctionsOutput{}, err
	}
	var stored mcpFromFunctionsReceipt
	if err := json.Unmarshal(receipt.ResultPayload, &stored); err != nil {
		return CreateMCPFromFunctionsOutput{}, mcpFromFunctionsUnavailable(err)
	}
	return s.finishMCPFromFunctions(ctx, principal, project, stored, receipt, charge), nil
}

func (s *MCPToolExposureService) createMCPFromFunctionsInTransaction(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, organizationSlug, name string, requested []urn.Tool) (mcpFromFunctionsReceipt, error) {
	txQueries := s.queries.WithTx(tx)
	// Held to commit, so the project cannot be deleted between the tool check
	// and the two creates.
	if _, err := txQueries.LockLivePlatformMCPProjectForRegistration(ctx, platformrepo.LockLivePlatformMCPProjectForRegistrationParams{
		ProjectID: project.ID, OrganizationID: principal.OrganizationID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return mcpFromFunctionsReceipt{}, mcpFromFunctionsProjectMissing()
	} else if err != nil {
		return mcpFromFunctionsReceipt{}, fmt.Errorf("lock project for MCP server from functions: %w", err)
	}
	// Checked here rather than before the receipt so a replay of a creation
	// that already committed returns its result even after a later deployment
	// stopped producing one of these tools.
	if err := s.requireDeployedTools(ctx, txQueries, principal, project.ID, requested); err != nil {
		return mcpFromFunctionsReceipt{}, err
	}

	values := toolURNStrings(requested)
	// Attributed to the user by id, as add_tools_to_mcp attributes its toolset
	// audit entry; the platform principal carries no display email.
	var email *string
	created, err := toolsets.CreateToolsetInTransaction(ctx, tx, s.logger, s.audit, toolsets.ToolsetCreateInput{
		OrganizationID: principal.OrganizationID, OrganizationSlug: organizationSlug, ProjectID: project.ID,
		ActorUserID: principal.UserID, ActorEmail: email, Name: name, Description: nil,
		DefaultEnvironmentSlug: nil, OriginRegistrySpecifier: nil,
		ToolURNs: values, ResourceURNs: nil,
	})
	if err != nil {
		return mcpFromFunctionsReceipt{}, classifyMCPFromFunctionsError(err)
	}
	// The dashboard names the server after the toolset it just created and
	// makes it private; so does this. The toolset id comes from the row this
	// transaction wrote, and the project-checked core refuses anything outside
	// this project, so the server and its toolset share a project by
	// construction rather than by convention.
	server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, s.audit, mcpservers.MCPServerTransactionInput{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		ActorUserID: principal.UserID, ActorEmail: email,
		Name: created.Toolset.Name, Visibility: mcpservers.VisibilityPrivate,
		NetworkAccessMode: networkaccess.ModePublicOnly,
		ToolsetID:         uuid.NullUUID{UUID: created.Toolset.ID, Valid: true},
	})
	if err != nil {
		return mcpFromFunctionsReceipt{}, classifyMCPFromFunctionsError(err)
	}

	result := mcpFromFunctionsReceipt{
		MCPID: server.ID.String(), MCPName: conv.PtrValOr(conv.FromPGText[string](server.Name), created.Toolset.Name),
		MCPSlug: conv.PtrValOr(conv.FromPGText[string](server.Slug), ""), Visibility: server.Visibility,
		ToolsetID: created.Toolset.ID.String(), ToolURNs: values,
		AddedToDefaultPlugin: created.AddedToDefaultPlugin,
		Publication:          "not_requested",
	}
	// Only an organization's first server joins the Default plugin, and only
	// that membership change owes a publish — the management API's rule. It is
	// decided from what the attach did, not from the auto-enable that led to it.
	if created.AddedToDefaultPlugin {
		outcome, err := s.publication.ProjectWithOutcome(ctx, tx, principal.OrganizationID, project.ID, principal.UserID)
		if err != nil {
			return mcpFromFunctionsReceipt{}, fmt.Errorf("request MCP server from functions publication: %w", err)
		}
		result.Publication = string(outcome)
	}
	return result, nil
}

func (s *MCPToolExposureService) finishMCPFromFunctions(ctx context.Context, principal Principal, project ResolvedProject, stored mcpFromFunctionsReceipt, receipt OperationReceipt, charge func(context.Context) error) CreateMCPFromFunctionsOutput {
	output := CreateMCPFromFunctionsOutput{
		Outcome: "created", MCPID: stored.MCPID, MCPName: stored.MCPName, MCPSlug: stored.MCPSlug, Visibility: stored.Visibility,
		AddedToDefaultPlugin: stored.AddedToDefaultPlugin,
		PublicationRequest:   stored.Publication, PublishSignal: "not_requested", IndexSignal: "not_required",
		Receipt: riskMutationToolReceipt(receipt),
	}
	// Signalled for every outcome but enqueued, which is the one case
	// SignalPluginPublishAfterRequest itself skips. not_configured in
	// particular still needs it: with emission enabled it means the project
	// has no marketplace connection yet, and the publish can create that first
	// repository, exactly as the dashboard's first-server path does.
	rerunErr := chargeRerun(ctx, receipt, charge)
	if stored.AddedToDefaultPlugin && stored.Publication != string(plugins.ProjectPublicationEnqueued) {
		if rerunErr != nil {
			output.PublishSignal = skippedRerun(ctx, s.logger, rerunErr)
		} else if s.publisher == nil {
			output.PublishSignal = "unavailable"
		} else if err := plugins.SignalPluginPublishAfterRequest(ctx, s.publisher, plugins.ProjectPublicationRequestOutcome(stored.Publication), project.ID, principal.UserID); err != nil {
			output.PublishSignal = "request_failed"
		} else {
			output.PublishSignal = "best_effort_requested"
		}
	}
	output.PublicationRequested = stored.AddedToDefaultPlugin &&
		(stored.Publication == string(plugins.ProjectPublicationEnqueued) || output.PublishSignal == "best_effort_requested")
	// Creating the toolset's first version left it without a search index, and
	// a dynamic-mode server refuses tools/list outright until one exists. The
	// target is the toolset recorded in the receipt, so a replay schedules the
	// toolset the original wrote.
	if rerunErr != nil {
		output.IndexSignal = skippedRerun(ctx, s.logger, rerunErr)
	} else {
		output.IndexSignal = s.scheduleIndex(ctx, project.ID, stored.ToolsetID)
	}

	mcpID, err := uuid.Parse(stored.MCPID)
	if err != nil {
		output.SnapshotScope = "verification_unavailable"
		return output
	}
	exposure, err := s.Exposure(ctx, principal, project.ID, mcpID)
	if err != nil {
		output.SnapshotScope = "verification_unavailable"
		return output
	}
	output.Exposure = &exposure
	output.SnapshotScope = "fresh_read_after_commit"
	return output
}

// MCPFromFunctionsPreview is what an unconfirmed create_mcp_from_functions
// call reports: the slugs the confirmed call will create for this exact name.
type MCPFromFunctionsPreview struct {
	Name string `json:"name"`
	// ToolsetSlug is the exact slug of the tool list behind the new server.
	// It is unique in the project, so a name whose slug is taken is refused.
	ToolsetSlug string `json:"toolset_slug"`
	// MCPSlugPrefix is the exact start of the new server's slug. A short
	// suffix from the server's own identifier is added at creation, so it
	// cannot be shown here; the result reports the full slug.
	MCPSlugPrefix string `json:"mcp_slug_prefix"`
	// WouldJoinDefaultPlugin is true when the organization has no MCP server
	// enabled yet, so this one would be its first: it then joins the project's
	// Default plugin on creation and everyone holding that plugin receives it.
	// It is read now and can change before the confirmed call; the result's
	// added_to_default_plugin is what actually happened.
	WouldJoinDefaultPlugin bool `json:"would_join_default_plugin"`
}

func mcpFromFunctionsPreview(name string, wouldJoinDefaultPlugin bool) *MCPFromFunctionsPreview {
	return &MCPFromFunctionsPreview{
		Name: name, ToolsetSlug: toolsets.ToolsetSlugFromName(name), MCPSlugPrefix: mcpservers.ServerSlugPrefix(name),
		WouldJoinDefaultPlugin: wouldJoinDefaultPlugin,
	}
}

func mcpFromFunctionsConfirmationRequired(name string, wouldJoinDefaultPlugin bool) error {
	return &MCPToolExposureError{
		Code:    "confirmation_required",
		Message: "Nothing was created. Show the user the project, the server name, the preview below, who will receive the server, and the exact tools, and once they confirm call again with the same request, the same idempotency key, and confirmed: true.",
		Cause:   ErrMCPToolExposureInvalid,
		Preview: mcpFromFunctionsPreview(name, wouldJoinDefaultPlugin),
	}
}

func validateMCPFromFunctions(input CreateMCPFromFunctionsInput) (uuid.UUID, string, []urn.Tool, error) {
	key := strings.TrimSpace(input.IdempotencyKey)
	if key == "" || len(key) > 128 {
		return uuid.Nil, "", nil, mcpFromFunctionsInvalid("Provide a stable idempotency key so retrying this exact creation is safe.")
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return uuid.Nil, "", nil, mcpFromFunctionsInvalid("Provide an explicit project ID.")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxMCPNameLength || conv.ToSlug(name) == "" {
		return uuid.Nil, "", nil, mcpFromFunctionsInvalid(fmt.Sprintf("Provide a server name of at most %d characters that includes at least one letter or digit.", maxMCPNameLength))
	}
	if len(input.ToolURNs) == 0 {
		return uuid.Nil, "", nil, mcpFromFunctionsInvalid("Name at least one tool.")
	}
	if len(input.ToolURNs) > maxToolExposureBatch {
		return uuid.Nil, "", nil, mcpFromFunctionsInvalid(fmt.Sprintf("Name at most %d tools in one creation.", maxToolExposureBatch))
	}
	seen := make(map[string]bool, len(input.ToolURNs))
	requested := make([]urn.Tool, 0, len(input.ToolURNs))
	var malformed, notFunctions []string
	for _, value := range input.ToolURNs {
		trimmed := strings.TrimSpace(value)
		parsed, err := urn.ParseTool(trimmed)
		if err != nil {
			malformed = append(malformed, trimmed)
			continue
		}
		if parsed.Kind != urn.ToolKindFunction {
			notFunctions = append(notFunctions, trimmed)
			continue
		}
		if seen[parsed.String()] {
			continue
		}
		seen[parsed.String()] = true
		requested = append(requested, parsed)
	}
	if len(malformed) > 0 {
		return uuid.Nil, "", nil, &MCPToolExposureError{
			Code: "invalid_request", UnknownTools: malformed, Cause: ErrMCPToolExposureInvalid,
			Message: "Some of the named tools are not tool identifiers. Use the exact tool URNs from the project's tool list.",
		}
	}
	if len(notFunctions) > 0 {
		return uuid.Nil, "", nil, &MCPToolExposureError{
			Code: "invalid_request", UnknownTools: notFunctions, Cause: ErrMCPToolExposureInvalid,
			Message: "Some of the named tools were not produced by a function, so nothing was created. A server can be made here only from function tools; one built from an API document has to be made in the dashboard.",
		}
	}
	return projectID, name, requested, nil
}

// resolveCreateTarget mirrors the dashboard's create checks and their order:
// live org:admin, then mcp:write keyed on the project — the resource id the
// management API's createToolset and createMcpServer both check, since the
// server does not exist yet — and only then the project lookup, so a caller
// without the grant cannot tell a real project from an invented one.
func (s *MCPToolExposureService) resolveCreateTarget(ctx context.Context, principal Principal, projectID uuid.UUID) (ResolvedProject, error) {
	if principal.OrganizationID == "" || principal.UserID == "" {
		return ResolvedProject{}, mcpFromFunctionsInvalid("The MCP server request is missing its caller identity.")
	}
	if err := s.admin.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return ResolvedProject{}, toolExposureAdminAuthorizationError(err)
	}
	if err := s.engine.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, projectID.String(), projectID.String())); err != nil {
		mapped := toolExposureAuthorizationError(err, authz.ScopeMCPWrite)
		if _, ok := errors.AsType[*ExternalAuthorizationError](mapped); !ok {
			return ResolvedProject{}, mcpFromFunctionsUnavailable(err)
		}
		return ResolvedProject{}, mapped
	}
	project, err := s.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedProject{}, mcpFromFunctionsProjectMissing()
	}
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("resolve live MCP server from functions project: %w", err)
	}
	return ResolvedProject{ID: project.ID, Name: project.Name, Slug: project.Slug}, nil
}

// requireDeployedTools is requireKnownTools for a server that does not exist
// yet: with no exposed list to fall back on, every tool must be produced by the
// project's latest completed deployment, and every one that is not is named.
func (s *MCPToolExposureService) requireDeployedTools(ctx context.Context, queries *platformrepo.Queries, principal Principal, projectID uuid.UUID, requested []urn.Tool) error {
	if err := s.requireKnownTools(ctx, queries, principal, projectID, requested, nil); err != nil {
		var refusal *MCPToolExposureError
		if errors.As(err, &refusal) && len(refusal.UnknownTools) > 0 {
			return &MCPToolExposureError{
				Code: refusal.Code, UnknownTools: refusal.UnknownTools, Cause: refusal.Cause,
				Message: "Some of the named tools are not produced by this project's latest deployment, so nothing was created. List the project's tools again; a tool pushed after that deployment finished is not available until the new one completes.",
			}
		}
		return err
	}
	return nil
}

func classifyMCPFromFunctionsError(err error) error {
	if errors.Is(err, toolsets.ErrToolsetSlugTaken) {
		return &MCPToolExposureError{
			Code:    "name_taken",
			Message: "This project already has an MCP server tool list with that name, so nothing was created. Choose a different name and confirm it with the user.",
			Cause:   err,
		}
	}
	if errors.Is(err, mcpservers.ErrServerReferenceOutsideProject) {
		return mcpFromFunctionsUnavailable(err)
	}
	// A server slug's short uniqueness suffix can collide within a project;
	// the management API reports that as a conflict, and so does this.
	pgErr, isPG := errors.AsType[*pgconn.PgError](err)
	if shareable, ok := errors.AsType[*oops.ShareableError](err); (ok && shareable.Code == oops.CodeConflict) || (isPG && pgErr.Code == pgerrcode.UniqueViolation) {
		return &MCPToolExposureError{
			Code:    "conflict",
			Message: "The MCP server could not be created because something it depends on changed at the same moment, so nothing was created. Try again with a fresh idempotency key.",
			Cause:   err,
		}
	}
	return mcpFromFunctionsUnavailable(err)
}

func mcpFromFunctionsBudgetError(err error) error {
	if errors.Is(err, ErrOperationRateLimited) {
		return &MCPToolExposureError{Code: "rate_limited", Message: "Creating MCP servers was asked for too often just now. Try again shortly.", Cause: err}
	}
	return mcpFromFunctionsUnavailable(err)
}

func mcpFromFunctionsInvalid(message string) error {
	return &MCPToolExposureError{Code: "invalid_request", Message: message, Cause: ErrMCPToolExposureInvalid}
}

func mcpFromFunctionsProjectMissing() error {
	return &MCPToolExposureError{
		Code:    "not_found",
		Message: "That project is not one an MCP server can be created in from here. List the projects again and choose one exactly.",
		Cause:   ErrMCPToolExposureMissing,
	}
}

func mcpFromFunctionsUnavailable(cause error) error {
	return &MCPToolExposureError{
		Code:    unavailableCode,
		Message: "Creating an MCP server from a project's functions is temporarily unavailable.",
		Cause:   errors.Join(ErrUnavailable, cause),
	}
}
