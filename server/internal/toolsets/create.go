//nolint:exhaustruct // The audit actor carries only the identity a toolset creation records.
package toolsets

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usageRepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// ToolsetCreateInput is what one toolset creation needs once its caller has
// authorized it. The caller owns the transaction and the authorization; this
// owns every write and every rule a new toolset is subject to.
type ToolsetCreateInput struct {
	OrganizationID          string
	OrganizationSlug        string
	ProjectID               uuid.UUID
	ActorUserID             string
	ActorEmail              *string
	Name                    string
	Description             *string
	DefaultEnvironmentSlug  *string
	OriginRegistrySpecifier *string
	// ToolURNs and ResourceURNs seed the first version. Nil for both creates
	// no version at all, exactly as the management API always has.
	ToolURNs     []string
	ResourceURNs []string
}

// ToolsetCreateResult reports what the creation decided as well as what it
// wrote, because two of those decisions have consequences the caller has to act
// on after commit: an MCP-enabled toolset joined the Default plugin, so a
// publish is owed, and pluginCreated means that plugin did not exist before.
type ToolsetCreateResult struct {
	Toolset    repo.Toolset
	McpEnabled bool
	// AddedToDefaultPlugin is what the attach actually did, not what the
	// auto-enable implies: true only when the toolset joined the project's
	// Default plugin in this transaction.
	AddedToDefaultPlugin bool
	PluginCreated        bool
}

// ErrToolsetSlugTaken is the toolset's derived slug colliding with a live
// toolset in the same project. It is carried inside the oops conflict the
// management API has always returned, so a second caller can tell it apart
// from any other conflict without matching on message text.
var ErrToolsetSlugTaken = errors.New("toolset slug already exists")

// ErrToolsetInputInvalid marks a name or description the toolsets columns
// cannot hold, refused before anything is written.
var ErrToolsetInputInvalid = errors.New("invalid toolset input")

// The limits the toolsets table's CHECK constraints enforce.
const (
	MaxToolsetNameLength        = 60
	MaxToolsetSlugLength        = 60
	MaxToolsetDescriptionLength = 250
)

// ToolsetSlugFromName derives a toolset's slug from its name and cuts it to
// the column limit with trailing hyphens trimmed. The name itself is never
// shortened; only the derived slug is.
func ToolsetSlugFromName(name string) string {
	slug := conv.ToSlug(name)
	if len(slug) > MaxToolsetSlugLength {
		// ToSlug keeps only ASCII letters, digits and hyphens, so a byte cut is
		// a character cut.
		slug = strings.TrimRight(slug[:MaxToolsetSlugLength], "-")
	}
	return slug
}

