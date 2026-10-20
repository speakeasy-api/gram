package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"

	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	operationUpdateMCPMetadata   = "update_mcp_metadata"
	receiptResultMetadataUpdated = "metadata_updated"

	maxLifecycleNameBytes              = 256
	maxLifecycleDocumentationURLBytes  = 2048
	maxLifecycleDocumentationTextBytes = 256
	maxLifecycleInstructionsBytes      = 16384
)

var (
	ErrLifecycleMetadataInvalid = errors.New("invalid platform mcp metadata update")
	// ErrLifecycleMetadataRenameUnsupported refuses a rename of a server this
	// platform did not register, or whose registration is not complete.
	ErrLifecycleMetadataRenameUnsupported = fmt.Errorf("%w: only a complete Platform-owned registration can be renamed", ErrLifecycleMetadataInvalid)
	// ErrLifecycleMetadataLogoInvalid refuses a logo that is not a live image
	// asset of the caller's organization.
	ErrLifecycleMetadataLogoInvalid = fmt.Errorf("%w: logo is not an image of this organization", ErrLifecycleMetadataInvalid)
	// ErrLifecycleMetadataInherited refuses an install-page patch for a server
	// whose install page shows its backing toolset's settings.
	ErrLifecycleMetadataInherited = fmt.Errorf("%w: install page is inherited from the backing toolset", ErrLifecycleMetadataInvalid)
)

// UpdateMCPMetadataInput patches one MCP server's details. A nil field is left
// unchanged; for the install-page fields an empty string clears the value.
// Name cannot be cleared, and renaming needs RegistrationID.
type UpdateMCPMetadataInput struct {
	ProjectSlug               string
	RegistrationID            string
	MCPID                     string
	Name                      *string
	LogoAssetID               *string
	ExternalDocumentationURL  *string
	ExternalDocumentationText *string
	Instructions              *string
	ExpectedVersion           string
	IdempotencyKey            string
}

type UpdateMCPMetadataResult struct {
	Project ResolvedProject
	// RegistrationID is set only for a rename, the one change that is bound to
	// a Platform registration.
	RegistrationID string
	MCPID          string
	Name           string
	Slug           string
	Visibility     string
	InstallPage    LifecycleInstallPage
	Version        string
	Receipt        OperationReceipt
}

// LifecycleInstallPage is an MCP server's install-page metadata: its logo,
// documentation link, and instructions. A nil field is unset.
type LifecycleInstallPage struct {
	LogoAssetID               *string
	ExternalDocumentationURL  *string
	ExternalDocumentationText *string
	Instructions              *string
}

// LifecycleMetadataService updates the details of an MCP server. Renames are
// limited to complete Platform-owned registrations and share the narrow
// mcpservers transaction command used by dashboard updates. Install-page
// metadata may be patched on any MCP server in the project through the narrow
// mcpmetadata patch command. Neither selects or changes plugin attachments.
type LifecycleMetadataService struct {
	db           *pgxpool.Pool
	updater      LifecycleMetadataUpdater
	installPages LifecycleInstallPageStore
	key          []byte
	now          func() time.Time
}

// LifecycleMetadataUpdater is supplied by server composition so Platform MCP
// invokes the same narrow mcpservers command as the dashboard without importing
// the dashboard package back into this boundary.
type LifecycleMetadataUpdater func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input LifecycleMetadataUpdate) (mcpserversrepo.McpServer, error)

type LifecycleMetadataUpdate struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ActorUserID    string
	ServerID       uuid.UUID
	Name           string
}

// LifecycleInstallPageStore is supplied by server composition for the same
// reason as LifecycleMetadataUpdater: both functions run the mcpmetadata
// package's narrow install-page commands in the caller's transaction, which
// already holds the server's row lock. Lock takes the save lock a dashboard
// metadata save takes and returns the current install page; Patch applies one
// patch and audits it. Patch reports ErrLifecycleMetadataLogoInvalid and
// ErrLifecycleMetadataInherited for the refusals it owns.
type LifecycleInstallPageStore struct {
	Lock  func(ctx context.Context, tx pgx.Tx, server mcpserversrepo.McpServer) (LifecycleInstallPage, error)
	Patch func(ctx context.Context, tx pgx.Tx, server mcpserversrepo.McpServer, patch LifecycleInstallPagePatch) (LifecycleInstallPage, error)
}

