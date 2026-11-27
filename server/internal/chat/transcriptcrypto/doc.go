// Package transcriptcrypto provides the versioned, row-bound message payload
// codec used by opt-in encrypted transcript storage. It never falls back to
// plaintext. Shadow copies do not remove legacy plaintext or enable
// encrypted-only reads; those require all writers and readers to migrate.
package transcriptcrypto
