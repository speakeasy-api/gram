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
// The package fails closed: a failing organization key never falls back to the
// application key, including during the staged transitions below. An
// organization with no configuration cannot encrypt or decrypt at all. A
// suspension stops a process within configCacheTTL of it being recorded. An
// unreachable or revoked KEK stops a process once its cached DEK expires,
// within DataKeyCacheTTL.
//
// Values that already exist under the application (platform) key move to
// organization keys one use case at a time, through a recorded transition
// state per organization: platform_key, then dual_write, then org_kms. Cipher
// decides from that state which copies application code writes and reads; in
// dual_write a failing organization key fails the operation rather than
// falling back. Runner backfills and verifies the organization-key copies
// through a CopyStore each feature implements, reading platform-key copies
// only in dual_write. Transitions finalizes and then clears the platform-key
// copies, and once a use case reaches org_kms nothing reads a platform-key
// copy.
//
// # Fencing
//
// Transition state is cached per process, and a writer can hold a Decision
// across a provider call, so every feature statement that writes or clears
// either copy of a value fences itself on the generation it decided under. It
// adds this predicate, comparing against the row's own organization column
// rather than a caller parameter so a row of another organization never
// passes:
//
//	AND COALESCE((
//	  SELECT t.generation FROM organization_encryption_transitions AS t
//	  WHERE t.organization_id = <table>.organization_id AND t.use_case = '<use case>'
//	), 0) = @decided_generation
//
// The COALESCE is required: an organization with no transition row is at
// generation 0, and without it every write of a platform_key organization
// would be fenced. orgencryptiontest.RequireFencedWrites checks a feature's
// queries.sql for this shape.
//
// An INSERT fences through INSERT ... SELECT ... WHERE or ON CONFLICT ... DO
// UPDATE ... WHERE. A statement that matches no row because of the fence
// returns ErrFenced to its caller, which decides again with Cipher.Redecide,
// seals the same plaintext again, and retries the write. It never repeats a
// call to a provider: a refresh may already have rotated the refresh token,
// and the only copy of the new one is the plaintext the caller holds.
//
// The subquery reads the transition state as of the statement's start, so a
// statement that started before finalize committed can still land after a
// clearing batch. Transitions.ClearPlatformCopies counts again after a settle
// delay to catch such writes.
package orgencryption