// LifecycleInstallPagePatch carries UpdateMCPMetadataInput's install-page
// fields with the same nil, empty, and set meanings.
type LifecycleInstallPagePatch struct {
	OrganizationID            string
	ProjectID                 uuid.UUID
	ActorUserID               string
	LogoAssetID               *string
	ExternalDocumentationURL  *string
	ExternalDocumentationText *string
	Instructions              *string
}

func (p LifecycleInstallPagePatch) empty() bool {
	return p.LogoAssetID == nil && p.ExternalDocumentationURL == nil && p.ExternalDocumentationText == nil && p.Instructions == nil
}

func NewLifecycleMetadataService(db *pgxpool.Pool, updater LifecycleMetadataUpdater, installPages LifecycleInstallPageStore, keyMaterial string) (*LifecycleMetadataService, error) {
	if db == nil || updater == nil || installPages.Lock == nil || installPages.Patch == nil || keyMaterial == "" {
		return nil, ErrLifecycleMetadataInvalid
	}
	return &LifecycleMetadataService{db: db, updater: updater, installPages: installPages, key: lifecycleMetadataVersionKey(keyMaterial), now: time.Now}, nil
}

func (s *LifecycleMetadataService) Update(ctx context.Context, principal Principal, input UpdateMCPMetadataInput) (UpdateMCPMetadataResult, error) {
	if s == nil || s.db == nil || s.updater == nil || s.installPages.Lock == nil || s.installPages.Patch == nil || len(s.key) == 0 || principal.OrganizationID == "" || principal.UserID == "" || input.ProjectSlug == "" || input.MCPID == "" || input.ExpectedVersion == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	name, err := validateLifecycleMetadataInput(input)
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	rename := input.Name != nil
	registrationID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if rename {
		parsed, err := uuid.Parse(input.RegistrationID)
		if err != nil {
			return UpdateMCPMetadataResult{}, ErrLifecycleMetadataRenameUnsupported
		}
		registrationID = uuid.NullUUID{UUID: parsed, Valid: true}
	}
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	project, err := NewRegistrationStore(s.db)
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	resolvedProject, err := project.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	inputHash := lifecycleMetadataInputHash(resolvedProject.ID, registrationID, mcpID, name, input)
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	patch := LifecycleInstallPagePatch{
		OrganizationID:            principal.OrganizationID,
		ProjectID:                 resolvedProject.ID,
		ActorUserID:               principal.UserID,
		LogoAssetID:               input.LogoAssetID,
		ExternalDocumentationURL:  input.ExternalDocumentationURL,
		ExternalDocumentationText: input.ExternalDocumentationText,
		Instructions:              input.Instructions,
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("begin platform mcp metadata update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	receiptLookup := platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		UserID:         conv.ToPGText(principal.UserID),
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      resolvedProject.ID,
		Operation:      operationUpdateMCPMetadata,
		IdempotencyKey: input.IdempotencyKey,
	}
	if err := q.LockPlatformMCPOperationReceipt(ctx, platformrepo.LockPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      resolvedProject.ID.String(),
		Operation:      operationUpdateMCPMetadata,
		IdempotencyKey: input.IdempotencyKey,
	}); err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("lock platform mcp metadata receipt: %w", err)
	}
	if _, err := q.DeleteExpiredPlatformMCPOperationReceipt(ctx, platformrepo.DeleteExpiredPlatformMCPOperationReceiptParams(receiptLookup)); err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("reclaim platform mcp metadata receipt: %w", err)
	}
	storedReceipt, err := q.GetPlatformMCPOperationReceipt(ctx, receiptLookup)
	if err == nil {
		if storedReceipt.InputHash != inputHash {
			return UpdateMCPMetadataResult{}, ErrRegistrationConflict
		}
		if storedReceipt.Status != receiptStatusSucceeded || storedReceipt.RegistrationID != registrationID {
			return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
		}
		result, err := s.currentResult(ctx, principal, resolvedProject, registrationID, mcpID, operationReceiptFromRow(storedReceipt, true))
		if err != nil {
			return UpdateMCPMetadataResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return UpdateMCPMetadataResult{}, fmt.Errorf("commit platform mcp metadata replay: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPMetadataResult{}, fmt.Errorf("load platform mcp metadata receipt: %w", err)
	}

	if rename {
		if err := requireRenameableRegistration(ctx, q, principal, resolvedProject.ID, registrationID.UUID, mcpID); err != nil {
			return UpdateMCPMetadataResult{}, err
		}
	}
	// Without a registration the server is identified by its ID within the
	// resolved project alone; the row lock serializes this update with every
	// other Platform lifecycle write to the same server.
	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: resolvedProject.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("lock platform mcp metadata target: %w", err)
	}
	// Read under the dashboard's metadata save lock, so a concurrent logo
	// change from either surface is caught by the version check.
	page, err := s.installPages.Lock(ctx, tx, server)
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("lock platform mcp install page: %w", err)
	}
	if !s.validVersion(server, page, input.ExpectedVersion) {
		return UpdateMCPMetadataResult{}, ErrRegistrationConflict
	}

	receipt, err := q.CreatePlatformMCPOperationReceipt(ctx, platformrepo.CreatePlatformMCPOperationReceiptParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            resolvedProject.ID,
		RegistrationID:       registrationID,
		ConnectionID:         connectionID,
		ConnectionGeneration: generation,
		UserID:               conv.ToPGText(principal.UserID),
		ActingSurface:        conv.ToPGText(string(principal.surface())),
		Operation:            operationUpdateMCPMetadata,
		IdempotencyKey:       input.IdempotencyKey,
		InputHash:            inputHash,
		Status:               receiptStatusPending,
		ResultCode:           pgtype.Text{String: "", Valid: false},
		ResultPayload:        nil,
		ExpiresAt:            pgtype.Timestamptz{Time: s.now().Add(receiptLifetime), InfinityModifier: pgtype.Finite, Valid: true},
	})
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("create platform mcp metadata receipt: %w", err)
	}
	updated := server
	if rename {
		updated, err = s.updater(ctx, tx, server, LifecycleMetadataUpdate{
			OrganizationID: principal.OrganizationID,
			ProjectID:      resolvedProject.ID,
			ActorUserID:    principal.UserID,
			ServerID:       mcpID,
			Name:           name,
		})
		if err != nil {
			return UpdateMCPMetadataResult{}, fmt.Errorf("update platform mcp metadata: %w", err)
		}
	}
	if !patch.empty() {
		page, err = s.installPages.Patch(ctx, tx, updated, patch)
		if errors.Is(err, ErrLifecycleMetadataInvalid) {
			return UpdateMCPMetadataResult{}, err
		}
		if err != nil {
			return UpdateMCPMetadataResult{}, fmt.Errorf("update platform mcp install page: %w", err)
		}
	}
	completed, err := q.CompletePlatformMCPOperationReceipt(ctx, platformrepo.CompletePlatformMCPOperationReceiptParams{
		RegistrationID: registrationID,
		Status:         receiptStatusSucceeded,
		ResultCode:     conv.ToPGText(receiptResultMetadataUpdated),
		ResultPayload:  nil,
		ID:             receipt.ID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("complete platform mcp metadata receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("commit platform mcp metadata update: %w", err)
	}
	return s.result(resolvedProject, registrationID, updated, page, operationReceiptFromRow(completed, false)), nil
}

// requireRenameableRegistration keeps renames to the complete Platform-owned
// registrations this surface created.
func requireRenameableRegistration(ctx context.Context, q *platformrepo.Queries, principal Principal, projectID, registrationID, mcpID uuid.UUID) error {
	registration, err := lifecycleRegistration(ctx, q, principal, projectID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLifecycleMetadataRenameUnsupported
	}
	if err != nil {
		return err
	}
	if registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) || !registration.RemoteMcpServerOwned || !registration.UserSessionIssuerOwned || !registration.McpServerOwned || !registration.McpEndpointOwned || !registration.McpServerID.Valid || registration.McpServerID.UUID != mcpID {
		return ErrLifecycleMetadataRenameUnsupported
	}
	return nil
}

