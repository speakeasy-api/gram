package delegation

import (
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/upstreamtokens"
)

// secret is one sensitive value of a delegation credential: either the copies
// the row holds, kept as they are, or a value set since the row was read. The
// zero value is an erased value.
//
// A kept value is written back by leaving its stored copies untouched, never
// by copying ciphertext: a value sealed under one transition state is not
// written again under another.
type secret struct {
	// plain is the value set since the row was read; empty erases it.
	plain string

	// stored is the row's copies as read, kept unless the value is replaced.
	stored *orgencryption.Copies
}

// String keeps a secret out of logs and formatted errors.
func (v secret) String() string { return "[delegation secret]" }

// GoString keeps a secret out of %#v.
func (v secret) GoString() string { return v.String() }

// MarshalJSON keeps a secret out of encoded output.
func (v secret) MarshalJSON() ([]byte, error) { return []byte(`""`), nil }

// erasedSecret is a value erased since the row was read.
func erasedSecret() secret {
	return secret{plain: "", stored: nil}
}

// newSecret is a value set to plain, which replaces the stored copies when
// written. An empty plain erases them.
func newSecret(plain string) secret {
	return secret{plain: plain, stored: nil}
}

// storedSecret is a value as the row stores it.
func storedSecret(platform, org pgtype.Text) secret {
	copies := upstreamtokens.Stored(platform, org)
	return secret{plain: "", stored: &copies}
}

// present reports whether the value is set.
func (v secret) present() bool {
	if v.stored != nil {
		return upstreamtokens.Has(*v.stored)
	}
	return v.plain != ""
}

// write is how a store writes the value: nil keeps the stored copies,
// otherwise the plaintext to seal, empty to erase both copies.
func (v secret) write() *string {
	if v.stored != nil {
		return nil
	}
	plain := v.plain
	return &plain
}
