package mcpmetadata

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	// ErrInstallPageLogoInvalid rejects a logo that is not a live image asset
	// of the caller's organization.
	ErrInstallPageLogoInvalid = errors.New("install page logo is not an image of this organization")
	// ErrInstallPageInherited rejects a patch for a server whose install page
	// shows its backing toolset's metadata: a new server-keyed row would win
	// every read and silently drop the inherited settings the patch omitted.
	ErrInstallPageInherited = errors.New("install page metadata is inherited from the backing toolset")
)

// InstallPage is the install-page metadata of one MCP server's row. A field
// that is not Valid is unset.
type InstallPage struct {
	LogoAssetID               uuid.NullUUID
	ExternalDocumentationURL  pgtype.Text
	ExternalDocumentationText pgtype.Text
	Instructions              pgtype.Text
}

// InstallPagePatch changes install-page metadata with patch semantics: a nil
// field leaves its column unchanged, a pointer to "" clears it to NULL, and
// any other value sets it. LogoAssetID must name an image of OrganizationID.
type InstallPagePatch struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ActorUserID    string
	ActorEmail     *string

	LogoAssetID               *string
	ExternalDocumentationURL  *string
	ExternalDocumentationText *string
	Instructions              *string
}

func (p InstallPagePatch) empty() bool {
	return p.LogoAssetID == nil && p.ExternalDocumentationURL == nil && p.ExternalDocumentationText == nil && p.Instructions == nil
}

// LockMCPServerInstallPageInTransaction takes the save lock SetMcpMetadata
// takes for this server and returns the metadata every reader shows for it, so
// a caller can check an expected version before patching under the same lock.
func LockMCPServerInstallPageInTransaction(ctx context.Context, tx pgx.Tx, server mcpservers_repo.McpServer) (InstallPage, error) {
	row, _, err := lockMCPServerMetadata(ctx, repo.New(tx), server)
	if err != nil {
		return InstallPage{}, err
	}
	return installPageFromRow(row), nil
}

// PatchMCPServerInstallPageInTransaction applies a patch to the row the
// dashboard reads for this MCP server, in the caller's transaction, and audits
// it as a dashboard metadata save is audited. Unlike SetMcpMetadata it is never
// a full replace: environment configs, the default environment, the
// installation override, and header display names are never touched, and an
// existing row is never rekeyed. A server with no row of its own gets a
// server-keyed row, the key SetMcpMetadata gives a new row saved by
// mcp_server_id. The caller must hold the server's row lock.
func PatchMCPServerInstallPageInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, server mcpservers_repo.McpServer, patch InstallPagePatch) (InstallPage, error) {
	if auditLogger == nil || patch.OrganizationID == "" || patch.ProjectID != server.ProjectID || patch.ActorUserID == "" {
		return InstallPage{}, errors.New("patch mcp server install page: incomplete input")
	}
	mcpr := repo.New(tx)
	existing, hasDifferentToolset, err := lockMCPServerMetadata(ctx, mcpr, server)
	if err != nil {
		return InstallPage{}, err
	}
	if patch.empty() {
		return installPageFromRow(existing), nil
	}

	logoID, err := patchLogoID(ctx, mcpr, patch)
	if err != nil {
		return InstallPage{}, err
	}
	docsURL, docsText, instructions := patchText(patch.ExternalDocumentationURL), patchText(patch.ExternalDocumentationText), patchText(patch.Instructions)

	var result repo.McpMetadatum
	switch {
	case existing != nil:
		result, err = mcpr.PatchMetadataInstallPage(ctx, repo.PatchMetadataInstallPageParams{
			SetLogoID:                    patch.LogoAssetID != nil,
			LogoID:                       logoID,
			SetExternalDocumentationUrl:  patch.ExternalDocumentationURL != nil,
			ExternalDocumentationUrl:     docsURL,
			SetExternalDocumentationText: patch.ExternalDocumentationText != nil,
			ExternalDocumentationText:    docsText,
			SetInstructions:              patch.Instructions != nil,
			Instructions:                 instructions,
			ID:                           existing.ID,
			ProjectID:                    server.ProjectID,
		})
		if err != nil {
			return InstallPage{}, fmt.Errorf("patch mcp server install page: %w", err)
		}
	case hasDifferentToolset:
		_, err := mcpr.GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: server.ProjectID, BackendID: server.ToolsetID.UUID})
		switch {
		case err == nil:
			return InstallPage{}, ErrInstallPageInherited
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return InstallPage{}, fmt.Errorf("get inherited mcp server install page: %w", err)
		}
		fallthrough
	default:
		// Clearing what was never set changes nothing, so it creates no row.
		if !logoID.Valid && !docsURL.Valid && !docsText.Valid && !instructions.Valid {
			return installPageFromRow(nil), nil
		}
		result, err = mcpr.CreateMetadataInstallPageByMcpServerID(ctx, repo.CreateMetadataInstallPageByMcpServerIDParams{
			McpServerID:               uuid.NullUUID{UUID: server.ID, Valid: true},
			ProjectID:                 server.ProjectID,
			LogoID:                    logoID,
			ExternalDocumentationUrl:  docsURL,
			ExternalDocumentationText: docsText,
			Instructions:              instructions,
		})
		if err != nil {
			return InstallPage{}, fmt.Errorf("create mcp server install page: %w", err)
		}
	}

	var before *types.McpMetadata
	if existing != nil {
		if before, err = ToMCPMetadata(ctx, mcpr, *existing); err != nil {
			return InstallPage{}, fmt.Errorf("convert existing mcp server install page: %w", err)
		}
	}
	after, err := ToMCPMetadata(ctx, mcpr, result)
	if err != nil {
		return InstallPage{}, fmt.Errorf("convert mcp server install page: %w", err)
	}
	if err := auditLogger.LogMCPMetadataUpdateForMcpServer(ctx, tx, audit.LogMCPMetadataUpdateForMcpServerEvent{
		OrganizationID: patch.OrganizationID,
		ProjectID:      server.ProjectID,

		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, patch.ActorUserID),
		ActorDisplayName: patch.ActorEmail,
		ActorSlug:        nil,

		McpServerURN:  urn.NewMcpServer(server.ID),
		McpServerName: conv.FromPGTextOrEmpty[string](server.Name),
		McpServerSlug: conv.FromPGTextOrEmpty[string](server.Slug),

		MCPMetadataSnapshotBefore: before,
		MCPMetadataSnapshotAfter:  after,
	}); err != nil {
		return InstallPage{}, fmt.Errorf("log mcp server install page update: %w", err)
	}
	return installPageFromRow(&result), nil
}

