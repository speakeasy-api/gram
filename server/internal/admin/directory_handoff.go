package admin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type AdminWorkOS interface {
	orgprovision.WorkOSOrganizationCreator
	WorkOSDirectoryReader
}

type WorkOSDirectoryReader interface {
	ListDirectories(ctx context.Context, organizationID string) ([]workos.Directory, error)
}

func (s *Service) GetOrganizationDirectoryHandoff(ctx context.Context, payload *gen.GetOrganizationDirectoryHandoffPayload) (*gen.DirectoryHandoffResult, error) {
	organization, err := s.canonicalAdminOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	result := &gen.DirectoryHandoffResult{
		Handoff:           nil,
		WorkosEnvironment: normalizedWorkOSEnvironment(s.workosEnvironment),
	}
	row, err := repo.New(s.db).AdminGetOrganizationDirectoryHandoff(ctx, organization.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load organization directory handoff").LogError(ctx, s.logger)
	}

	handoff := directoryHandoffView(
		row.OrganizationID,
		row.DirectoryScimBaseUrl,
		row.DirectoryScimTokenFingerprint,
		row.DirectoryHandoffSetByUserID,
		row.DirectoryHandoffUpdatedAt,
		nil,
	)
	result.Handoff = handoff

	workosOrganizationID := conv.FromPGTextOrEmpty[string](organization.WorkosID)
	if workosOrganizationID == "" {
		return result, nil
	}
	if s.directoryWorkOS == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "WorkOS directory lookup is unavailable").LogWarn(ctx, s.logger)
	}

	directories, err := s.directoryWorkOS.ListDirectories(ctx, workosOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list WorkOS directories").LogError(ctx, s.logger)
	}
	directory := selectDirectoryForHandoff(directories, conv.FromPGTextOrEmpty[string](row.DirectoryWorkosID))
	if directory == nil {
		return result, nil
	}

	handoff.WorkosDirectoryID = conv.PtrEmpty(directory.ID)
	handoff.WorkosDirectoryState = conv.PtrEmpty(directory.State)
	if !row.DirectoryWorkosID.Valid {
		err := repo.New(s.db).AdminCacheOrganizationDirectoryWorkOSID(ctx, repo.AdminCacheOrganizationDirectoryWorkOSIDParams{
			DirectoryWorkosID: conv.ToPGText(directory.ID),
			OrganizationID:    organization.ID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "cache WorkOS directory id").LogError(ctx, s.logger)
		}
	}

	return result, nil
}

func (s *Service) SetOrganizationDirectoryHandoff(ctx context.Context, payload *gen.SetOrganizationDirectoryHandoffPayload) (*gen.DirectoryHandoff, error) {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	baseURL, err := validateDirectorySCIMBaseURL(payload.ScimBaseURL)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid SCIM base URL")
	}
	token := strings.TrimSpace(payload.ScimToken)
	if token == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "SCIM token is required")
	}
	if s.applicationEncryption == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "directory handoff encryption is unavailable").LogWarn(ctx, s.logger)
	}
	_, _, operatorEmail := adminActor(ctx)
	if operatorEmail == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	encryptedToken, err := s.applicationEncryption.Encrypt([]byte(token))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt directory handoff token").LogError(ctx, s.logger)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))[:8]

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin directory handoff transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	row, err := repo.New(tx).AdminSetOrganizationDirectoryHandoff(ctx, repo.AdminSetOrganizationDirectoryHandoffParams{
		OrganizationID:                organizationID,
		DirectoryScimBaseUrl:          conv.ToPGText(baseURL.String()),
		DirectoryScimTokenEncrypted:   conv.ToPGText(encryptedToken),
		DirectoryScimTokenFingerprint: conv.ToPGText(fingerprint),
		DirectoryHandoffSetByUserID:   conv.ToPGText(*operatorEmail),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store organization directory handoff").LogError(ctx, s.logger)
	}
	actor, displayName, _ := adminActor(ctx)
	if err := s.audit.LogDirectoryHandoffSet(ctx, tx, audit.LogDirectoryHandoffEvent{
		OrganizationID:      organizationID,
		Actor:               actor,
		ActorDisplayName:    displayName,
		ActorSlug:           nil,
		DirectoryHandoffURN: urn.NewDirectoryHandoff(organizationID),
		BaseURLHost:         baseURL.Hostname(),
		TokenFingerprint:    fingerprint,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit directory handoff set").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit directory handoff transaction").LogError(ctx, s.logger)
	}

	return directoryHandoffView(
		row.OrganizationID,
		row.DirectoryScimBaseUrl,
		row.DirectoryScimTokenFingerprint,
		row.DirectoryHandoffSetByUserID,
		row.DirectoryHandoffUpdatedAt,
		nil,
	), nil
}

