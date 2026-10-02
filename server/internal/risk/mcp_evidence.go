package risk

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const (
	// mcpFindingEvidenceRetention matches the ClickHouse risk_findings TTL.
	mcpFindingEvidenceRetention = 90 * 24 * time.Hour
)

// ErrMCPFindingEvidenceNotStored means evidence is unavailable or has expired.
var ErrMCPFindingEvidenceNotStored = errors.New("MCP finding evidence not stored")

// MCPFindingEvidence is one plaintext match before encrypted persistence.
type MCPFindingEvidence struct {
	// ID is the finding ID written to ClickHouse.
	ID uuid.UUID

	// Match is the raw scanner match.
	Match string
}

// MCPFindingEvidenceBatch carries findings from one policy evaluation.
type MCPFindingEvidenceBatch struct {
	// OrganizationID owns every finding in the batch.
	OrganizationID string

	// ProjectID owns every finding in the batch.
	ProjectID uuid.UUID

	// CreatedAt starts the evidence retention window.
	CreatedAt time.Time

	// Findings are encrypted and stored atomically.
	Findings []MCPFindingEvidence
}

// MCPFindingEvidenceStore encrypts MCP matches before writing them to Postgres.
type MCPFindingEvidenceStore struct {
	db  *pgxpool.Pool
	enc *encryption.Client
}

// NewMCPFindingEvidenceStore creates an encrypted evidence store.
func NewMCPFindingEvidenceStore(db *pgxpool.Pool, enc *encryption.Client) *MCPFindingEvidenceStore {
	return &MCPFindingEvidenceStore{db: db, enc: enc}
}

// Store encrypts and persists an evidence batch.
func (s *MCPFindingEvidenceStore) Store(ctx context.Context, batch MCPFindingEvidenceBatch) error {
	if len(batch.Findings) == 0 {
		return nil
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin MCP finding evidence transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)

	expiresAt := batch.CreatedAt.Add(mcpFindingEvidenceRetention)
	for _, finding := range batch.Findings {
		ciphertext, err := s.enc.Encrypt([]byte(finding.Match))
		if err != nil {
			return fmt.Errorf("encrypt MCP finding evidence: %w", err)
		}
		if err := queries.UpsertMCPFindingEvidence(ctx, repo.UpsertMCPFindingEvidenceParams{
			FindingID:      finding.ID,
			OrganizationID: batch.OrganizationID,
			ProjectID:      batch.ProjectID,
			MatchEncrypted: ciphertext,
			CreatedAt:      conv.ToPGTimestamptz(batch.CreatedAt),
			ExpiresAt:      conv.ToPGTimestamptz(expiresAt),
		}); err != nil {
			return fmt.Errorf("store MCP finding evidence: %w", err)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit MCP finding evidence: %w", err)
	}
	return nil
}

// Reveal returns decrypted evidence only while its retention window is active.
func (s *MCPFindingEvidenceStore) Reveal(ctx context.Context, organizationID string, projectID, findingID uuid.UUID, now time.Time) (string, error) {
	ciphertext, err := repo.New(s.db).GetMCPFindingEvidence(ctx, repo.GetMCPFindingEvidenceParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		FindingID:      findingID,
		Now:            conv.ToPGTimestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMCPFindingEvidenceNotStored
	}
	if err != nil {
		return "", fmt.Errorf("load MCP finding evidence: %w", err)
	}

	match, err := s.enc.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt MCP finding evidence: %w", err)
	}
	return match, nil
}