func (s *LifecycleMetadataService) currentResult(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.NullUUID, mcpID uuid.UUID, receipt OperationReceipt) (UpdateMCPMetadataResult, error) {
	q := platformrepo.New(s.db)
	if registrationID.Valid {
		if err := requireRenameableRegistration(ctx, q, principal, project.ID, registrationID.UUID, mcpID); err != nil {
			return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
		}
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: project.ID})
	if err != nil {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	page, err := lifecycleInstallPage(ctx, q, project.ID, mcpID)
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	return s.result(project, registrationID, server, page, receipt), nil
}

func (s *LifecycleMetadataService) result(project ResolvedProject, registrationID uuid.NullUUID, server mcpserversrepo.McpServer, page LifecycleInstallPage, receipt OperationReceipt) UpdateMCPMetadataResult {
	result := UpdateMCPMetadataResult{
		Project: project, RegistrationID: "", MCPID: server.ID.String(), Name: lifecycleMCPDisplayName(server), Slug: server.Slug.String,
		Visibility: server.Visibility, InstallPage: page, Version: s.version(server, page), Receipt: receipt,
	}
	if registrationID.Valid {
		result.RegistrationID = registrationID.UUID.String()
	}
	return result
}

func (s *LifecycleMetadataService) version(server mcpserversrepo.McpServer, page LifecycleInstallPage) string {
	return lifecycleServerVersion(s.key, server, page)
}