// lockMCPServerMetadata resolves the row GetMcpMetadata reads for a server by
// mcp_server_id, under the save lock and row locks SetMcpMetadata takes. It
// also reports whether the server is backed by a toolset other than itself,
// whose metadata the install page falls back to when the server has no row.
func lockMCPServerMetadata(ctx context.Context, mcpr *repo.Queries, server mcpservers_repo.McpServer) (*repo.McpMetadatum, bool, error) {
	backend := &resolvedMetadataBackend{toolset: nil, mcpServer: &server}
	if err := mcpr.AcquireMetadataBackendLock(ctx, repo.AcquireMetadataBackendLockParams{ProjectID: server.ProjectID, BackendID: backend.backendID()}); err != nil {
		return nil, false, fmt.Errorf("lock mcp server install page: %w", err)
	}
	rows, err := mcpr.LockMetadataByBackendID(ctx, repo.LockMetadataByBackendIDParams{ProjectID: server.ProjectID, BackendID: backend.backendID()})
	if err != nil {
		return nil, false, fmt.Errorf("load mcp server install page: %w", err)
	}
	return winningMetadataRow(rows), server.ToolsetID.Valid && !isCanonicalWrapper(&server), nil
}

// patchLogoID parses and authorizes a supplied logo. SetMcpMetadata takes any
// asset ID; this path accepts only a live image the organization owns.
func patchLogoID(ctx context.Context, mcpr *repo.Queries, patch InstallPagePatch) (uuid.NullUUID, error) {
	if patch.LogoAssetID == nil || *patch.LogoAssetID == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(*patch.LogoAssetID)
	if err != nil || id == uuid.Nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ErrInstallPageLogoInvalid
	}
	found, err := mcpr.OrganizationImageAssetExists(ctx, repo.OrganizationImageAssetExistsParams{ID: id, OrganizationID: patch.OrganizationID})
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("check mcp server install page logo: %w", err)
	}
	if !found {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ErrInstallPageLogoInvalid
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func patchText(value *string) pgtype.Text {
	if value == nil || *value == "" {
		return pgtype.Text{String: "", Valid: false}
	}
	return conv.ToPGText(*value)
}

func installPageFromRow(row *repo.McpMetadatum) InstallPage {
	if row == nil {
		return InstallPage{
			LogoAssetID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ExternalDocumentationURL:  pgtype.Text{String: "", Valid: false},
			ExternalDocumentationText: pgtype.Text{String: "", Valid: false},
			Instructions:              pgtype.Text{String: "", Valid: false},
		}
	}
	return InstallPage{
		LogoAssetID:               row.LogoID,
		ExternalDocumentationURL:  row.ExternalDocumentationUrl,
		ExternalDocumentationText: row.ExternalDocumentationText,
		Instructions:              row.Instructions,
	}
}
