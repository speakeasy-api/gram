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
	repo   *repo.Queries
	enc    *encryption.Client
}

func NewHeaders(logger *slog.Logger, db repo.DBTX, enc *encryption.Client) *Headers {
	return &Headers{
		logger: logger.With(attr.SlogComponent("remote_mcp_server_headers")),
		repo:   repo.New(db),
		enc:    enc,
	}
}

func (h *Headers) ListHeaders(ctx context.Context, serverID uuid.UUID, redacted bool) ([]repo.RemoteMcpServerHeader, error) {
	headers, err := h.repo.ListHeadersByServerID(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("list headers: %w", err)
	}

	result := make([]repo.RemoteMcpServerHeader, len(headers))
	for i, header := range headers {
		result[i] = header

		if !header.IsSecret || !header.Value.Valid {
			continue
		}

		decrypted, err := h.enc.Decrypt(header.Value.String)
		if err != nil {
			return nil, fmt.Errorf("decrypt header %s: %w", header.Name, err)
		}

		if redacted {
			decrypted = redactValue(decrypted)
		}

		result[i].Value = pgtype.Text{String: decrypted, Valid: true}
	}

	return result, nil
}

// ListHeadersByServerIDs fetches headers for multiple servers in a single query
// and returns them grouped by server ID.
func (h *Headers) ListHeadersByServerIDs(ctx context.Context, serverIDs []uuid.UUID, redacted bool) (map[uuid.UUID][]repo.RemoteMcpServerHeader, error) {
	if len(serverIDs) == 0 {
		return map[uuid.UUID][]repo.RemoteMcpServerHeader{}, nil
	}

	headers, err := h.repo.ListHeadersByServerIDs(ctx, serverIDs)
	if err != nil {
		return nil, fmt.Errorf("list headers by server ids: %w", err)
	}

	result := make(map[uuid.UUID][]repo.RemoteMcpServerHeader, len(serverIDs))
	for _, header := range headers {
		if header.IsSecret && header.Value.Valid {
			decrypted, err := h.enc.Decrypt(header.Value.String)
			if err != nil {
				return nil, fmt.Errorf("decrypt header %s: %w", header.Name, err)
			}

			if redacted {
				decrypted = redactValue(decrypted)
			}

			header.Value = pgtype.Text{String: decrypted, Valid: true}
		}

		result[header.RemoteMcpServerID] = append(result[header.RemoteMcpServerID], header)
	}

	return result, nil
}

func (h *Headers) CreateHeader(ctx context.Context, params repo.CreateHeaderParams) (repo.RemoteMcpServerHeader, error) {
	originalValue := params.Value.String

	params, err := h.encryptHeaderParams(params.IsSecret, params)
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("encrypt header %s: %w", params.Name, err)
	}

	header, err := h.repo.CreateHeader(ctx, params)
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("create header %s: %w", params.Name, err)
	}

	if header.IsSecret && header.Value.Valid {
		header.Value = pgtype.Text{String: redactValue(originalValue), Valid: true}
	}

	return header, nil
}

func (h *Headers) UpsertHeader(ctx context.Context, params repo.UpsertHeaderParams) (repo.RemoteMcpServerHeader, error) {
	originalValue := params.Value.String

	createParams := repo.CreateHeaderParams(params)

	createParams, err := h.encryptHeaderParams(params.IsSecret, createParams)
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("encrypt header %s: %w", params.Name, err)
	}

	header, err := h.repo.UpsertHeader(ctx, repo.UpsertHeaderParams(createParams))
	if err != nil {
		return repo.RemoteMcpServerHeader{}, fmt.Errorf("upsert header %s: %w", params.Name, err)
	}

	if header.IsSecret && header.Value.Valid {
		header.Value = pgtype.Text{String: redactValue(originalValue), Valid: true}
	}

	return header, nil
}

func (h *Headers) DeleteHeader(ctx context.Context, serverID uuid.UUID, name string) error {
	err := h.repo.DeleteHeader(ctx, repo.DeleteHeaderParams{
		RemoteMcpServerID: serverID,
		Name:              name,
	})
	if err != nil {
		return fmt.Errorf("delete header %s: %w", name, err)
	}

	return nil
}

func (h *Headers) encryptHeaderParams(isSecret bool, params repo.CreateHeaderParams) (repo.CreateHeaderParams, error) {
	if !isSecret || !params.Value.Valid || params.Value.String == "" {
		return params, nil
	}

	encrypted, err := h.enc.Encrypt([]byte(params.Value.String))
	if err != nil {
		return params, fmt.Errorf("encrypt value: %w", err)
	}

	params.Value = pgtype.Text{String: encrypted, Valid: true}

	return params, nil
}

func redactValue(val string) string {
	if val == "" {
		return "<EMPTY>"
	}

	return "***"
}