func (s *LifecycleMetadataService) validVersion(server mcpserversrepo.McpServer, page LifecycleInstallPage, value string) bool {
	return hmac.Equal([]byte(s.version(server, page)), []byte(value))
}

// validateLifecycleMetadataInput checks the shape of a patch and returns the
// trimmed name, which is empty when the patch does not rename.
func validateLifecycleMetadataInput(input UpdateMCPMetadataInput) (string, error) {
	if input.Name == nil && input.LogoAssetID == nil && input.ExternalDocumentationURL == nil && input.ExternalDocumentationText == nil && input.Instructions == nil {
		return "", ErrLifecycleMetadataInvalid
	}
	name := ""
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if name == "" || len(name) > maxLifecycleNameBytes || strings.ContainsAny(name, "\r\n") {
			return "", ErrLifecycleMetadataInvalid
		}
		if input.RegistrationID == "" {
			return "", ErrLifecycleMetadataRenameUnsupported
		}
	}
	if input.LogoAssetID != nil && *input.LogoAssetID != "" {
		if id, err := uuid.Parse(*input.LogoAssetID); err != nil || id == uuid.Nil {
			return "", ErrLifecycleMetadataLogoInvalid
		}
	}
	if input.ExternalDocumentationURL != nil && *input.ExternalDocumentationURL != "" && !validLifecycleDocumentationURL(*input.ExternalDocumentationURL) {
		return "", ErrLifecycleMetadataInvalid
	}
	if input.ExternalDocumentationText != nil && (len(*input.ExternalDocumentationText) > maxLifecycleDocumentationTextBytes || strings.ContainsAny(*input.ExternalDocumentationText, "\r\n")) {
		return "", ErrLifecycleMetadataInvalid
	}
	if input.Instructions != nil && len(*input.Instructions) > maxLifecycleInstructionsBytes {
		return "", ErrLifecycleMetadataInvalid
	}
	return name, nil
}

