# AGE-3131 Manual Enterprise Trial Conversion Plan

**Goal:** Add a dedicated admin transition that records an enterprise contract conversion, safely restores trial-demoted access, and remains retry-safe.

**Constraints:** Strict TDD. Dedicated endpoint; organization ID only; authenticated admin actor. Eligible running, ending-soon, expired, demoted. Preserve `ends_at`, historical `demoted_at`, and organization `disabled_at`. Server assigns `converted_at`. No trial/PAYG return 409; missing org 404. Upstream key work precedes conversion/admission commit and removes only `trial_demotion`. Already converted succeeds without duplicate audit but always retries post-commit `TrialInactive`. Audit action `organization:enterprise_trial_converted`; source `admin`, while Stripe uses `stripe_checkout`.

## Task 1: Shared conversion audit and Stripe attribution

**Files:** `server/internal/audit/organizations.go`, `server/internal/outbox/events/audit_log.go`, `server/internal/usage/stripe_webhook.go`, focused usage tests.

- Add failing tests proving first Stripe conversion writes one enterprise-trial-converted audit/outbox event with `conversion_source=stripe_checkout`; replay writes none; audit failure rolls back conversion/PAYG activation.
- Add the shared action/logger and make Stripe use `MarkTrialConverted` row count to gate the event in its existing transaction.
- Run focused then full usage/audit tests.
- Commit `feat: audit enterprise trial conversions`.

## Task 2: Dedicated admin conversion endpoint and transaction

**Files:** `server/design/admin/design.go`, `server/internal/trials/queries.sql`, generated Goa/SQLc, `server/internal/admin/impl.go`, setup/tests, new `server/internal/admin/converttrial_test.go`.

- Add failing endpoint/service tests for running, ending-soon, expired, demoted, already converted, missing org, no trial, PAYG, disabled organization, and updateOrganization non-conversion.
- Add `markEnterpriseTrialConverted`: admin auth, required organization `id`, `POST /admin/trial.convert`, `AdminOrganization` result.
- Add SQL that locks the trial first, assigns `converted_at` with `clock_timestamp()`, preserves `ends_at`/`demoted_at`, and distinguishes eligibility/idempotency/conflicts.
- Generate with `mise run gen:sqlc-server` and `mise run gen:goa-server`; never hand-edit generated files.
- Implement global order: trial row lock → deterministic chat/internal billing locks → required OpenRouter work → organization/features/audit writes → commit.
- Apply enterprise limits to every `openrouter.AllKeyTypes` key and remove only `DisableCauseTrialDemotion`; preserve other causes and missing-key no-ops.
- Restore demoted account/admission/features without clearing `disabled_at`. Active/expired enterprise conversions avoid unnecessary restoration.
- Write one admin-actor audit/outbox event with `conversion_source=admin` in the transaction.
- After commit, recap legacy zero-limit keys, refresh caches, and call `TrialInactive`. Already-converted retries skip mutation/audit but still call `TrialInactive`; notifier failures do not undo success.
- Run focused and full admin/trials/OpenRouter tests.
- Commit `feat: convert enterprise trials manually`.

## Task 3: Concurrency, retry, and lifecycle regressions

**Files:** admin conversion tests plus relevant demotion, trial-email, audit, and OpenRouter tests.

- Add deterministic bounded tests proving conversion serializes against demotion using trial-row-before-key-lock ordering.
- Prove upstream failure commits no converted/admitted state; retries converge after partial upstream work.
- Prove composed causes preserve admin/billing locks and effective disable state.
- Prove repeated converted calls re-enqueue `TrialInactive` but never duplicate conversion audit.
- Prove converted trials cannot be demoted or re-armed.
- Run full admin, usage, background activities, background, trialemails, audit, trials, and OpenRouter suites.
- Commit `test: cover enterprise trial conversion races` if separate changes are required.

## Task 4: Verification and PR stack

- Run `mise lint:server` and repository compile-only check.
- Run generated checks and confirm no schema migration is introduced.
- Run focused/aggregate server tests and inspect audit payloads for key/customer-data leakage.
- Rebase the remaining stack branches, run post-rebase checks, request final branch review, then push/update stacked PRs without bypassing safeguards.

## Routine rulings

- Already-converted success applies only to non-PAYG trial conversions; PAYG remains a conflict for this endpoint.
- Audit/outbox is transactional; `TrialInactive` is strictly post-commit and repeatable.
- Explicit enterprise limits are supplied during pre-commit key work; any legacy zero-limit policy recap happens after commit.
- No conversion reversal, no inference from account-type edits, and no customer-facing API change.
