//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image"
	_ "image/gif"  // Registers GIF dimensions for the approval preview.
	_ "image/jpeg" // Registers JPEG dimensions for the approval preview.
	_ "image/png"  // Registers PNG dimensions for the approval preview.
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	admingen "github.com/speakeasy-api/gram/server/gen/admin_assets"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

const (
	platformImageUploadPrefix = "adminMCPImageUpload:"
	platformImageUsedPrefix   = "adminMCPImageUploadUsed:"
	// platformImageUploadWindow is how long a signed upload URL accepts bytes.
	platformImageUploadWindow = 15 * time.Minute
	// platformImageRetention keeps uploaded bytes until the proposal they
	// back can no longer be approved or executed.
	platformImageRetention = platformImageUploadWindow + proposalLifetime + 5*time.Minute
	maxPlatformImageBytes  = assets.MaxFileSizeImage
)

// platformImageTypes matches the dashboard's platform image allowlist. SVG is
// excluded because it can carry script.
var platformImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// PlatformImageStore stores a publicly served platform-tier image.
type PlatformImageStore interface {
	StorePlatformImage(ctx context.Context, logger *slog.Logger, operatorEmail string, reader io.Reader, contentLength int64, contentType string) (*admingen.UploadImageResult, error)
}

type PrepareUploadPlatformImageInput struct{}

type PlatformImageUploadOutput struct {
	UploadURL            string   `json:"upload_url"`
	Method               string   `json:"method"`
	MaxBytes             int64    `json:"max_bytes"`
	AllowedContentTypes  []string `json:"allowed_content_types"`
	ExpiresAt            string   `json:"expires_at"`
	ExampleCommand       string   `json:"example_command"`
	ApprovalInstructions string   `json:"approval_instructions"`
}

// platformImageSlot is a single-use upload URL and, once used, the uploaded
// bytes awaiting approval. The token is stored only as a hash.
type platformImageSlot struct {
	ID         uuid.UUID     `json:"id"`
	TokenHash  string        `json:"token_hash"`
	Owner      proposalOwner `json:"owner"`
	StaffEmail string        `json:"staff_email"`
	ExpiresAt  time.Time     `json:"expires_at"`
	Uploaded   bool          `json:"uploaded"`
	SHA256     string        `json:"sha256,omitempty"`
	MimeType   string        `json:"mime_type,omitempty"`
	Data       []byte        `json:"data,omitempty"`
}