// CreateToolsetInTransaction is the one implementation of toolset creation.
// The management API's createToolset and the Platform MCP's server authoring
// both call it inside a transaction they own, so the slug rules, the
// first-server auto-enable, the default environment, the initial version, the
// audit entry and the Default plugin attachment cannot drift between them.
//
// It returns oops errors with the same codes the management API has always
// returned, so that handler passes them straight through.
func CreateToolsetInTransaction(ctx context.Context, tx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, input ToolsetCreateInput) (ToolsetCreateResult, error) {
	if tx == nil || logger == nil || auditLogger == nil || input.OrganizationID == "" || input.OrganizationSlug == "" || input.ProjectID == uuid.Nil || input.ActorUserID == "" {
		return ToolsetCreateResult{}, oops.E(oops.CodeUnexpected, nil, "invalid toolset creation input")
	}
	// Checked here rather than left to the column CHECKs, which would turn an
	// over-long name into an unexpected error instead of a readable refusal.
	if input.Name == "" || utf8.RuneCountInString(input.Name) > MaxToolsetNameLength {
		return ToolsetCreateResult{}, oops.E(oops.CodeBadRequest, ErrToolsetInputInvalid, "toolset name must be between 1 and %d characters", MaxToolsetNameLength)
	}
	// An empty or whitespace-only description is no description: the column
	// refuses an empty string, and the caller did not give one.
	if input.Description != nil && strings.TrimSpace(*input.Description) == "" {
		input.Description = nil
	}
	if input.Description != nil && utf8.RuneCountInString(*input.Description) > MaxToolsetDescriptionLength {
		return ToolsetCreateResult{}, oops.E(oops.CodeBadRequest, ErrToolsetInputInvalid, "toolset description must be at most %d characters", MaxToolsetDescriptionLength)
	}
	slug := ToolsetSlugFromName(input.Name)
	if slug == "" {
		return ToolsetCreateResult{}, oops.E(oops.CodeBadRequest, ErrToolsetInputInvalid, "toolset name must contain at least one letter or digit")
	}

	slugSuffix, err := conv.GenerateRandomSlug(5)
	if err != nil {
		return ToolsetCreateResult{}, oops.E(oops.CodeUnexpected, err, "failed to generate random slug").LogError(ctx, logger)
	}
	mcpSlug := input.OrganizationSlug + "-" + slugSuffix

	params := repo.CreateToolsetParams{
		OrganizationID:         input.OrganizationID,
		ProjectID:              input.ProjectID,
		Name:                   input.Name,
		Slug:                   slug,
		Description:            conv.PtrToPGText(input.Description),
		DefaultEnvironmentSlug: conv.PtrToPGText(nil),
		McpSlug:                conv.ToPGText(mcpSlug),
		McpEnabled:             false,
	}
	// The first toolset in an organization is enabled as an MCP server and
	// joins the Default plugin, which publishes it to everyone holding that
	// plugin. That is the widest reach a creation can have, so a failure to
	// read the count fails the creation rather than defaulting to "first".
	enabled, err := usageRepo.New(tx).GetEnabledServerCount(ctx, input.OrganizationID)
	if err != nil {
		return ToolsetCreateResult{}, oops.E(oops.CodeUnexpected, err, "error getting enabled server count").LogError(ctx, logger)
	}
	params.McpEnabled = enabled == 0

	environments := environmentsRepo.New(tx)
	if input.DefaultEnvironmentSlug != nil {
		if _, err := environments.GetEnvironmentBySlug(ctx, environmentsRepo.GetEnvironmentBySlugParams{
			Slug:      conv.ToLower(*input.DefaultEnvironmentSlug),
			ProjectID: input.ProjectID,
		}); err != nil {
			return ToolsetCreateResult{}, oops.E(oops.CodeUnexpected, err, "error finding environment")
		}
		params.DefaultEnvironmentSlug = conv.ToPGText(conv.ToLower(*input.DefaultEnvironmentSlug))
	} else {
		listed, err := environments.ListEnvironments(ctx, input.ProjectID)
		if err != nil {
			return ToolsetCreateResult{}, oops.E(oops.CodeUnexpected, err, "error listing environments")
		}
		for _, environment := range listed {
			if environment.Slug == "default" { // autofill the default environment when one exists
				params.DefaultEnvironmentSlug = conv.ToPGText(environment.Slug)
				break
			}
		}
	}

	if mcpSlug, err = ensureGeneratedMcpSlug(ctx, tx, logger, input.OrganizationSlug, input.OrganizationID, mcpSlug); err != nil {
		return ToolsetCreateResult{}, err
	}
	params.McpSlug = conv.ToPGText(mcpSlug)

	tr := repo.New(tx)
	created, err := tr.CreateToolset(ctx, params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return ToolsetCreateResult{}, oops.E(oops.CodeConflict, ErrToolsetSlugTaken, "toolset slug already exists")
		}
		return ToolsetCreateResult{}, oops.E(oops.CodeUnexpected, err, "failed to create toolset").LogError(ctx, logger)
	}

	if input.OriginRegistrySpecifier != nil {
		if _, err := tr.CreateToolsetOrigin(ctx, repo.CreateToolsetOriginParams{
			OrganizationID:    input.OrganizationID,
			ToolsetID:         created.ID,
			RegistrySpecifier: *input.OriginRegistrySpecifier,
		}); err != nil {
			return ToolsetCreateResult{}, oops.E(oops.CodeUnexpected, err, "failed to create toolset origin").LogError(ctx, logger)
		}
	}

	if err := createToolsetVersionInTransaction(ctx, logger.With(attr.SlogProjectID(input.ProjectID.String())), tr, created.ID, input.ToolURNs, input.ResourceURNs); err != nil {
		return ToolsetCreateResult{}, err
	}

	if err := auditLogger.LogToolsetCreate(ctx, tx, audit.LogToolsetCreateEvent{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID),
		ActorDisplayName: input.ActorEmail,
		ActorSlug:        nil,
		ToolsetURN:       urn.NewToolset(created.ID),
		ToolsetName:      created.Name,
		ToolsetSlug:      created.Slug,
	}); err != nil {
		return ToolsetCreateResult{}, oops.E(oops.CodeUnexpected, err, "failed to log toolset creation").LogError(ctx, logger)
	}

	result := ToolsetCreateResult{Toolset: created, McpEnabled: params.McpEnabled, AddedToDefaultPlugin: false, PluginCreated: false}
	if params.McpEnabled {
		projectID := input.ProjectID
		actor := &contextvalues.AuthContext{ActiveOrganizationID: input.OrganizationID, UserID: input.ActorUserID, Email: input.ActorEmail, ProjectID: &projectID}
		outcome, err := attachToDefaultPluginWithOutcome(ctx, tx, logger, auditLogger, actor, created.ID, created.Name)
		if err != nil {
			return ToolsetCreateResult{}, err
		}
		result.AddedToDefaultPlugin, result.PluginCreated = outcome.Attached, outcome.PluginCreated
	}
	return result, nil
}