// validLifecycleDocumentationURL admits the absolute HTTPS links the install
// page renders as a documentation button.
func validLifecycleDocumentationURL(raw string) bool {
	if len(raw) > maxLifecycleDocumentationURLBytes {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

// lifecycleInstallPage reads the install page the inventory reports for a
// server, outside the metadata save lock.
func lifecycleInstallPage(ctx context.Context, q *platformrepo.Queries, projectID, mcpID uuid.UUID) (LifecycleInstallPage, error) {
	row, err := q.GetPlatformMCPInstallPage(ctx, platformrepo.GetPlatformMCPInstallPageParams{ProjectID: projectID, McpServerID: mcpID})
	if errors.Is(err, pgx.ErrNoRows) {
		return LifecycleInstallPageFromColumns(uuid.NullUUID{UUID: uuid.Nil, Valid: false}, pgtype.Text{String: "", Valid: false}, pgtype.Text{String: "", Valid: false}, pgtype.Text{String: "", Valid: false}), nil
	}
	if err != nil {
		return LifecycleInstallPage{}, fmt.Errorf("read platform mcp install page: %w", err)
	}
	return LifecycleInstallPageFromColumns(row.LogoID, row.ExternalDocumentationUrl, row.ExternalDocumentationText, row.Instructions), nil
}

// LifecycleInstallPageFromColumns reads an install page from its stored
// mcp_metadata columns.
func LifecycleInstallPageFromColumns(logoID uuid.NullUUID, documentationURL, documentationText, instructions pgtype.Text) LifecycleInstallPage {
	page := LifecycleInstallPage{
		LogoAssetID:               nil,
		ExternalDocumentationURL:  conv.FromPGText[string](documentationURL),
		ExternalDocumentationText: conv.FromPGText[string](documentationText),
		Instructions:              conv.FromPGText[string](instructions),
	}
	if logoID.Valid {
		page.LogoAssetID = new(logoID.UUID.String())
	}
	return page
}

func mcpInstallPageFromLifecycle(page LifecycleInstallPage) *MCPInstallPage {
	return &MCPInstallPage{
		LogoAssetID:               conv.PtrValOr(page.LogoAssetID, ""),
		ExternalDocumentationURL:  conv.PtrValOr(page.ExternalDocumentationURL, ""),
		ExternalDocumentationText: conv.PtrValOr(page.ExternalDocumentationText, ""),
		Instructions:              conv.PtrValOr(page.Instructions, ""),
	}
}

// lifecycleMetadataInputHash binds an idempotency key to one exact patch. Each
// optional field hashes as "-" when omitted and "+" and its value when given,
// so omitting a field and clearing it are different requests.
func lifecycleMetadataInputHash(projectID uuid.UUID, registrationID uuid.NullUUID, mcpID uuid.UUID, name string, input UpdateMCPMetadataInput) string {
	registration := ""
	if registrationID.Valid {
		registration = registrationID.UUID.String()
	}
	trimmedName := input.Name
	if trimmedName != nil {
		trimmedName = &name
	}
	fields := []string{projectID.String(), registration, mcpID.String()}
	for _, value := range []*string{trimmedName, input.LogoAssetID, input.ExternalDocumentationURL, input.ExternalDocumentationText, input.Instructions} {
		fields = append(fields, lifecyclePatchField(value))
	}
	fields = append(fields, input.ExpectedVersion)
	digest := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func lifecyclePatchField(value *string) string {
	if value == nil {
		return "-"
	}
	return "+" + *value
}

// lifecycleMetadataVersion is an opaque optimistic-concurrency token over the
// persisted fields update_mcp_metadata and the visibility tools can change:
// the server's name, slug, and visibility, and its install page. It is not an
// authorization token; live authorization and registration ownership are
// always checked separately.
func lifecycleMetadataVersionKey(keyMaterial string) []byte {
	digest := sha256.Sum256([]byte("platform-mcp-lifecycle-metadata:" + keyMaterial))
	return digest[:]
}

func lifecycleMetadataVersion(key []byte, mcpID, projectID, name, slug, visibility string, page LifecycleInstallPage) string {
	fields := []string{mcpID, projectID, name, slug, visibility}
	// "" for unset and "=" and the value for set keep NULL apart from a stored
	// empty string. Postgres text cannot hold NUL, so the separator is safe.
	for _, value := range []*string{page.LogoAssetID, page.ExternalDocumentationURL, page.ExternalDocumentationText, page.Instructions} {
		if value == nil {
			fields = append(fields, "")
			continue
		}
		fields = append(fields, "="+*value)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(strings.Join(fields, "\x00")))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func lifecycleServerVersion(key []byte, server mcpserversrepo.McpServer, page LifecycleInstallPage) string {
	return lifecycleMetadataVersion(key, server.ID.String(), server.ProjectID.String(), lifecycleMCPDisplayName(server), server.Slug.String, server.Visibility, page)
}

func lifecycleMCPDisplayName(server mcpserversrepo.McpServer) string {
	if server.Name.Valid && server.Name.String != "" {
		return server.Name.String
	}
	if server.Slug.Valid && server.Slug.String != "" {
		return server.Slug.String
	}
	return server.ID.String()
}
