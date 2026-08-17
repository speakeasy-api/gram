---
name: review-rfc
description: Use when reviewing, critiquing, stress-testing, approving, or identifying gaps in an existing Gram RFC, technical proposal, architecture decision, migration design, or feature design before implementation.
---

# Review an RFC

Review the proposal as a decision artifact and technical argument, not as a
template-completion or copy-editing exercise. Prioritize correctness,
feasibility, customer impact, security, operability, and reversible delivery.

## Boundaries

- Do not change RFC status, publish an approval, comment externally, rewrite, or
  implement unless the user requests that specific action. Still provide the
  evaluative verdict required by this review.
- A request to approve authorizes the external action, not a predetermined
  verdict. Never publish an approval that contradicts the evidence-based
  verdict. Verify the destination before any external comment; if it is
  unknown, return the proposed comment and request the target.
- Never repeat customer-identifying or secret information into repository
  artifacts. Replace it with placeholders and flag the confidentiality issue.
- Treat the RFC as evidence of the proposed intent, not independent proof of
  current-system facts or predicted outcomes.

## Gather evidence

1. Read the complete RFC and applicable `AGENTS.md` files.
2. Inspect evidence needed to test decisive claims and highest-consequence
   failure modes. Consider referenced code, schemas, APIs, related RFCs,
   feature gates, billing paths, and operations only when material; state the
   resulting evidence boundary.
3. Load any area-specific skill triggered by the proposal.
4. Separate verified facts, author assumptions, and reviewer inferences.

If the user intentionally supplies only an excerpt and no complete source is
available, review that scope, state that whole-document completeness is
unverified, and describe omitted material as absent from the excerpt rather
than definitively absent from the RFC.

Honor explicit evidence limits, including a prohibition on repository
inspection, and state the verification those limits leave incomplete.

Do not infer maturity from the RFC's workflow status. A Draft may already be a
complete implementation blueprint, while an Approved RFC may still contain a
stale placeholder. Review the actual argument and evidence.

For approval-blocking evidence gaps, name the missing artifact, affected
decision, owner when known, and closure condition. Report lesser uncertainty
without manufacturing a finding. Require production-topology evidence when it
affects capacity, availability, residency, security, rollout, or rollback.

## Review the template contract

Check every required section; apply context-dependent rows only when relevant.
For an intentionally scoped excerpt, do not create findings merely because
whole-RFC sections are not shown. Raise an omission only when a decision in the
excerpt depends on that missing material:

| Area                   | Review question                                                                                                                                                                                 |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Overview               | Does it connect current state, customer or business consequence, why the problem matters, and the proposed direction?                                                                           |
| User stories           | For customer-affecting work, do representative stories connect an actor and need to the obstacle and intended outcome without inventing customer facts?                                         |
| Goals                  | Are goals verifiable and compatible with non-goals?                                                                                                                                             |
| Stretch goals          | Are optional outcomes kept out of committed scope?                                                                                                                                              |
| Key decisions          | Do they accurately summarize the proposal and give the rationale or constraint behind each choice?                                                                                              |
| Proposal               | Are producers, consumers, interfaces, data flows, state changes, ownership, authorization, and failure behavior concrete?                                                                       |
| Data model             | Are important fields summarized with type and meaning, without an exhaustive schema dump?                                                                                                       |
| Detail level           | Does it prefer conceptual tables and pseudocode, use focused syntax only when exact semantics are decision-critical, and avoid full DDL, YAML/API specs, generated schemas, or production code? |
| User Experience        | Are dashboard, CLI/API, self-service, docs, and migration covered or explicitly unaffected?                                                                                                     |
| Billing                | Are pricing, packaging, entitlements, metering, and billing traced?                                                                                                                             |
| Threat Modeling        | Are trust boundaries, threats, controls, accepted risks, and InfoSec review credible?                                                                                                           |
| Alternatives           | Are credible options compared using consistent criteria?                                                                                                                                        |
| Rollout and Validation | Are tests, monitoring, success criteria, failure scenarios, rollback triggers, and mechanics concrete?                                                                                          |
| Open Questions         | Are unresolved decisions visible and owned where possible?                                                                                                                                      |

Also test compatibility, authorization, tenant isolation, privacy, concurrency,
data lifecycle, migrations, capacity, degraded dependencies, observability,
incident response, and blast radius where relevant.

Check the decision history when present. Resolved Questions should retain the
answer and evidence; verification updates should say exactly what was and was
not proved; amendments should distinguish the accepted design from the shipped
path without rewriting history. Flag stale statements that conflict with a
later amendment or implementation result.

## Report findings

Open with a concise summary of the decision, decisive findings, and evidence
coverage.

Use only these severities:

- **Blocking:** unsafe, incorrect, or unable to satisfy a stated goal.
- **Major:** material design gap to resolve before implementation.
- **Minor:** worthwhile improvement that does not invalidate the design.
- **Question:** missing information that may change the verdict.

Do not issue `Approve` when a plausible material risk to correctness, billing,
security, tenant isolation, rollout, or operability remains unresolved and
additional evidence could change the verdict. Unresolved material Questions
require `Revise before approval`; non-material Questions may accompany
`Approve with changes`.

Do not label every omission `Blocking`. Use `Blocking` only when the current
proposal is demonstrably unsafe, incorrect, or cannot satisfy a stated goal;
use `Major` for a material design gap and `Question` when missing evidence may
change the verdict. Style or template preferences are findings only when they
hide a decision, evidence gap, or plausible consequence.

Each finding must include the RFC section, concrete concern, plausible impact or
failure scenario, supporting evidence, and smallest useful resolution. Avoid
generic checklist findings without a demonstrated consequence.

Finish with one verdict: `Approve`, `Approve with changes`, or `Revise before
approval`. State which findings determine the verdict and which verification
remains incomplete.
