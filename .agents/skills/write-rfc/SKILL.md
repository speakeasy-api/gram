---
name: write-rfc
description: Use when drafting, creating, scoping, or substantially revising a Gram software-engineering RFC, technical proposal, architecture decision, migration design, or feature design before implementation.
---

# Write an RFC

Produce a decision-ready technical argument grounded in the current system.
Follow `assets/rfc-template.md`, preserve every top-level section, state "No
impact" when appropriate, and add useful domain-specific subsections.

## Boundaries

- Treat drafting, publishing, approving, and implementing as separate actions.
  Do not post the RFC or change code unless the user explicitly requests it.
- Draft and publish in one request only after checks pass and destination,
  audience, and confidentiality boundary are unambiguous.
- Never put customer names, identifiers, emails, URLs with identifying slugs,
  or commercial figures in repository artifacts. Use `<CUSTOMER>`,
  `<PROJECT_ID>`, or "the customer" and keep real values in internal systems.
- Use customer identity internally only when explicitly authorized and allowed
  by destination policy; otherwise redact it.
- Mark unknown metadata with angle-bracket placeholders. Do not invent authors,
  reviewers, channels, projects, design partners, or customers.

## Establish the decision

Identify the problem, affected users, customer or business impact, desired
outcome, constraints, decision deadline, and success criteria. Build the
Overview as: current state, concrete consequence, why it matters, and proposed
direction. For customer-affecting work, include representative user stories
that connect an actor and need to the current obstacle and intended outcome;
use `As a …` syntax only when it reads naturally. Never invent customer facts.

## Ground the proposal

1. Read the applicable `AGENTS.md` files and any skills triggered by the area.
2. Search for related RFCs, plans, docs, implementations, APIs, schemas, feature
   gates, billing paths, and operational conventions.
3. Trace the current behavior before proposing a replacement.
4. Cite repository evidence and internal sources without copying confidential
   identifiers; separate verified behavior, assumptions, and recommendations.

Do not present an invented interface, type, table, or service as current state.

## Design the proposal

- Make goals verifiable without forcing artificial metrics; keep non-goals
  explicit; label optional work as stretch goals.
- Trace important interfaces and data flows end to end: producers, consumers,
  synchronous or asynchronous boundaries, identity and authorization, state
  changes, failure behavior, and operational ownership.
- For data-model changes, use a compact table for decision-relevant fields with
  name, type, and meaning; add constraints or defaults only when material. Do
  not enumerate every field or reproduce the complete schema.
- When changing a CLI or API contract, characterize existing flags, request and
  response shapes, stdout/stderr behavior, exit semantics, and compatibility
  expectations with repository evidence or golden tests.
- Cover every User Experience surface named by the template. State why an
  unaffected surface is unaffected.
- Trace pricing, packaging, entitlements, metering, and billing. State "No
  billing impact" only with evidence; otherwise write `Billing impact:
Unverified`, name the missing evidence and owner, and keep it unresolved.
- Identify trust boundaries, sensitive data, authorization decisions, abuse
  paths, and failure modes. Pair each concrete threat with a control; record
  accepted risks and the required Information Security reviewer.
- Compare credible alternatives using consistent criteria. Do not manufacture
  weak alternatives to make the proposal appear stronger.
- Define incremental rollout, observability, success thresholds, important
  failure scenarios, rollback triggers, and rollback mechanics.

Use diagrams, tables, and pseudocode only when they clarify a material
relationship or decision. Keep code illustrative: do not paste full table DDL,
YAML or API specifications, generated schemas, or production-ready samples.
Reference exact existing contracts instead of copying them into the RFC.

## Write in Gram's RFC voice

- Write the Overview, rationale, and trade-offs as connected prose. Reserve
  bullets for parallel facts, requirements, or decisions.
- Say what happens today, what breaks, and what this RFC proposes. Label
  uncertainty `verified`, `partially verified`, or `unverified`.
- Write each TLDR item as a decision plus its rationale or governing
  constraint, for example: `**Dynamic sync, not package baking.** This keeps
targeting and revocation independent of plugin releases.`
- Keep simple conclusions proportionate. Once billing or another required
  no-impact claim has been traced, state it directly instead of inventing
  speculative detail.
- Organize around real decision domains, adding Architecture, Day 2 Operations,
  Resolved Questions, Verification Updates, or Appendices as useful. Keep large
  parent RFCs on shared architecture and rollout; move subsystems to child RFCs.

## Preserve the decision record

Treat an RFC as a living record, not a disposable draft. Move answered Open
Questions into `Resolved Questions` with the answer and supporting evidence.
Add dated verification updates when experiments narrow or invalidate a claim.
After review or acceptance, do not silently rewrite a material decision;
record the correction, supersession, or amendment explicitly while preserving
the earlier context.

## Finish the RFC

Write `TLDR / Key Decisions` after completing the proposal. Remove unused
template helper text, but retain explicit placeholders for genuinely unknown
metadata or decisions. Then verify:

1. Every goal maps to proposal behavior and validation evidence.
2. Non-goals and stretch goals have not entered the committed scope.
3. Claims about current behavior are supported by sources.
4. User experience, billing, threat modeling, alternatives, rollout,
   validation, rollback, and open questions are complete.
5. Open questions name the missing decision and, when known, its owner.
6. The document contains no customer-identifying or secret information.
7. Every key decision states why it was chosen or which constraint it resolves.
8. Resolved questions, verification updates, and amendments preserve material
   decision history where applicable.
9. Customer stories, interfaces, data flows, and important data-model fields
   make the proposal concrete without turning it into an implementation spec.

Return the RFC and unresolved assumptions. If published, report destination,
status, and redactions; do not infer authorization to implement.
