package platformmcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// A result card shows an MCP server's logo inline as a data: URI, because
// MCP Apps hosts block remote images and the card view's image rule is fixed
// for every result. The logo is the one stored for the MCP server when it was
// added; a card never fetches from the network.

// maxCardLogoBytes bounds a logo inlined in a card. Favicons and app icons
// are a few KiB; 64 KiB keeps every card result small.
const maxCardLogoBytes = 64 << 10 // 64 KiB

var (
	// errCardLogoType rejects a logo that is not an allowed raster image.
	errCardLogoType = errors.New("card logo is not an allowed image type")

	// errCardLogoSize rejects a logo that is empty or over maxCardLogoBytes.
	errCardLogoSize = errors.New("card logo size is out of bounds")
)

// cardLogoDataURI turns stored logo bytes into a data: URI the card template
// trusts. The bytes must be within maxCardLogoBytes, and both the stored type
// and the type sniffed from the bytes must be the same raster image type; SVG
// is never allowed.
func cardLogoDataURI(contentType string, body []byte) (template.URL, error) {
	if len(body) == 0 || len(body) > maxCardLogoBytes {
		return "", errCardLogoSize
	}
	if !slices.Contains(assets.PlatformImageTypes, contentType) || http.DetectContentType(body) != contentType {
		return "", errCardLogoType
	}
	//nolint:gosec // G203: the URI is built here from validated raster image bytes, never from caller text.
	return template.URL("data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(body)), nil
}

// cardLogos reads the stored logo of a registration's MCP server.
type cardLogos struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	storage assets.BlobStore
}

// registrationLogo returns the card logo for a registration, or "" when it
// has none or the stored one is not safe to inline. A card is decoration, so
// failures are logged and never fail the tool result.
func (l *cardLogos) registrationLogo(ctx context.Context, organizationID string, projectID uuid.UUID, registrationID string) template.URL {
	if l == nil {
		return ""
	}
	parsedID, err := uuid.Parse(registrationID)
	if err != nil {
		return ""
	}
	logo, err := l.read(ctx, organizationID, projectID, parsedID)
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, errCardLogoSize), errors.Is(err, errCardLogoType):
		// No logo, or one that can never be inlined: show the first letter.
		return ""
	case err != nil:
		l.logger.WarnContext(ctx, "read platform mcp card logo", attr.SlogError(err), attr.SlogProjectID(projectID.String()))
		return ""
	}
	return logo
}

func (l *cardLogos) read(ctx context.Context, organizationID string, projectID, registrationID uuid.UUID) (template.URL, error) {
	row, err := platformrepo.New(l.db).GetPlatformMCPRegistrationLogo(ctx, platformrepo.GetPlatformMCPRegistrationLogoParams{
		RegistrationID: registrationID,
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		return "", fmt.Errorf("load card logo asset: %w", err)
	}
	// Skip the storage read for a logo too large to inline.
	if row.ContentLength <= 0 || row.ContentLength > maxCardLogoBytes {
		return "", errCardLogoSize
	}
	assetURL, err := url.Parse(row.Url)
	if err != nil {
		return "", fmt.Errorf("parse card logo asset url: %w", err)
	}

	body, err := l.storage.Read(ctx, assetURL)
	if err != nil {
		return "", fmt.Errorf("read card logo asset: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return body.Close() })

	// Read one byte past the limit so an object larger than its recorded
	// length is still rejected.
	data, err := io.ReadAll(io.LimitReader(body, maxCardLogoBytes+1))
	if err != nil {
		return "", fmt.Errorf("read card logo bytes: %w", err)
	}
	return cardLogoDataURI(row.ContentType, data)
}
