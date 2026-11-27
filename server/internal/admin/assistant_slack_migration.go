package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tombstone"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformtoolscore "github.com/speakeasy-api/gram/server/internal/platformtools/core"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	assistantSlackMigrationMigrated        = "migrated"
	assistantSlackMigrationAlreadyMigrated = "already_migrated"
	assistantSlackMigrationNoSlack         = "no_slack"
	assistantSlackMigrationNeedsIdentity   = "needs_identity"
	assistantSlackMigrationFailed          = "failed"

	slackPlatformToolURNPrefix = "tools:platform:slack:"
)

// ErrAssistantSlackMigrationUnavailable reports that application encryption is
// not configured, so Slack secrets cannot be migrated to slack_app auth clients.
var ErrAssistantSlackMigrationUnavailable = errors.New("application encryption is not configured")

// SetApplicationEncryption supplies the application-level encryption client used
// to migrate customer secrets (such as Slack environment entries) into encrypted
// typed auth client headers.
func (s *Service) SetApplicationEncryption(enc *encryption.Client) {
	s.applicationEncryption = enc
}

// MigrateOrganizationAssistantSlack migrates every live assistant in one exact
// organization from legacy per-assistant Slack toolsets and environment-backed
// Slack triggers to the built-in assistant-slack toolset backed by a slack_app
// typed auth client.
func (s *Service) MigrateOrganizationAssistantSlack(ctx context.Context, p *gen.MigrateOrganizationAssistantSlackPayload) (*gen.AdminAssistantSlackMigrationRun, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if s.applicationEncryption == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "assistant Slack migration is not available: application encryption is not configured").LogWarn(ctx, s.logger)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))

	detached := context.WithoutCancel(ctx)
	runCtx, cancel := context.WithTimeout(detached, assistantIdentityMigrationTimeout)
	defer cancel()

	tx, err := s.db.Begin(runCtx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin assistant Slack migration").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(detached) })

	run, err := MigrateAssistantSlackTx(runCtx, tx, logger, s.tracerProvider, s.audit, s.applicationEncryption, organizationID)
	switch {
	case errors.Is(err, ErrAssistantMigrationOrganizationNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "organization not found")
	case errors.Is(err, ErrAssistantSlackMigrationUnavailable):
		return nil, oops.E(oops.CodeUnavailable, err, "assistant Slack migration is not available: application encryption is not configured").LogWarn(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "migrate assistant Slack").LogError(ctx, logger)
	}
	if err := tx.Commit(runCtx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit assistant Slack migration").LogError(ctx, logger)
	}

	logger.InfoContext(detached, "assistant Slack migration ran",
		attr.SlogAuditAction("assistant_slack:migrate"),
		attr.SlogAdminOIDCSubject(auth.OIDCSubject),
	)
	return run, nil
}

// MigrateAssistantSlackTx is shared by the staff API and Admin MCP. Each
// assistant runs inside its own savepoint so one failure never aborts its peers.
// Writes are ordered so the slack_app auth client and mcp:connect / mcp:auth
// grants exist before legacy triggers are repointed or per-assistant Slack
// toolsets are detached, keeping in-flight turns working across cutover.
func MigrateAssistantSlackTx(
	ctx context.Context,
	tx pgx.Tx,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	auditLogger *audit.Logger,
	enc *encryption.Client,
	organizationID string,
) (*gen.AdminAssistantSlackMigrationRun, error) {
	if enc == nil {
		return nil, ErrAssistantSlackMigrationUnavailable
	}
	if tracerProvider == nil {
		tracerProvider = otel.GetTracerProvider()
	}
	q := repo.New(tx)
	if _, err := q.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: organizationID, AllowSlug: false}); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssistantMigrationOrganizationNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read organization: %w", err)
	}

	rows, err := q.AdminListAssistantSlackMigrationTargets(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list assistant Slack migration targets: %w", err)
	}

	run := &gen.AdminAssistantSlackMigrationRun{
		OrganizationID:  organizationID,
		Migrated:        0,
		AlreadyMigrated: 0,
		NoSlack:         0,
		NeedsIdentity:   0,
		Failed:          0,
		Results:         make([]*gen.AdminAssistantSlackMigrationResult, 0, len(rows)),
	}

	for _, row := range rows {
		res, err := migrateOneAssistantSlack(ctx, tx, logger, tracerProvider, auditLogger, enc, organizationID, row)
		if err != nil {
			logger.ErrorContext(ctx, "migrate assistant Slack", attr.SlogError(err), attr.SlogAssistantID(row.AssistantID.String()))
			res = &gen.AdminAssistantSlackMigrationResult{
				AssistantID:       row.AssistantID.String(),
				ProjectID:         row.ProjectID.String(),
				Name:              row.Name,
				Outcome:           assistantSlackMigrationFailed,
				AuthClientID:      nil,
				TriggersRepointed: 0,
				ToolsetsDetached:  0,
				ToolsetsDeleted:   0,
			}
		}
		switch res.Outcome {
		case assistantSlackMigrationMigrated:
			run.Migrated++
		case assistantSlackMigrationAlreadyMigrated:
			run.AlreadyMigrated++
		case assistantSlackMigrationNoSlack:
			run.NoSlack++
		case assistantSlackMigrationNeedsIdentity:
			run.NeedsIdentity++
		default:
			run.Failed++
		}
		run.Results = append(run.Results, res)
	}
	return run, nil
}