type platformImageArguments struct {
	UploadID string `json:"upload_id"`
	SHA256   string `json:"sha256"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
}

type platformImageExpected struct {
	SHA256 string `json:"sha256"`
}

type platformImagePreview struct {
	SHA256   string `json:"sha256"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

type platformImageReceipt struct {
	AssetID  string `json:"asset_id"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	MimeType string `json:"mime_type"`
}

// platformImageWriter publishes one staff-uploaded image as a platform asset.
// Bytes arrive out of band on a signed URL because MCP tool arguments are
// JSON and the Admin MCP request body is capped well below image sizes.
type platformImageWriter struct {
	store   *proposalStore
	cache   cache.Cache
	images  PlatformImageStore
	siteURL *url.URL
	writes  WriteConfig
	baseURL string
	logger  *slog.Logger
	now     func() time.Time
}

func (s *platformImageWriter) loadSlot(ctx context.Context, id string) (platformImageSlot, error) {
	var slot platformImageSlot
	if err := s.cache.Get(ctx, platformImageUploadPrefix+id, &slot); err != nil {
		return platformImageSlot{}, fmt.Errorf("load platform image upload: %w", err)
	}
	return slot, nil
}

func (s *platformImageWriter) prepare(ctx context.Context) (PlatformImageUploadOutput, error) {
	authority, err := requireWriteAuthority(ctx, s.writes, OperationPublishPlatformImage)
	if err != nil {
		return PlatformImageUploadOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return PlatformImageUploadOutput{}, err
	}
	token, err := staffOpaqueToken()
	if err != nil {
		return PlatformImageUploadOutput{}, fmt.Errorf("create upload token: %w", err)
	}
	slot := platformImageSlot{ID: uuid.New(), TokenHash: staffTokenHash(token), Owner: owner, StaffEmail: authority.Staff.Email, ExpiresAt: s.now().Add(platformImageUploadWindow)}
	if err := s.cache.Set(ctx, platformImageUploadPrefix+slot.ID.String(), slot, platformImageRetention); err != nil {
		return PlatformImageUploadOutput{}, fmt.Errorf("store platform image upload: %w", err)
	}
	uploadURL := s.baseURL + "/uploads/" + slot.ID.String() + "/" + token
	return PlatformImageUploadOutput{
		UploadURL: uploadURL, Method: http.MethodPut, MaxBytes: maxPlatformImageBytes, AllowedContentTypes: slices.Clone(platformImageTypes),
		ExpiresAt:            slot.ExpiresAt.UTC().Format(time.RFC3339),
		ExampleCommand:       "curl -fsS -X PUT --data-binary @icon.png '" + uploadURL + "'",
		ApprovalInstructions: "The URL accepts one upload. Its JSON response is a pending proposal with an approval_url; approve it in the admin site, then call execute_admin_proposal with its proposal_id.",
	}, nil
}

// UploadHandler accepts one image on a signed URL from prepare_upload_platform_image
// and answers with the pending proposal that would publish it.
func (s *platformImageWriter) UploadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPut {
			w.Header().Set("Allow", http.MethodPut)
			staffJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use PUT"})
			return
		}
		if !s.writes.OperationEnabled(OperationPublishPlatformImage) {
			staffJSON(w, http.StatusForbidden, map[string]string{"error": ErrWriteDisabled.Error()})
			return
		}
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		slot, err := s.loadSlot(r.Context(), id.String())
		if err != nil || slot.ID != id || !digestsEqual(slot.TokenHash, staffTokenHash(r.PathValue("token"))) {
			http.NotFound(w, r)
			return
		}
		if slot.Uploaded || !s.now().Before(slot.ExpiresAt) {
			staffJSON(w, http.StatusGone, map[string]string{"error": "this upload URL was already used or has expired; prepare a new one"})
			return
		}
		first, err := s.cache.Add(r.Context(), platformImageUsedPrefix+id.String(), platformImageRetention)
		if err != nil {
			staffJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "upload unavailable"})
			return
		}
		if !first {
			staffJSON(w, http.StatusGone, map[string]string{"error": "this upload URL was already used; prepare a new one"})
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPlatformImageBytes))
		if err != nil {
			staffJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "images are limited to 4 MiB; prepare a new upload URL"})
			return
		}
		mimeType := http.DetectContentType(data)
		if len(data) == 0 || !slices.Contains(platformImageTypes, mimeType) {
			staffJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "upload a PNG, JPEG, GIF or WebP image; prepare a new upload URL"})
			return
		}
		sum := sha256.Sum256(data)
		slot.Uploaded, slot.SHA256, slot.MimeType, slot.Data = true, hex.EncodeToString(sum[:]), mimeType, data
		if err := s.cache.Set(r.Context(), platformImageUploadPrefix+id.String(), slot, platformImageRetention); err != nil {
			staffJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "upload unavailable"})
			return
		}
		proposal, err := s.createProposal(r.Context(), slot)
		if err != nil {
			staffJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		staffJSON(w, http.StatusOK, proposal)
	})
}

func (s *platformImageWriter) createProposal(ctx context.Context, slot platformImageSlot) (ProposalOutput, error) {
	arguments, err := json.Marshal(platformImageArguments{UploadID: slot.ID.String(), SHA256: slot.SHA256, MimeType: slot.MimeType, Size: len(slot.Data)})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode platform image arguments: %w", err)
	}
	expected, err := json.Marshal(platformImageExpected{SHA256: slot.SHA256})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode expected platform image state: %w", err)
	}
	preview := platformImagePreview{SHA256: slot.SHA256, MimeType: slot.MimeType, Size: len(slot.Data)}
	if config, _, err := image.DecodeConfig(bytes.NewReader(slot.Data)); err == nil {
		preview.Width, preview.Height = config.Width, config.Height
	}
	encodedPreview, err := json.Marshal(preview)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode platform image preview: %w", err)
	}
	proposal, replay, err := s.store.Create(ctx, slot.Owner, NewProposal{Operation: OperationPublishPlatformImage, SchemaVersion: 1, IdempotencyKey: "upload:" + slot.ID.String(), Arguments: arguments, ExpectedState: expected, Preview: encodedPreview}, s.now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, s.baseURL), nil
}

// uploadedImage returns the stored bytes for a proposal after checking that
// they are still the exact bytes the proposal digested.
func (s *platformImageWriter) uploadedImage(ctx context.Context, proposal Proposal) (platformImageSlot, error) {
	if proposal.Operation != OperationPublishPlatformImage || proposal.SchemaVersion != 1 || !proposal.PlatformGlobal || proposal.Target.OrganizationID != "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return platformImageSlot{}, ErrProposalInvalidated
	}
	var args platformImageArguments
	if err := json.Unmarshal(proposal.Arguments, &args); err != nil || args.UploadID == "" || args.SHA256 == "" {
		return platformImageSlot{}, ErrProposalInvalidated
	}
	slot, err := s.loadSlot(ctx, args.UploadID)
	if err != nil || !slot.Uploaded || slot.Owner.SubjectURN != proposal.SubjectURN {
		return platformImageSlot{}, ErrProposalInvalidated
	}
	sum := sha256.Sum256(slot.Data)
	if hex.EncodeToString(sum[:]) != args.SHA256 || slot.SHA256 != args.SHA256 || slot.MimeType != args.MimeType || len(slot.Data) != args.Size {
		return platformImageSlot{}, ErrProposalInvalidated
	}
	expected, err := json.Marshal(platformImageExpected{SHA256: args.SHA256})
	if err != nil {
		return platformImageSlot{}, fmt.Errorf("encode expected platform image state: %w", err)
	}
	digest, err := stateDigest(expected)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return platformImageSlot{}, ErrStaleState
	}
	return slot, nil
}

func (s *platformImageWriter) revalidate(ctx context.Context, _ pgx.Tx, proposal Proposal) error {
	_, err := s.uploadedImage(ctx, proposal)
	return err
}

func (s *platformImageWriter) view(ctx context.Context, proposal Proposal) (proposalView, error) {
	slot, err := s.uploadedImage(ctx, proposal)
	if err != nil {
		return proposalView{}, err
	}
	var preview platformImagePreview
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.SHA256 != slot.SHA256 || !slices.Contains(platformImageTypes, slot.MimeType) {
		return proposalView{}, ErrProposalInvalidated
	}
	dimensions := "unknown"
	if preview.Width > 0 && preview.Height > 0 {
		dimensions = strconv.Itoa(preview.Width) + "×" + strconv.Itoa(preview.Height)
	}
	return proposalView{
		Summary:        "Publish an uploaded image as a public Speakeasy asset",
		PlatformGlobal: true,
		Changes: []proposalViewChange{
			{Setting: "Content type", Before: "(not published)", After: preview.MimeType},
			{Setting: "Size", Before: "(not published)", After: strconv.Itoa(preview.Size) + " bytes"},
			{Setting: "Dimensions", Before: "(not published)", After: dimensions},
			{Setting: "SHA-256", Before: "(not published)", After: preview.SHA256},
		},
		// The bytes were sniffed against the raster allowlist at upload, so
		// the data URI cannot carry markup.
		Image:       template.URL("data:" + slot.MimeType + ";base64," + base64.StdEncoding.EncodeToString(slot.Data)), // #nosec G203 -- MIME type is from the sniffed raster allowlist.
		SideEffects: "Anyone can fetch the image from its Speakeasy URL without signing in. Identical images share one asset.",
	}, nil
}

func (s *platformImageWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{Run: func(ctx context.Context, _ pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
		slot, err := s.uploadedImage(ctx, proposal)
		if err != nil {
			return "", nil, err
		}
		// Platform assets upsert by content hash, so a retried execution
		// resolves to the same asset rather than publishing a duplicate.
		stored, err := s.images.StorePlatformImage(ctx, s.logger, authority.Staff.Email, bytes.NewReader(slot.Data), int64(len(slot.Data)), slot.MimeType)
		if err != nil {
			return "", nil, fmt.Errorf("store platform image: %w", err)
		}
		if stored == nil || stored.Asset == nil {
			return "", nil, errors.New("store platform image: no asset returned")
		}
		assetID, err := uuid.Parse(stored.Asset.ID)
		if err != nil {
			return "", nil, fmt.Errorf("parse platform asset id: %w", err)
		}
		result, err := json.Marshal(platformImageReceipt{AssetID: assetID.String(), URL: assets.ServeImageURL(s.siteURL, assetID), SHA256: stored.Asset.Sha256, MimeType: slot.MimeType})
		if err != nil {
			return "", nil, fmt.Errorf("encode platform image receipt: %w", err)
		}
		return "succeeded", result, nil
	}}
}

func (s *platformImageWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_upload_platform_image", Title: "Prepare Public Image Upload", Description: "Create a single-use signed URL for uploading one image, for example a registry entry icon. PUT the raw file bytes to upload_url (PNG, JPEG, GIF or WebP, up to 4 MiB; SVG is refused) from a shell with access to the admin site. The PUT response is a pending proposal: approve it in the admin site, where the image is shown, then call execute_admin_proposal. The receipt returns the asset ID and its public URL, usable as a registry entry's server.icons[].src. Registry icons accept PNG, JPEG or WebP. Requires admin:write.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true)}}, func(ctx context.Context, _ *mcp.CallToolRequest, _ PrepareUploadPlatformImageInput) (*mcp.CallToolResult, PlatformImageUploadOutput, error) {
		out, err := s.prepare(ctx)
		return nil, out, err
	})
}

var _ operationWriter = (*platformImageWriter)(nil)
