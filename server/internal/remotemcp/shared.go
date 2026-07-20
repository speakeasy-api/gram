package remotemcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// Headers handles encryption and decryption of header values.
// All header access should go through this wrapper.
type Headers struct {
	logger *slog.Logger
	db     repo.DBTX
	enc    *encryption.Client
}

func NewHeaders(logger *slog.Logger, db repo.DBTX, enc *encryption.Client) *Headers {
	return &Headers{
		logger: logger.With(attr.SlogComponent("remote_mcp_server_headers")),
		db:     db,
		enc:    enc,
	}
}

// ListHeaders reads a server's headers without scoping to a project. The MCP
// proxy calls this after it has already resolved the server row, and needs
// decrypted values to inject into outbound requests. Management callers must
// use ListServerHeaders instead so a project boundary is enforced.
func (h *Headers) ListHeaders(ctx context.Context, serverID uuid.UUID, redacted bool) ([]repo.RemoteMcpServerHeader, error) {
	headers, err := repo.New(h.db).ListHeadersByServerID(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("list headers: %w", err)
	}

	return h.revealHeaders(headers, redacted)
}

// ListServerHeaders reads a server's headers, scoped to the given project.
// Returns an empty slice when the server does not exist in that project.
func (h *Headers) ListServerHeaders(ctx context.Context, serverID uuid.UUID, projectID uuid.UUID, redacted bool) ([]repo.RemoteMcpServerHeader, error) {
	headers, err := repo.New(h.db).ListServerHeaders(ctx, repo.ListServerHeadersParams{
		RemoteMcpServerID: serverID,
		ProjectID:         projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list server headers: %w", err)
	}

	return h.revealHeaders(headers, redacted)
}

// GetServerHeader reads one header by id, scoped to the given project.
func (h *Headers) GetServerHeader(ctx context.Context, id uuid.UUID, projectID uuid.UUID, redacted bool) (repo.RemoteMcpServerHeader, error) {
	header, err := repo.New(h.db).GetServerHeader(ctx, repo.GetServerHeaderParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("get server header: %w", err)
	}

	return h.revealHeader(header, redacted)
}

func (h *Headers) CreateServerHeader(ctx context.Context, params repo.CreateServerHeaderParams) (repo.RemoteMcpServerHeader, error) {
	value, err := h.encryptValue(params.IsSecret, params.Value)
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("encrypt header %s: %w", params.Name, err)
	}
	params.Value = value

	header, err := repo.New(h.db).CreateServerHeader(ctx, params)
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("create server header %s: %w", params.Name, err)
	}

	return h.revealHeader(header, true)
}

// UpdateServerHeader replaces a header's mutable fields. When params.SetValue is
// false the value column is left untouched by the query, so no plaintext is
// supplied and nothing is encrypted — this is the path that preserves an
// existing secret's stored value.
func (h *Headers) UpdateServerHeader(ctx context.Context, params repo.UpdateServerHeaderParams) (repo.RemoteMcpServerHeader, error) {
	if params.SetValue {
		value, err := h.encryptValue(params.IsSecret, params.Value)
		if err != nil {
			return repo.RemoteMcpServerHeader{}, fmt.Errorf("encrypt header %s: %w", params.Name, err)
		}
		params.Value = value
	} else {
		params.Value = pgtype.Text{String: "", Valid: false}
	}

	header, err := repo.New(h.db).UpdateServerHeader(ctx, params)
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("update server header %s: %w", params.Name, err)
	}

	return h.revealHeader(header, true)
}

// revealHeaders decrypts each secret header's value, redacting it when the
// caller is a management read rather than the proxy.
func (h *Headers) revealHeaders(headers []repo.RemoteMcpServerHeader, redacted bool) ([]repo.RemoteMcpServerHeader, error) {
	result := make([]repo.RemoteMcpServerHeader, len(headers))
	for i, header := range headers {
		revealed, err := h.revealHeader(header, redacted)
		if err != nil {
			return nil, err
		}

		result[i] = revealed
	}

	return result, nil
}

func (h *Headers) revealHeader(header repo.RemoteMcpServerHeader, redacted bool) (repo.RemoteMcpServerHeader, error) {
	if !header.IsSecret || !header.Value.Valid {
		return header, nil
	}

	decrypted, err := h.enc.Decrypt(header.Value.String)
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("decrypt header %s: %w", header.Name, err)
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