func (s *Service) ClearOrganizationDirectoryHandoff(ctx context.Context, payload *gen.ClearOrganizationDirectoryHandoffPayload) error {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin directory handoff transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	row, err := queries.AdminGetOrganizationDirectoryHandoff(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "commit empty directory handoff clear").LogError(ctx, s.logger)
		}
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load organization directory handoff for clear").LogError(ctx, s.logger)
	}
	if err := queries.AdminClearOrganizationDirectoryHandoff(ctx, organizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "clear organization directory handoff").LogError(ctx, s.logger)
	}

	baseURL, err := url.Parse(conv.FromPGTextOrEmpty[string](row.DirectoryScimBaseUrl))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse stored directory handoff URL").LogError(ctx, s.logger)
	}
	actor, displayName, _ := adminActor(ctx)
	if err := s.audit.LogDirectoryHandoffCleared(ctx, tx, audit.LogDirectoryHandoffEvent{
		OrganizationID:      organizationID,
		Actor:               actor,
		ActorDisplayName:    displayName,
		ActorSlug:           nil,
		DirectoryHandoffURN: urn.NewDirectoryHandoff(organizationID),
		BaseURLHost:         baseURL.Hostname(),
		TokenFingerprint:    conv.FromPGTextOrEmpty[string](row.DirectoryScimTokenFingerprint),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit directory handoff clear").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit directory handoff transaction").LogError(ctx, s.logger)
	}
	return nil
}

func validateDirectorySCIMBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("URL must be absolute and use HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("URL must not contain credentials, a query, or a fragment")
	}
	return parsed, nil
}

func normalizedWorkOSEnvironment(value string) string {
	switch value {
	case "development", "production", "unknown":
		return value
	default:
		return "unknown"
	}
}

func selectDirectoryForHandoff(directories []workos.Directory, storedID string) *workos.Directory {
	if storedID != "" {
		for i := range directories {
			if directories[i].ID == storedID {
				return &directories[i]
			}
		}
		return nil
	}
	if len(directories) == 1 {
		return &directories[0]
	}
	var linked *workos.Directory
	for i := range directories {
		if directories[i].State != "linked" {
			continue
		}
		if linked != nil {
			return nil
		}
		linked = &directories[i]
	}
	return linked
}

func directoryHandoffView(
	organizationID string,
	baseURL pgtype.Text,
	fingerprint pgtype.Text,
	setBy pgtype.Text,
	updatedAt pgtype.Timestamptz,
	directory *workos.Directory,
) *gen.DirectoryHandoff {
	result := &gen.DirectoryHandoff{
		OrganizationID:       organizationID,
		ScimBaseURL:          conv.FromPGTextOrEmpty[string](baseURL),
		TokenFingerprint:     conv.FromPGTextOrEmpty[string](fingerprint),
		WorkosDirectoryID:    nil,
		WorkosDirectoryState: nil,
		SetBy:                conv.FromPGTextOrEmpty[string](setBy),
		UpdatedAt:            updatedAt.Time.Format(time.RFC3339),
	}
	if directory != nil {
		result.WorkosDirectoryID = conv.PtrEmpty(directory.ID)
		result.WorkosDirectoryState = conv.PtrEmpty(directory.State)
	}
	return result
}
