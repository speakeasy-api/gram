// Package orgencryption encrypts organization data under keys scoped to the
// organization, instead of the single application key in package encryption.
//
// It uses envelope encryption. Each organization has one key encryption key
// (KEK): a symmetric KMS key that Speakeasy mints on first use, or that the
// customer supplies and controls. The KEK wraps one data encryption key (DEK)
// per registered use case. DEKs encrypt application values locally with
// AES-256-GCM. On the data path KMS only wraps and unwraps DEKs, and a
// plaintext DEK is cached in memory for DataKeyCacheTTL; key management also
// calls KMS to verify a KEK before using it.
//
// Every ciphertext binds the organization, the use case and the column it is
// stored in as associated data, so a value cannot be moved to another
// organization, consumer or column and still decrypt.
//
// Switching an organization to another KEK re-wraps its DEKs and rewrites no
// application rows. During a switch each DEK is wrapped by both KEKs, so the
// previous KEK keeps everything readable until the switch completes or rolls
// back.
//
// Rotating a DEK retires the old one to decrypt-only; there is no rewrite job.
// Rows sealed with the old DEK re-encrypt under the new one when their owner
// next writes them, so a use case whose rows are rarely rewritten keeps its
// decrypt-only DEKs indefinitely.
//
// The package fails closed, and nothing falls back to the application key. An
// organization with no configuration cannot encrypt or decrypt at all. A
// suspension stops a process within configCacheTTL of it being recorded. An
// unreachable or revoked KEK stops a process once its cached DEK expires,
// within DataKeyCacheTTL.
package orgencryption