// ensureGeneratedMcpSlug guards a generated platform mcp_slug against the
// unified namespace, regenerating on the rare collision with a live endpoint.
func ensureGeneratedMcpSlug(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, orgSlug, orgID, slug string) (string, error) {
	for attempt := 0; ; attempt++ {
		if err := mcpendpoints.LockSlugScope(ctx, dbtx, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, slug); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "lock mcp slug scope").LogError(ctx, logger)
		}
		available, err := mcpendpoints.CheckSlugAvailable(ctx, dbtx, mcpendpoints.SlugAvailabilityCheck{
			Slug:                     slug,
			CustomDomainID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			OrganizationID:           orgID,
			ExcludeToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ExcludeMcpServerID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			SkipDomainOwnershipCheck: false,
		})
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "check mcp slug availability").LogError(ctx, logger)
		}
		if available {
			return slug, nil
		}
		if attempt == 2 {
			return "", oops.E(oops.CodeConflict, nil, "could not generate a unique mcp slug").LogError(ctx, logger)
		}
		suffix, err := conv.GenerateRandomSlug(5)
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to generate random slug").LogError(ctx, logger)
		}
		slug = orgSlug + "-" + suffix
	}
}

// attachToDefaultPluginInTransaction adds a newly MCP-enabled toolset to the
// project's Default plugin so it's included in the auto-published marketplace
// without a human visiting the Plugins page. No-op if the toolset is already
// attached. Returns pluginCreated=true if this call lazily created the Default
// plugin (project predates this feature) — callers should enqueue an initial
// publish for it, but only after their own transaction commits, since this
// runs pre-commit and the DB writes could still roll back.
func attachToDefaultPluginInTransaction(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, toolsetID uuid.UUID, displayName string) (bool, error) {
	outcome, err := attachToDefaultPluginWithOutcome(ctx, dbtx, logger, auditLogger, authCtx, toolsetID, displayName)
	return outcome.PluginCreated, err
}

