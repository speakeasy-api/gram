# AGE-3351 Admin Organization Overview Implementation Plan

**Goal:** Match the approved organization Overview layout while preserving existing organization controls.

**Approved design:** Wide Details column, Enterprise trial side panel, Danger zone; Extend/Re-arm move into the side panel; converted state hides the panel.

**Constraints:** TDD each task. Reuse existing mutations and eligibility. Do not implement AGE-3149 conversion. Validate visually with Playwright. Do not push.

## Task 1: Separate lifecycle and trial action scopes

**Files:** `client/admin/src/pages/organizations/OrganizationActions.test.tsx`, `OrganizationActions.tsx`

- Add RED tests: lifecycle excludes Re-arm; trial includes Re-arm but excludes Disable/Re-enable; live trial Extend remains trial-only.
- Run `aube run -F admin test -- src/pages/organizations/OrganizationActions.test.tsx` and capture RED.
- Add a scoped `showRearm` condition and use it in button/menu layouts.
- Re-run focused test for GREEN.
- Commit.

## Task 2: Build responsive Overview panels

**Files:** `client/admin/src/pages/organization/Overview.test.tsx`, `Overview.tsx`

- Add RED tests for Details, Enterprise trial, Danger zone; state visibility; actions in their new panels.
- Preserve account type, whitelist, copy controls, and TrialFacts.
- Build wrapping wide-left/narrow-right layout. Left contains Details and Danger zone. Trial panel shows for running, ending-soon, expired, demoted; hides for no trial and converted.
- Put `OrganizationActions actions="trial"` in trial panel and `actions="lifecycle"` in Danger zone.
- Expired panel has no invented action. Use existing eligibility.
- Run focused test for GREEN.
- Commit.

## Task 3: Remove obsolete record-level placements

**Files:** `client/admin/src/pages/organization/RecordHeader.test.tsx`, `RecordHeader.tsx`, `RecordLayout.test.tsx`, `RecordLayout.tsx`, delete `TrialCallout.tsx` and `TrialCallout.test.tsx`

- Add RED assertions: header keeps Open in Dashboard but no lifecycle actions; layout has no page-wide trial callout.
- Remove old action/callout placements and delete unused callout.
- Run focused tests for GREEN.
- Commit.

## Task 4: Verify and review

- Run focused organization tests:
  `aube run -F admin test -- src/pages/organization/Overview.test.tsx src/pages/organization/RecordHeader.test.tsx src/pages/organization/RecordLayout.test.tsx src/pages/organization/TrialFacts.test.tsx src/pages/organizations/OrganizationActions.test.tsx src/pages/organizations/rowActions.test.tsx`
- Run `aube run -F admin type-check`.
- Start the local admin stack using repository tasks; validate responsive/state behavior with Playwright and capture bounded screenshots.
- Task review after every task; fix/re-review; final branch review.

## Routine rulings

- Use label **Enterprise trial**.
- Do not add **Mark as converted** in this ticket.
- Converted trial facts remain in Details while the side panel hides.
- Existing server/client eligibility remains authoritative.
