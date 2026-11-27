package transcriptstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

// ErrIncompatibleSchema reports a database without the tables, columns and
// conflict targets the store needs.
var ErrIncompatibleSchema = errors.New("chat transcript schema is unavailable or incompatible")

// CheckSchema verifies, from the catalogs alone, that the database has what the
// store reads and writes. Run it before enabling transcripts: a deployment
// whose migrations lag the code fails here instead of on its first write.
func CheckSchema(ctx context.Context, db repo.DBTX) error {
	compatible, err := repo.New(db).SchemaCompatible(ctx)
	if err != nil {
		return fmt.Errorf("check chat transcript schema: %w", err)
	}
	if !compatible {
		return ErrIncompatibleSchema
	}
	return nil
}
