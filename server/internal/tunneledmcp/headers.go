package tunneledmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// Headers handles encryption and decryption of tunneled MCP server header
// values. All header value access goes through this wrapper.
type Headers struct {
	logger *slog.Logger
	db     repo.DBTX
	enc    *encryption.Client
}

func NewHeaders(logger *slog.Logger, db repo.DBTX, enc *encryption.Client) *Headers {
	return &Headers{
		logger: logger.With(attr.SlogComponent("tunneled_mcp_server_headers")),
		db:     db,
		enc:    enc,
	}
}

// ConfiguredHeaders reads a tunnel's headers, decrypted, in the form the MCP
// proxy applies. projectID is the project of the MCP server being served; a
// tunnel outside it yields no headers. Management callers must use
// ListServerHeaders.
func (h *Headers) ConfiguredHeaders(ctx context.Context, projectID uuid.UUID, tunneledMcpServerID uuid.UUID) ([]proxy.ConfiguredHeader, error) {
	rows, err := repo.New(h.db).ListHeadersByServerID(ctx, repo.ListHeadersByServerIDParams{
		TunneledMcpServerID: tunneledMcpServerID,
		ProjectID:           projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list tunneled mcp server headers: %w", err)
	}

	configured := make([]proxy.ConfiguredHeader, 0, len(rows))
	for _, row := range rows {
		revealed, err := h.revealHeader(row, false)
		if err != nil {
			return nil, err
		}
		configured = append(configured, proxy.ConfiguredHeader{
			IsRequired:             revealed.IsRequired,
			Name:                   revealed.Name,
			StaticValue:            revealed.Value.String,
			ValueFromRequestHeader: revealed.ValueFromRequestHeader.String,
		})
	}

	return configured, nil
}

// ListServerHeaders reads a tunnel's headers with secret values redacted,
// scoped to the given project.
func (h *Headers) ListServerHeaders(ctx context.Context, tunneledMcpServerID uuid.UUID, projectID uuid.UUID) ([]repo.TunneledMcpServerHeader, error) {
	rows, err := repo.New(h.db).ListServerHeaders(ctx, repo.ListServerHeadersParams{
		TunneledMcpServerID: tunneledMcpServerID,
		ProjectID:           projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list server headers: %w", err)
	}

	result := make([]repo.TunneledMcpServerHeader, len(rows))
	for i, row := range rows {
		result[i] = redactHeader(row)
	}

	return result, nil
}

// GetServerHeader reads one header by id with its secret value redacted,
// scoped to the given project.
func (h *Headers) GetServerHeader(ctx context.Context, id uuid.UUID, projectID uuid.UUID) (repo.TunneledMcpServerHeader, error) {
	row, err := repo.New(h.db).GetServerHeader(ctx, repo.GetServerHeaderParams{ID: id, ProjectID: projectID})
	if err != nil {
		return repo.TunneledMcpServerHeader{}, fmt.Errorf("get server header: %w", err)
	}

	return redactHeader(row), nil
}

// CreateServerHeader encrypts a secret value and inserts the header, returning
// it redacted.
func (h *Headers) CreateServerHeader(ctx context.Context, params repo.CreateServerHeaderParams) (repo.TunneledMcpServerHeader, error) {
	value, err := h.encryptValue(params.IsSecret, params.Value)
	if err != nil {
		return repo.TunneledMcpServerHeader{}, fmt.Errorf("encrypt header value: %w", err)
	}
	params.Value = value

	row, err := repo.New(h.db).CreateServerHeader(ctx, params)
	if err != nil {
		return repo.TunneledMcpServerHeader{}, fmt.Errorf("create server header: %w", err)
	}

	return redactHeader(row), nil
}

// UpdateServerHeader replaces a header's mutable fields, returning it
// redacted. When params.SetValue is false the stored value is left in place
// and nothing is encrypted; that is how an existing secret is preserved.
func (h *Headers) UpdateServerHeader(ctx context.Context, params repo.UpdateServerHeaderParams) (repo.TunneledMcpServerHeader, error) {
	// Only a secret's stored ciphertext may be kept; keeping it on a plain
	// row would send ciphertext upstream as the header's value.
	if !params.SetValue && !params.IsSecret {
		return repo.TunneledMcpServerHeader{}, errors.New("update server header: only a secret header can keep its stored value")
	}
	if params.SetValue {
		value, err := h.encryptValue(params.IsSecret, params.Value)
		if err != nil {
			return repo.TunneledMcpServerHeader{}, fmt.Errorf("encrypt header value: %w", err)
		}
		params.Value = value
	} else {
		params.Value = pgtype.Text{String: "", Valid: false}
	}

	row, err := repo.New(h.db).UpdateServerHeader(ctx, params)
	if err != nil {
		return repo.TunneledMcpServerHeader{}, fmt.Errorf("update server header: %w", err)
	}

	return redactHeader(row), nil
}

// redactHeader replaces a secret header's stored value with a placeholder
// without decrypting it, for rows that are only being reported, so one
// undecryptable row cannot fail a management read.
func redactHeader(header repo.TunneledMcpServerHeader) repo.TunneledMcpServerHeader {
	if header.IsSecret && header.Value.Valid {
		header.Value = pgtype.Text{String: redactValue(header.Value.String), Valid: true}
	}
	return header
}

func (h *Headers) revealHeader(header repo.TunneledMcpServerHeader, redacted bool) (repo.TunneledMcpServerHeader, error) {
	if !header.IsSecret || !header.Value.Valid || header.Value.String == "" {
		return header, nil
	}

	decrypted, err := h.enc.Decrypt(header.Value.String)
	if err != nil {
		return repo.TunneledMcpServerHeader{}, fmt.Errorf("decrypt header %s: %w", header.Name, err)
	}

	if redacted {
		decrypted = redactValue(decrypted)
	}
	header.Value = pgtype.Text{String: decrypted, Valid: true}

	return header, nil
}

func (h *Headers) encryptValue(isSecret bool, value pgtype.Text) (pgtype.Text, error) {
	if !isSecret || !value.Valid || value.String == "" {
		return value, nil
	}

	encrypted, err := h.enc.Encrypt([]byte(value.String))
	if err != nil {
		return value, fmt.Errorf("encrypt value: %w", err)
	}

	return pgtype.Text{String: encrypted, Valid: true}, nil
}

func redactValue(val string) string {
	if val == "" {
		return "<EMPTY>"
	}

	return "***"
}
