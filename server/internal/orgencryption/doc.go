// Package orgencryption encrypts organization data under keys scoped to the
// organization, instead of the single application key in package encryption.
//
// It uses envelope encryption. Each organization has one key encryption key
// (KEK): a symmetric KMS key that Speakeasy mints on first use, or that the
// customer supplies and controls. The KEK wraps one data encryption key (DEK)
// per registered use case. DEKs encrypt application values locally with
// AES-256-GCM, so KMS is only called to wrap or unwrap a DEK, and a plaintext
// DEK is cached in memory for DataKeyCacheTTL.
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
// The package fails closed. An organization with no configuration, a
// suspended configuration, or an unreachable KEK cannot encrypt or decrypt,
// and nothing falls back to the application key.
package orgencryption