func attachToDefaultPluginWithOutcome(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, toolsetID uuid.UUID, displayName string) (plugins.DefaultPluginAttachOutcome, error) {
	outcome, err := plugins.AttachToDefaultPluginAuditedWithOutcome(ctx, dbtx, auditLogger, authCtx, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ToolsetID:      uuid.NullUUID{UUID: toolsetID, Valid: true},
		McpServerID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		DisplayName:    displayName,
	})
	if err != nil {
		return plugins.DefaultPluginAttachOutcome{}, oops.E(oops.CodeUnexpected, err, "attach toolset to default plugin").LogError(ctx, logger)
	}
	return outcome, nil
}

// createToolsetVersionInTransaction records a new version when the given URNs
// differ from the latest one. A nil list for tools or resources inherits that
// half from the latest version; an empty, non-nil list clears it.
func createToolsetVersionInTransaction(ctx context.Context, logger *slog.Logger, tr *repo.Queries, toolsetID uuid.UUID, toolUrnStrings []string, resourceUrnStrings []string) error {
	logger = logger.With(attr.SlogToolsetID(toolsetID.String()))

	// Only create a version if URNs are provided (indicating a change). Check nil (not len==0) so that toolsets can be made empty.
	if toolUrnStrings == nil && resourceUrnStrings == nil {
		return nil
	}

	allToolUrns := []urn.Tool{}
	for _, urnStr := range toolUrnStrings {
		var toolUrn urn.Tool
		if err := toolUrn.UnmarshalText([]byte(urnStr)); err != nil {
			logger.WarnContext(ctx, "invalid tool URN", attr.SlogError(err), attr.SlogToolURN(urnStr))
			continue
		}
		allToolUrns = append(allToolUrns, toolUrn)
	}

	allResourceUrns := []urn.Resource{}
	for _, urnStr := range resourceUrnStrings {
		var resourceUrn urn.Resource
		if err := resourceUrn.UnmarshalText([]byte(urnStr)); err != nil {
			logger.WarnContext(ctx, "invalid resource URN", attr.SlogError(err), attr.SlogResourceURN(urnStr))
			continue
		}
		allResourceUrns = append(allResourceUrns, resourceUrn)
	}

	latestVersion, err := tr.GetLatestToolsetVersion(ctx, toolsetID)
	latestVersionNumber := int64(0)
	var predecessorID uuid.NullUUID
	if err == nil {
		predecessorID = uuid.NullUUID{UUID: latestVersion.ID, Valid: true}
		latestVersionNumber = latestVersion.Version
	}

	if toolUrnStrings == nil && len(latestVersion.ToolUrns) > 0 {
		allToolUrns = append(allToolUrns, latestVersion.ToolUrns...)
	}

	if resourceUrnStrings == nil && len(latestVersion.ResourceUrns) > 0 {
		allResourceUrns = append(allResourceUrns, latestVersion.ResourceUrns...)
	}

	if err == nil && sameToolURNs(latestVersion.ToolUrns, allToolUrns) && sameResourceURNs(latestVersion.ResourceUrns, allResourceUrns) {
		return nil // No change needed
	}

	if _, err := tr.CreateToolsetVersion(ctx, repo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       latestVersionNumber + 1,
		ToolUrns:      allToolUrns,
		ResourceUrns:  allResourceUrns,
		PredecessorID: predecessorID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create toolset version").LogError(ctx, logger)
	}
	return nil
}

func sameToolURNs(existing, next []urn.Tool) bool {
	if len(existing) != len(next) {
		return false
	}
	set := make(map[string]bool, len(existing))
	for _, value := range existing {
		set[value.String()] = true
	}
	for _, value := range next {
		if !set[value.String()] {
			return false
		}
	}
	return true
}

func sameResourceURNs(existing, next []urn.Resource) bool {
	if len(existing) != len(next) {
		return false
	}
	set := make(map[string]bool, len(existing))
	for _, value := range existing {
		set[value.String()] = true
	}
	for _, value := range next {
		if !set[value.String()] {
			return false
		}
	}
	return true
}
