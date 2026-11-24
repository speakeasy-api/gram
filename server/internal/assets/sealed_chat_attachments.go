package assets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
)

// sealedChatAttachments keeps encrypted copies of chat attachments for the
// projects on the chat transcript shadow allowlists. The plaintext object
// stays in place; the encrypted copy sits next to it.
type sealedChatAttachments struct {
	store *transcriptstore.AssetStore
	// capture are the projects whose uploads also write an encrypted copy.
	capture map[uuid.UUID]struct{}
	// read are the projects whose downloads serve the encrypted copy when one
	// exists. Each also captures.
	read map[uuid.UUID]struct{}
}

// WithSealedChatAttachments seals chat attachments uploaded to the capture
// projects and serves the read projects' downloads from the encrypted copy.
// An attachment with no encrypted copy, such as one uploaded before capture
// began, is served from plaintext. A nil store leaves every project on
// plaintext.
func (s *Service) WithSealedChatAttachments(store *transcriptstore.AssetStore, captureProjectIDs, readProjectIDs []uuid.UUID) *Service {
	if store == nil {
		s.sealed = nil
		return s
	}
	sealed := &sealedChatAttachments{
		store:   store,
		capture: make(map[uuid.UUID]struct{}, len(captureProjectIDs)),
		read:    make(map[uuid.UUID]struct{}, len(readProjectIDs)),
	}
	for _, id := range captureProjectIDs {
		sealed.capture[id] = struct{}{}
	}
	for _, id := range readProjectIDs {
		sealed.read[id] = struct{}{}
	}
	s.sealed = sealed
	return s
}

func chatAttachmentIdentity(projectID uuid.UUID, contentHash string) transcriptcrypto.AssetIdentity {
	return transcriptcrypto.AssetIdentity{Kind: transcriptcrypto.AssetChatAttachment, ProjectID: projectID, ChatID: uuid.Nil, SHA256: contentHash}
}

// sealChatAttachment writes the encrypted copy of an uploaded attachment when
// the project captures encrypted copies. It runs before the asset row is
// created, so a failure leaves no row that points at an unsealed object.
func (s *Service) sealChatAttachment(ctx context.Context, organizationID string, projectID uuid.UUID, objectPath string, contentHash string, file *os.File) error {
	if s.sealed == nil {
		return nil
	}
	if _, enabled := s.sealed.capture[projectID]; !enabled {
		return nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind chat attachment for sealing: %w", err)
	}
	body, err := io.ReadAll(io.LimitReader(file, MaxFileSizeChatAttachment+1))
	if err != nil {
		return fmt.Errorf("read chat attachment for sealing: %w", err)
	}
	if err := s.sealed.store.Seal(ctx, organizationID, chatAttachmentIdentity(projectID, contentHash), objectPath, body); err != nil {
		return fmt.Errorf("seal chat attachment: %w", err)
	}
	return nil
}

// sealExistingChatAttachment seals the asset a deduplicated upload reuses,
// when the project captures encrypted copies and the asset has none yet, for
// example because it was uploaded before capture began. The upload's bytes are
// the asset's bytes: both hash to contentHash.
func (s *Service) sealExistingChatAttachment(ctx context.Context, organizationID string, projectID uuid.UUID, assetID string, contentHash string, file *os.File) error {
	if s.sealed == nil {
		return nil
	}
	if _, enabled := s.sealed.capture[projectID]; !enabled {
		return nil
	}
	id, err := uuid.Parse(assetID)
	if err != nil {
		return fmt.Errorf("parse reused chat attachment id: %w", err)
	}
	row, err := s.repo.GetChatAttachmentAssetURL(ctx, repo.GetChatAttachmentAssetURLParams{ID: id, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		// Another kind of asset with the same bytes. It is not served as a
		// chat attachment, so there is nothing to seal.
		return nil
	}
	if err != nil {
		return fmt.Errorf("load reused chat attachment: %w", err)
	}
	if row.Sha256 != contentHash || row.OrganizationID.String != organizationID {
		return nil
	}
	assetURL, err := url.Parse(row.Url)
	if err != nil {
		return fmt.Errorf("parse reused chat attachment url: %w", err)
	}
	sealed, err := s.sealed.store.HasSealedCopy(ctx, assetURL)
	if err != nil {
		return fmt.Errorf("check reused chat attachment for an encrypted copy: %w", err)
	}
	if sealed {
		return nil
	}
	objectPath, err := assetStorageKey(tierProject, projectID, organizationID, row.Name)
	if err != nil {
		return fmt.Errorf("resolve reused chat attachment storage key: %w", err)
	}
	// Seal only where readers will look: next to the stored object.
	if !strings.HasSuffix(assetURL.Host+assetURL.Path, objectPath) {
		return nil
	}
	return s.sealChatAttachment(ctx, organizationID, projectID, objectPath, contentHash, file)
}

// openSealedChatAttachment returns the attachment's encrypted copy for a read
// project. found is false when the project reads plaintext or the attachment
// has no encrypted copy. An encrypted copy that does not open is an error and
// is never replaced by plaintext.
func (s *Service) openSealedChatAttachment(ctx context.Context, projectID uuid.UUID, row repo.GetChatAttachmentAssetURLRow, assetURL *url.URL) (body io.ReadCloser, found bool, err error) {
	if s.sealed == nil {
		return nil, false, nil
	}
	if _, enabled := s.sealed.read[projectID]; !enabled {
		return nil, false, nil
	}
	if !row.OrganizationID.Valid || row.OrganizationID.String == "" || !transcriptcrypto.IsContentHash(row.Sha256) {
		// Only attachments uploaded with an organization and content hash
		// are sealed. A copy next to one without them cannot be verified,
		// and is never skipped in favour of plaintext.
		if err := s.sealed.store.RefuseUnverifiable(ctx, assetURL); err != nil {
			return nil, false, fmt.Errorf("open encrypted chat attachment: %w", err)
		}
		return nil, false, nil
	}
	opened, found, err := s.sealed.store.Open(ctx, row.OrganizationID.String, chatAttachmentIdentity(projectID, row.Sha256), assetURL, MaxFileSizeChatAttachment)
	if err != nil {
		return nil, false, fmt.Errorf("open encrypted chat attachment: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	return io.NopCloser(bytes.NewReader(opened)), true, nil
}
