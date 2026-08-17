---
name: implement-rfc
description: Use when implementing, building, executing, or shipping an accepted Gram RFC or technical design in the repository, including code, schemas, migrations, tests, observability, documentation, and validation.
---

# Implement an RFC

Treat the accepted RFC as a requirements contract while verifying its
assumptions against the current repository. Do not silently redesign material
decisions during implementation.

## Establish the contract

1. Read the complete RFC, applicable `AGENTS.md` files, and skills triggered by
   every affected area.
2. Extract goals, non-goals, stretch goals, key decisions, security controls,
   rollout requirements, success criteria, and open questions.
3. Inspect the current implementation and related tests before editing.
4. Identify stale assumptions, contradictions, or missing decisions.

A stretch goal is out of scope unless the user, implementation plan, or accepted
RFC explicitly marks that specific goal for this implementation; its presence
under `Stretch goals` alone is not authorization. If the RFC is still Draft,
flag that fact. Ask before proceeding only when an unresolved decision
materially changes public behavior, security, data compatibility, billing,
migration safety, or implementation direction.

Never copy customer-identifying information into code, fixtures, comments,
filenames, branches, commits, logs, or other repository surfaces. Use approved
placeholders.

## Maintain traceability

Create and maintain a working table:

| RFC requirement | Planned change       | Verification       | Status  |
| --------------- | -------------------- | ------------------ | ------- |
| <Requirement>   | <Files or subsystem> | <Test or evidence> | Pending |

Include negative requirements and rollout controls, not only feature behavior.
Update the table as evidence is produced. Keep it in the working plan or final
handoff unless the repository or user requires a persistent artifact.

## Implement incrementally

1. Plan small stages that remain buildable and testable.
2. Find and update every call site when changing a public function, interface,
   schema, command, or API contract.
   If a caller cannot adopt the accepted contract without changing a material
   accepted decision, do not alter the contract or caller semantics; record the
   conflict and stop for direction.
3. Reuse existing abstractions and follow repository generation and migration
   workflows; never edit generated artifacts directly.
4. Add or update tests alongside each behavior change.
5. Add the RFC's authorization, threat controls, observability, compatibility,
   rollout, and rollback mechanisms.
6. Run focused checks after each stage and broader repository checks before
   completion.
7. Keep unrelated cleanup outside the RFC scope.

Repository edits do not authorize deployment, external posting, feature-flag
changes, data migration execution, or other production mutations.

## Handle deviations

When repository evidence requires a deviation, record:

- Original RFC decision
- Discovered constraint and evidence
- Implemented or proposed deviation
- Consequences for users, security, operations, and compatibility
- Whether the RFC must be updated or re-approved

Stop for direction when the deviation changes a material accepted decision.
If the deviation itself is not authorized, include the proposal in the handoff
and leave the affected requirement blocked. For an authorized deviation, record
it before treating it as an implementation requirement:

- When RFC or decision-record editing is authorized, persist it there.
- When external editing is not authorized, put an interim amendment in the
  visible working plan before editing code. Record its date and status,
  authorizing owner or approval reference, original decision, invalidating
  evidence, approved deviation, expected consequences, and publication status.
  That record is sufficient to proceed unless the user explicitly makes
  external publication a prerequisite.

Preserve the accepted RFC as decision history. Do not rewrite its goals or
proposal to imply that the shipped path was always the plan. In the final
handoff, expand the interim record into a dated
`Amendment — Shipped Implementation` containing:

- the final implementation status, references, and verification evidence;
- the evidence that invalidated or changed the accepted assumption;
- a compact accepted-versus-shipped comparison;
- capabilities removed, delayed, or narrowed;
- changed security, compatibility, operational, and user consequences; and
- follow-up work, external publication status, and explicitly superseded
  decisions.

An implementation link or ticket does not replace the amendment when the design
changed materially.

## Validate completion

1. Map every committed goal and key decision to implemented behavior.
2. Run the RFC's validation plan plus relevant tests, type checks, builds,
   linters, generators, migration checks, and end-to-end checks.
3. Exercise important failure scenarios and rollback controls where safe.
4. Review the final diff for scope, confidentiality, compatibility, and missing
   observability or documentation.
5. Mark requirements complete only with evidence; report anything unverified.

Return the implemented changes, requirement coverage, checks and results,
deviations, RFC amendment or decision-record status, unverified areas, and
remaining risks. Do not claim the RFC is fully implemented while required work
remains.
