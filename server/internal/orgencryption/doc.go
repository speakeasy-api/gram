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
//	  FOR KEY SHARE
//	), 0) = @decided_generation
//
// The COALESCE is required: an organization with no transition row is at
// generation 0, and without it every write of a platform_key organization
// would be fenced.
//
// FOR KEY SHARE is required too. The subquery alone reads the state as of the
// statement's start, and a write could commit long after a transition that
// committed in between. The lock closes that gap: a writer holds it on the
// transition row until its own transaction ends, and every transition locks
// that row FOR UPDATE first, so a transition waits for open writes to commit.
// A write that reaches the row while a transition holds it waits, then
// re-checks the committed row and is fenced if the generation moved. The
// lock does not conflict with ordinary updates of the transition row, such
// as the runner's bookkeeping. orgencryptiontest.RequireFencedWrites checks a
// feature's queries.sql for this whole shape.
//
// An organization with no transition row has nothing to lock, so keep writes
// that touch either copy short: one statement in autocommit, or a short
// transaction that never waits on a provider or a person while it is open. A
// writer that held a transaction open across enrollment and the minimum time
// in dual_write could still write a platform_key copy after finalize.
//
// An INSERT fences through INSERT ... SELECT ... WHERE or ON CONFLICT ... DO
// UPDATE ... WHERE. A statement that matches no row because of the fence
// returns ErrFenced to its caller, which decides again with Cipher.Redecide,
// seals the same plaintext again, and retries the write. It never repeats a
// call to a provider: a refresh may already have rotated the refresh token,
// and the only copy of the new one is the plaintext the caller holds.
//
// Transitions.ClearPlatformCopies still waits a settle delay and counts again
// after clearing, as a backstop for any write that bypasses the fence.
package orgencryption
