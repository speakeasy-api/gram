package orgencryption

import "context"

// CopyColumns names the pair of columns that hold one value's two copies,
// and the use case whose data key seals the organization-key copy.
type CopyColumns struct {
	// UseCase is the use case whose data key seals the organization-key copy.
	UseCase UseCase

	// PlatformColumn is the "table.column" of the platform-key copy. It keys
	// organization_encryption_unreadable_values.source_column.
	PlatformColumn string

	// OrgColumn is the "table.column" of the organization-key copy. It is
	// bound into every organization-key ciphertext as associated data, so it
	// must never change once values are sealed.
	OrgColumn string
}

// WaitingValue is a stored value with a platform-key copy and no
// organization-key copy.
type WaitingValue struct {
	// RowID is the source row's single-column primary key formatted exactly
	// as the feature's queries compare it with
	// organization_encryption_unreadable_values.row_id, for example id::text
	// for a uuid key, which matches uuid.UUID.String().
	RowID string

	// Platform is the platform-key ciphertext as stored. For a text column it
	// is the text; for a bytea column it is the raw bytes.
	Platform string
}

// SealedValue is a stored value with both copies, for verification.
type SealedValue struct {
	// RowID is formatted as in WaitingValue.
	RowID string

	// Platform is the platform-key ciphertext as stored.
	Platform string

	// Org is the organization-key ciphertext as stored.
	Org string
}

// CopyCounts are a copy store's counts for one organization.
type CopyCounts struct {
	// Waiting counts values with a platform-key copy and no
	// organization-key copy, excluding values recorded as unreadable whose
	// recorded hash still matches the stored platform-key ciphertext.
	Waiting int64

	// Unreadable counts values with a platform-key copy and no
	// organization-key copy that are recorded as unreadable with a hash that
	// still matches the stored platform-key ciphertext: for a text column,
	// sha256(convert_to(<platform column>, 'UTF8')); for a bytea column,
	// sha256(<platform column>). A record left behind for a value that has
	// since gained an organization-key copy therefore no longer counts.
	Unreadable int64

	// Unattributed counts values that may belong to the organization but are
	// stored without an organization the feature can attribute them to yet,
	// such as rows whose organization_id has not been backfilled. Finalize
	// refuses while it is above zero, because the organization's clear would
	// never reach them.
	Unattributed int64

	// PlatformCopies counts values that still hold a platform-key copy,
	// with or without an organization-key copy.
	PlatformCopies int64
}

// CopyStore is how the helper reaches the two stored copies of one value, a
// pair of columns in a feature's table. A feature implements it with its own
// sqlc queries, one per method. Every query covers every row that holds
// ciphertext, including soft-deleted rows. A write changes only the copy
// column it names, never updated_at or any other column of the row.
//
// The list methods page by the source row's own primary key, not by the text
// of RowID: they order by the key and return rows whose key is greater than
// after, parsed back into the key's type. after is "" for the first page and
// otherwise the RowID of the last value of the previous page.
type CopyStore interface {
	// Columns names the value's pair of columns.
	Columns() CopyColumns

	// ListWaiting returns the next page of up to limit waiting values of the
	// organization, including values recorded as unreadable so they are
	// tried again.
	ListWaiting(ctx context.Context, organizationID string, after string, limit int32) ([]WaitingValue, error)

	// SealWaiting stores orgCiphertext as the value's organization-key copy.
	// It lands only while the row still holds value.Platform as its
	// platform-key copy, holds no organization-key copy, and the transition
	// is still at generation. It reports whether it landed.
	SealWaiting(ctx context.Context, organizationID string, generation int64, value WaitingValue, orgCiphertext string) (bool, error)

	// ListSealed returns the next page of up to limit values of the
	// organization that hold both copies.
	ListSealed(ctx context.Context, organizationID string, after string, limit int32) ([]SealedValue, error)

	// Count returns the column's counts for the organization.
	Count(ctx context.Context, organizationID string) (CopyCounts, error)

	// ClearPlatformCopies sets the platform-key copy to NULL on up to limit
	// rows of the organization that still hold one, fenced on generation,
	// and returns how many it cleared.
	ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (int64, error)
}