func migrateOneAssistantSlack(
	ctx context.Context,
	tx pgx.Tx,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	auditLogger *audit.Logger,
	enc *encryption.Client,
	organizationID string,
	row repo.AdminListAssistantSlackMigrationTargetsRow,
) (*gen.AdminAssistantSlackMigrationResult, error) {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin assistant Slack migration savepoint: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return savepoint.Rollback(ctx) })

	q := repo.New(savepoint)
	triggers, err := q.AdminListAssistantSlackTriggers(ctx, repo.AdminListAssistantSlackTriggersParams{
		OrganizationID: organizationID,
		ProjectID:      row.ProjectID,
		AssistantID:    row.AssistantID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list assistant Slack triggers: %w", err)
	}
	toolsets, err := q.AdminListAssistantSlackToolsets(ctx, repo.AdminListAssistantSlackToolsetsParams{
		OrganizationID: organizationID,
		ProjectID:      row.ProjectID,
		AssistantID:    row.AssistantID,
	})
	if err != nil {
		return nil, fmt.Errorf("list assistant Slack toolsets: %w", err)
	}

	var legacyTriggers []repo.AdminListAssistantSlackTriggersRow
	var migratedTriggers []repo.AdminListAssistantSlackTriggersRow
	for _, trig := range triggers {
		if trig.AuthClientID.Valid {
			migratedTriggers = append(migratedTriggers, trig)
		} else {
			legacyTriggers = append(legacyTriggers, trig)
		}
	}

	var slackOnlyToolsets []repo.AdminListAssistantSlackToolsetsRow
	for _, ts := range toolsets {
		if isSlackOnlyToolset(ts.ToolUrns) {
			slackOnlyToolsets = append(slackOnlyToolsets, ts)
		}
	}

	// Check whether the assistant's agent already owns a slack_app client even
	// when no trigger exists (outbound-only Slack assistant).
	var existingAgentClientID uuid.NullUUID
	if row.AgentID.Valid {
		foundID, findErr := q.AdminFindAgentSlackAuthClient(ctx, repo.AdminFindAgentSlackAuthClientParams{
			OrganizationID: organizationID,
			ProjectID:      uuid.NullUUID{UUID: row.ProjectID, Valid: true},
			AgentID:        row.AgentID.UUID,
		})
		switch {
		case findErr == nil:
			existingAgentClientID = uuid.NullUUID{UUID: foundID, Valid: true}
		case errors.Is(findErr, pgx.ErrNoRows):
		default:
			return nil, fmt.Errorf("find existing agent slack_app auth client: %w", findErr)
		}
	}

	hasSlack := len(triggers) > 0 || len(toolsets) > 0 || existingAgentClientID.Valid
	if !hasSlack {
		return &gen.AdminAssistantSlackMigrationResult{
			AssistantID:       row.AssistantID.String(),
			ProjectID:         row.ProjectID.String(),
			Name:              row.Name,
			Outcome:           assistantSlackMigrationNoSlack,
			AuthClientID:      nil,
			TriggersRepointed: 0,
			ToolsetsDetached:  0,
			ToolsetsDeleted:   0,
		}, nil
	}

	if !row.AgentID.Valid {
		return &gen.AdminAssistantSlackMigrationResult{
			AssistantID:       row.AssistantID.String(),
			ProjectID:         row.ProjectID.String(),
			Name:              row.Name,
			Outcome:           assistantSlackMigrationNeedsIdentity,
			AuthClientID:      nil,
			TriggersRepointed: 0,
			ToolsetsDetached:  0,
			ToolsetsDeleted:   0,
		}, nil
	}

	actor, displayName, _ := adminActor(ctx)
	changed := false

	// 1. Collect Slack secrets from every legacy environment referenced by this
	// assistant's Slack triggers or attached Slack toolsets.
	envSecrets, err := collectAssistantSlackEnvSecrets(ctx, q, enc, organizationID, row.ProjectID, legacyTriggers, toolsets)
	if err != nil {
		return nil, err
	}

	// 2. Reuse an existing slack_app auth client for this assistant (from a
	// migrated trigger or the agent's existing mcp:auth grant) or create a new one.
	authClients := authclientsrepo.New(savepoint)
	var authClientID uuid.UUID
	var authClientName string
	switch {
	case len(migratedTriggers) > 0:
		authClientID = migratedTriggers[0].AuthClientID.UUID
	case existingAgentClientID.Valid:
		authClientID = existingAgentClientID.UUID
	}

	if authClientID == uuid.Nil {
		authClientName = slackAuthClientDisplayName(row.Name)
		created, err := authClients.CreateAuthClient(ctx, authclientsrepo.CreateAuthClientParams{
			OrganizationID:        organizationID,
			ProjectID:             uuid.NullUUID{UUID: row.ProjectID, Valid: true},
			ClientType:            authz.AuthClientTypeSlackApp,
			Name:                  authClientName,
			CreatedByUserID:       pgtype.Text{String: "", Valid: false},
			RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ClientAttachmentScope: pgtype.Text{String: "", Valid: false},
		})
		if err != nil {
			return nil, fmt.Errorf("create slack_app auth client: %w", err)
		}
		authClientID = created.ID
		changed = true

		writtenHeaders, err := writeMissingSlackAuthClientHeaders(ctx, authClients, enc, organizationID, authClientID, nil, envSecrets)
		if err != nil {
			return nil, err
		}
		if err := auditLogger.LogAuthClientCreate(ctx, savepoint, audit.LogAuthClientUpdateEvent{
			OrganizationID:           organizationID,
			ProjectID:                row.ProjectID,
			Actor:                    actor,
			ActorDisplayName:         displayName,
			ActorSlug:                nil,
			AuthClientURN:            urn.NewAuthClient(created.ID),
			AuthClientName:           created.Name,
			AuthClientSnapshotBefore: nil,
			AuthClientSnapshotAfter:  slackAuthClientAuditSnapshot(created, writtenHeaders),
		}); err != nil {
			return nil, fmt.Errorf("audit slack_app auth client create: %w", err)
		}
	} else if len(envSecrets) > 0 {
		stored, err := authClients.ListAuthClientStoredHeaders(ctx, authclientsrepo.ListAuthClientStoredHeadersParams{
			AuthClientID:   authClientID,
			OrganizationID: organizationID,
		})
		if err != nil {
			return nil, fmt.Errorf("list existing slack_app auth client headers: %w", err)
		}
		existingNames := make(map[string]struct{}, len(stored))
		for _, h := range stored {
			if h.Value.Valid {
				existingNames[strings.ToUpper(h.Name)] = struct{}{}
			}
		}
		addedHeaders, err := writeMissingSlackAuthClientHeaders(ctx, authClients, enc, organizationID, authClientID, existingNames, envSecrets)
		if err != nil {
			return nil, err
		}
		if len(addedHeaders) > 0 {
			changed = true
		}
	}

	// 3. Grant permanent mcp:connect on PlatformToolsetID("assistant-slack") AND
	// permanent mcp:auth on the slack_app auth client BEFORE repointing triggers
	// or detaching old toolsets.
	grantsAdded, err := ensureAssistantSlackAgentGrants(ctx, savepoint, auditLogger, organizationID, row.ProjectID, row.AgentID.UUID, authClientID, actor, displayName)
	if err != nil {
		return nil, err
	}
	if grantsAdded > 0 {
		changed = true
	}

	// 4. Repoint every legacy environment-backed Slack trigger to auth_client_id
	// and clear environment_id so hard-deleting the auth client can never fall
	// back to stale environment credentials.
	var triggersRepointed int64
	for _, trig := range legacyTriggers {
		updated, err := q.AdminRepointSlackTriggerToAuthClient(ctx, repo.AdminRepointSlackTriggerToAuthClientParams{
			AuthClientID:   uuid.NullUUID{UUID: authClientID, Valid: true},
			ID:             trig.ID,
			OrganizationID: organizationID,
			ProjectID:      row.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("repoint Slack trigger %s: %w", trig.ID, err)
		}
		triggersRepointed++
		changed = true

		beforeSnap := triggerAuditSnapshot(trig)
		afterSnap := triggerAuditSnapshot(repo.AdminListAssistantSlackTriggersRow(updated))
		if err := auditLogger.LogTriggerInstanceUpdate(ctx, savepoint, audit.LogTriggerInstanceUpdateEvent{
			OrganizationID:                organizationID,
			ProjectID:                     row.ProjectID,
			Actor:                         actor,
			ActorDisplayName:              displayName,
			ActorSlug:                     nil,
			TriggerInstanceURN:            urn.NewTriggerInstance(trig.ID),
			Name:                          updated.Name,
			DefinitionSlug:                updated.DefinitionSlug,
			TriggerInstanceSnapshotBefore: beforeSnap,
			TriggerInstanceSnapshotAfter:  afterSnap,
		}); err != nil {
			return nil, fmt.Errorf("audit Slack trigger repoint %s: %w", trig.ID, err)
		}
	}

	// 5. Detach Slack-only per-assistant toolsets from this assistant, and
	// soft-delete + tombstone them ONLY if no other assistant, plugin, meta MCP
	// server, or Slack app still references them. Environments are never deleted.
	var toolsetsDetached int64
	var toolsetsDeleted int64
	for _, ts := range slackOnlyToolsets {
		detached, err := q.AdminDetachAssistantMCPServer(ctx, repo.AdminDetachAssistantMCPServerParams{
			AssistantID: row.AssistantID,
			McpServerID: ts.McpServerID,
			ProjectID:   row.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("detach per-assistant Slack toolset %s: %w", ts.ToolsetID, err)
		}
		if detached > 0 {
			toolsetsDetached++
			changed = true
		}

		refCount, err := q.AdminCountToolsetExternalReferences(ctx, repo.AdminCountToolsetExternalReferencesParams{
			McpServerID: ts.McpServerID,
			ProjectID:   row.ProjectID,
			ToolsetID:   ts.ToolsetID,
		})
		if err != nil {
			return nil, fmt.Errorf("count external references for toolset %s: %w", ts.ToolsetID, err)
		}
		if refCount > 0 {
			continue
		}

		deletedToolset, err := toolsetsrepo.New(savepoint).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{
			Slug:      ts.ToolsetSlug,
			ProjectID: row.ProjectID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("soft-delete unreferenced Slack toolset %s: %w", ts.ToolsetID, err)
		}
		if err == nil {
			toolsetsDeleted++
			changed = true
			if err := tombstoneUnreferencedSlackMCPServer(ctx, savepoint, logger, tracerProvider, auditLogger, organizationID, row.ProjectID, ts, actor, displayName); err != nil {
				return nil, err
			}
			if err := auditLogger.LogToolsetDelete(ctx, savepoint, audit.LogToolsetDeleteEvent{
				OrganizationID:   organizationID,
				ProjectID:        row.ProjectID,
				Actor:            actor,
				ActorDisplayName: displayName,
				ActorSlug:        nil,
				ToolsetURN:       urn.NewToolset(deletedToolset.ID),
				ToolsetName:      deletedToolset.Name,
				ToolsetSlug:      deletedToolset.Slug,
			}); err != nil {
				return nil, fmt.Errorf("audit Slack toolset delete %s: %w", ts.ToolsetID, err)
			}
		}
	}

	if err := savepoint.Commit(ctx); err != nil {
		return nil, fmt.Errorf("release assistant Slack migration savepoint: %w", err)
	}

	outcome := assistantSlackMigrationAlreadyMigrated
	if changed {
		outcome = assistantSlackMigrationMigrated
	}
	return &gen.AdminAssistantSlackMigrationResult{
		AssistantID:       row.AssistantID.String(),
		ProjectID:         row.ProjectID.String(),
		Name:              row.Name,
		Outcome:           outcome,
		AuthClientID:      new(authClientID.String()),
		TriggersRepointed: triggersRepointed,
		ToolsetsDetached:  toolsetsDetached,
		ToolsetsDeleted:   toolsetsDeleted,
	}, nil
}

func isSlackOnlyToolset(toolURNs []string) bool {
	if len(toolURNs) == 0 {
		return false
	}
	for _, u := range toolURNs {
		if !strings.HasPrefix(u, slackPlatformToolURNPrefix) {
			return false
		}
	}
	return true
}

func slackAuthClientDisplayName(assistantName string) string {
	trimmed := strings.TrimSpace(assistantName)
	if trimmed == "" {
		return "Assistant Slack"
	}
	const suffix = " Slack"
	if strings.HasSuffix(strings.ToLower(trimmed), " slack") {
		return trimmed
	}
	return trimmed + suffix
}

func collectAssistantSlackEnvSecrets(
	ctx context.Context,
	q *repo.Queries,
	enc *encryption.Client,
	organizationID string,
	projectID uuid.UUID,
	legacyTriggers []repo.AdminListAssistantSlackTriggersRow,
	toolsets []repo.AdminListAssistantSlackToolsetsRow,
) (map[string]string, error) {
	seenEnvs := make(map[uuid.UUID]struct{})
	envIDs := make([]uuid.UUID, 0, len(legacyTriggers)+len(toolsets)*2)
	addEnv := func(id uuid.UUID) {
		if id == uuid.Nil {
			return
		}
		if _, ok := seenEnvs[id]; ok {
			return
		}
		seenEnvs[id] = struct{}{}
		envIDs = append(envIDs, id)
	}

	for _, trig := range legacyTriggers {
		if trig.EnvironmentID.Valid {
			addEnv(trig.EnvironmentID.UUID)
		}
	}
	for _, ts := range toolsets {
		if ts.AttachmentEnvironmentID.Valid {
			addEnv(ts.AttachmentEnvironmentID.UUID)
		}
		if ts.DefaultEnvironmentSlug.Valid && ts.DefaultEnvironmentSlug.String != "" {
			envID, err := q.AdminGetEnvironmentIDBySlug(ctx, repo.AdminGetEnvironmentIDBySlugParams{
				OrganizationID: organizationID,
				ProjectID:      projectID,
				Slug:           ts.DefaultEnvironmentSlug.String,
			})
			switch {
			case err == nil:
				addEnv(envID)
			case errors.Is(err, pgx.ErrNoRows):
			default:
				return nil, fmt.Errorf("resolve toolset default environment %q: %w", ts.DefaultEnvironmentSlug.String, err)
			}
		}
	}

	secrets := make(map[string]string)
	for _, envID := range envIDs {
		entries, err := q.AdminListEnvironmentSlackEntries(ctx, repo.AdminListEnvironmentSlackEntriesParams{
			OrganizationID: organizationID,
			ProjectID:      projectID,
			EnvironmentID:  envID,
		})
		if err != nil {
			return nil, fmt.Errorf("list Slack environment entries for %s: %w", envID, err)
		}
		for _, entry := range entries {
			key := strings.ToUpper(strings.TrimSpace(entry.Name))
			if _, already := secrets[key]; already {
				continue
			}
			plainVal, err := enc.Decrypt(entry.Value)
			if err != nil {
				return nil, fmt.Errorf("decrypt Slack environment entry %s: %w", key, err)
			}
			plain := strings.TrimSpace(plainVal)
			if plain == "" {
				continue
			}
			secrets[key] = plain
		}
	}
	return secrets, nil
}

var canonicalSlackSecretOrder = []string{
	authclients.SlackHeaderSigningSecret,
	authclients.SlackHeaderBotToken,
	authclients.SlackHeaderUserToken,
	authclients.SlackHeaderToken,
}

func writeMissingSlackAuthClientHeaders(
	ctx context.Context,
	authClients *authclientsrepo.Queries,
	enc *encryption.Client,
	organizationID string,
	authClientID uuid.UUID,
	existing map[string]struct{},
	secrets map[string]string,
) ([]string, error) {
	written := make([]string, 0, len(secrets))
	for _, name := range canonicalSlackSecretOrder {
		plain, ok := secrets[name]
		if !ok || plain == "" {
			continue
		}
		if _, exists := existing[name]; exists {
			continue
		}
		ciphertext, err := enc.Encrypt([]byte(plain))
		if err != nil {
			return nil, fmt.Errorf("encrypt Slack auth client header %s: %w", name, err)
		}
		if err := authClients.CreateAuthClientHeader(ctx, authclientsrepo.CreateAuthClientHeaderParams{
			AuthClientID:           authClientID,
			OrganizationID:         organizationID,
			Name:                   name,
			Description:            pgtype.Text{String: "", Valid: false},
			IsRequired:             false,
			IsSecret:               true,
			Value:                  pgtype.Text{String: ciphertext, Valid: true},
			ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
		}); err != nil {
			return nil, fmt.Errorf("insert Slack auth client header %s: %w", name, err)
		}
		written = append(written, name)
	}
	return written, nil
}

func ensureAssistantSlackAgentGrants(
	ctx context.Context,
	tx pgx.Tx,
	auditLogger *audit.Logger,
	organizationID string,
	projectID uuid.UUID,
	agentID uuid.UUID,
	authClientID uuid.UUID,
	actor urn.Principal,
	displayName *string,
) (int64, error) {
	agents := agentrepo.New(tx)
	existing, err := agents.ListAgentPolicyGrants(ctx, agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: organizationID,
		AgentID:        agentID,
	})
	if err != nil {
		return 0, fmt.Errorf("list agent policy grants: %w", err)
	}

	desired := []authz.Grant{
		assistantidentity.BuiltinConnectGrant(projectID, platformtoolscore.AssistantSlackPlatformToolsetSlug),
		assistantidentity.AssistantSlackAuthGrant(authClientID),
	}
	agentURN := urn.NewAgentIdentity(agentID.String())
	var added int64
	for _, grant := range desired {
		prior, found := findExactGrant(existing, grant)
		if found && !prior.ExpiresAt.Valid {
			continue
		}
		encoded, err := json.Marshal(grant.Selector)
		if err != nil {
			return 0, fmt.Errorf("encode agent grant selector: %w", err)
		}
		written, err := agents.EnsurePermanentAgentPolicyGrant(ctx, agentrepo.EnsurePermanentAgentPolicyGrantParams{
			OrganizationID: organizationID,
			AgentID:        agentID,
			Scope:          string(grant.Scope),
			Selectors:      encoded,
		})
		if err != nil {
			return 0, fmt.Errorf("ensure agent grant %s: %w", grant.Scope, err)
		}
		action := audit.ActionAgentPolicyGrantCreate
		var before *audit.AgentPolicyGrantSnapshot
		if found {
			action = audit.ActionAgentPolicyGrantUpdate
			before = &audit.AgentPolicyGrantSnapshot{
				ID:       prior.ID,
				Scope:    prior.Scope,
				Effect:   "allow",
				Selector: grant.Selector,
			}
		}
		if err := auditLogger.LogAgentPolicyGrant(ctx, tx, audit.LogAgentPolicyGrantEvent{
			OrganizationID:   organizationID,
			AgentURN:         agentURN,
			Actor:            actor,
			ActorDisplayName: displayName,
			Action:           action,
			Name:             "",
			Before:           before,
			After: &audit.AgentPolicyGrantSnapshot{
				ID:       written.ID,
				Scope:    written.Scope,
				Effect:   "allow",
				Selector: grant.Selector,
			},
		}); err != nil {
			return 0, fmt.Errorf("audit agent grant %s: %w", grant.Scope, err)
		}
		added++
	}
	return added, nil
}

func tombstoneUnreferencedSlackMCPServer(
	ctx context.Context,
	tx pgx.Tx,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	auditLogger *audit.Logger,
	organizationID string,
	projectID uuid.UUID,
	ts repo.AdminListAssistantSlackToolsetsRow,
	actor urn.Principal,
	displayName *string,
) error {
	locked, err := tombstone.Lock(ctx, tx, organizationID, projectID, ts.McpServerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock hosted Slack MCP server %s: %w", ts.McpServerID, err)
	}
	if !locked.Server.ToolsetID.Valid || locked.Server.ToolsetID.UUID != ts.ToolsetID {
		return nil
	}
	_, err = tombstone.Tombstone(ctx, tx, auditLogger, locked, tombstone.Input{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ActorUserID:    "",
		ActorEmail:     displayName,
		ActorSystem:    "admin",
		TracerProvider: tracerProvider,
		Logger:         logger,
	})
	if err != nil {
		return fmt.Errorf("tombstone hosted Slack MCP server %s: %w", ts.McpServerID, err)
	}
	if err := auditLogger.LogMcpServerDelete(ctx, tx, audit.LogMcpServerDeleteEvent{
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		Actor:            actor,
		ActorDisplayName: displayName,
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(ts.McpServerID),
		McpServerName:    ts.ToolsetName,
		McpServerSlug:    locked.Server.Slug.String,
	}); err != nil {
		return fmt.Errorf("audit hosted Slack MCP server delete %s: %w", ts.McpServerID, err)
	}
	return nil
}

func slackAuthClientAuditSnapshot(client authclientsrepo.McpAuthClient, headerNames []string) *types.AuthClient {
	sorted := slices.Clone(headerNames)
	slices.Sort(sorted)
	headers := make([]*types.AuthClientHeader, 0, len(sorted))
	for _, name := range sorted {
		headers = append(headers, &types.AuthClientHeader{
			Name:                   name,
			Description:            nil,
			IsRequired:             false,
			IsSecret:               true,
			HasStoredValue:         true,
			ValueFromRequestHeader: nil,
		})
	}
	var projectID *string
	if client.ProjectID.Valid {
		projectID = new(client.ProjectID.UUID.String())
	}
	return &types.AuthClient{
		ID:                    client.ID.String(),
		ClientType:            client.ClientType,
		Source:                types.AuthClientSource(client.Source),
		Name:                  client.Name,
		Enabled:               client.Enabled,
		Personal:              false,
		ProjectID:             projectID,
		RemoteSessionClientID: nil,
		Servers:               []*types.AuthClientServer{},
		Headers:               headers,
		CreatedAt:             client.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:             client.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func triggerAuditSnapshot(row repo.AdminListAssistantSlackTriggersRow) *types.TriggerInstance {
	var envID *string
	if row.EnvironmentID.Valid {
		envID = new(row.EnvironmentID.UUID.String())
	}
	var authClientID *string
	if row.AuthClientID.Valid {
		authClientID = new(row.AuthClientID.UUID.String())
	}
	var cfg map[string]any
	if len(row.ConfigJson) > 0 {
		_ = json.Unmarshal(row.ConfigJson, &cfg)
	}
	return &types.TriggerInstance{
		ID:             row.ID.String(),
		ProjectID:      row.ProjectID.String(),
		DefinitionSlug: row.DefinitionSlug,
		Name:           row.Name,
		EnvironmentID:  envID,
		AuthClientID:   authClientID,
		TargetKind:     row.TargetKind,
		TargetRef:      row.TargetRef,
		TargetDisplay:  row.TargetDisplay,
		Config:         cfg,
		Status:         row.Status,
		WebhookURL:     nil,
		CreatedAt:      row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}
