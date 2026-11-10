package risk

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

const (
	// ResultMatchEncryptedColumn is the column a sealed risk_results match is
	// stored in, bound into its ciphertext as associated data.
	ResultMatchEncryptedColumn = "risk_results.match_encrypted"

	// ResultSpansEncryptedColumn is the column sealed risk_results spans are
	// stored in, bound into their ciphertext as associated data.
	ResultSpansEncryptedColumn = "risk_results.spans_encrypted"
)

// ErrResultEvidenceUnavailable means a row holds sealed evidence but this
// process has no organization encryption to open it with.
var ErrResultEvidenceUnavailable = errors.New("risk result evidence is encrypted and organization encryption is not configured")

// ResultEvidence is the matched text of one risk_results row in the columns it
// is stored in. An organization that has not been cut over to encryption keeps
// the plaintext columns; one that has keeps only the encrypted columns.
type ResultEvidence struct {
	// Match is risk_results.match.
	Match pgtype.Text

	// Spans is risk_results.spans, a JSON array.
	Spans []byte

	// MatchEncrypted is risk_results.match_encrypted.
	MatchEncrypted pgtype.Text

	// SpansEncrypted is risk_results.spans_encrypted.
	SpansEncrypted pgtype.Text
}

// ResultEvidenceCipher seals risk_results match and spans under the
// organization's risk-evidence data encryption key, and opens them again.
//
// An organization is cut over once it has a DEK for
// orgencryption.UseCaseRiskEvidence; from then on Seal never returns plaintext
// for it. The DEK is provisioned by the risk results encryption backfill, not
// on the write path.
type ResultEvidenceCipher struct {
	enc *orgencryption.Service
}

// NewResultEvidenceCipher builds a cipher over enc. A nil enc means
// organization encryption is off in this process: Seal keeps plaintext and
// opening sealed evidence fails with ErrResultEvidenceUnavailable.
func NewResultEvidenceCipher(enc *orgencryption.Service) *ResultEvidenceCipher {
	return &ResultEvidenceCipher{enc: enc}
}

// Seal returns the columns to store match and spans in. They stay plaintext
// when organization encryption is off or the organization has no
// risk-evidence DEK yet. Any other failure, such as a suspended or revoked
// key, returns the zero ResultEvidence and the error, so the caller stores the
// row without its evidence rather than in plaintext.
func (c *ResultEvidenceCipher) Seal(ctx context.Context, organizationID string, match pgtype.Text, spans []byte) (ResultEvidence, error) {
	plaintext := ResultEvidence{
		Match:          match,
		Spans:          spans,
		MatchEncrypted: pgtype.Text{String: "", Valid: false},
		SpansEncrypted: pgtype.Text{String: "", Valid: false},
	}
	if !match.Valid && spans == nil {
		return plaintext, nil
	}
	if c == nil || c.enc == nil {
		return plaintext, nil
	}

	sealed, err := c.seal(ctx, organizationID, match, spans)
	switch {
	case errors.Is(err, orgencryption.ErrNotProvisioned):
		return plaintext, nil
	case err != nil:
		return ResultEvidence{}, err
	}

	return sealed, nil
}

// SealEncrypted is Seal without the plaintext fallback: it fails when the
// organization has no risk-evidence DEK.
func (c *ResultEvidenceCipher) SealEncrypted(ctx context.Context, organizationID string, match pgtype.Text, spans []byte) (ResultEvidence, error) {
	if c == nil || c.enc == nil {
		return ResultEvidence{}, ErrResultEvidenceUnavailable
	}

	return c.seal(ctx, organizationID, match, spans)
}

func (c *ResultEvidenceCipher) seal(ctx context.Context, organizationID string, match pgtype.Text, spans []byte) (ResultEvidence, error) {
	enc, err := c.enc.For(organizationID, orgencryption.UseCaseRiskEvidence)
	if err != nil {
		return ResultEvidence{}, fmt.Errorf("risk evidence encryptor: %w", err)
	}

	sealed := ResultEvidence{
		Match:          pgtype.Text{String: "", Valid: false},
		Spans:          nil,
		MatchEncrypted: pgtype.Text{String: "", Valid: false},
		SpansEncrypted: pgtype.Text{String: "", Valid: false},
	}
	if match.Valid {
		ciphertext, err := enc.Encrypt(ctx, ResultMatchEncryptedColumn, []byte(match.String))
		if err != nil {
			return ResultEvidence{}, fmt.Errorf("encrypt risk result match: %w", err)
		}
		sealed.MatchEncrypted = pgtype.Text{String: ciphertext, Valid: true}
	}
	if spans != nil {
		ciphertext, err := enc.Encrypt(ctx, ResultSpansEncryptedColumn, spans)
		if err != nil {
			return ResultEvidence{}, fmt.Errorf("encrypt risk result spans: %w", err)
		}
		sealed.SpansEncrypted = pgtype.Text{String: ciphertext, Valid: true}
	}

	return sealed, nil
}

// OpenMatch returns a row's match, decrypting match_encrypted when it is set
// and returning the plaintext column otherwise.
func (c *ResultEvidenceCipher) OpenMatch(ctx context.Context, organizationID string, match, matchEncrypted pgtype.Text) (pgtype.Text, error) {
	if !matchEncrypted.Valid {
		return match, nil
	}

	plaintext, err := c.open(ctx, organizationID, ResultMatchEncryptedColumn, matchEncrypted.String)
	if err != nil {
		return pgtype.Text{String: "", Valid: false}, fmt.Errorf("decrypt risk result match: %w", err)
	}

	return pgtype.Text{String: string(plaintext), Valid: true}, nil
}

// OpenSpans returns a row's spans, decrypting spans_encrypted when it is set
// and returning the plaintext column otherwise.
func (c *ResultEvidenceCipher) OpenSpans(ctx context.Context, organizationID string, spans []byte, spansEncrypted pgtype.Text) ([]byte, error) {
	if !spansEncrypted.Valid {
		return spans, nil
	}

	plaintext, err := c.open(ctx, organizationID, ResultSpansEncryptedColumn, spansEncrypted.String)
	if err != nil {
		return nil, fmt.Errorf("decrypt risk result spans: %w", err)
	}

	return plaintext, nil
}

func (c *ResultEvidenceCipher) open(ctx context.Context, organizationID string, column string, ciphertext string) ([]byte, error) {
	if c == nil || c.enc == nil {
		return nil, ErrResultEvidenceUnavailable
	}

	enc, err := c.enc.For(organizationID, orgencryption.UseCaseRiskEvidence)
	if err != nil {
		return nil, fmt.Errorf("risk evidence encryptor: %w", err)
	}

	plaintext, err := enc.Decrypt(ctx, column, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", column, err)
	}

	return plaintext, nil
}
