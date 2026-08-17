# RFC: <YOUR TITLE HERE>

Status: Draft  
Author: <NAME>  
Reviewers: <NAMES>  
Team channel: <CHANNEL>  
Linear project: <LINK>  
Design partners: <NAMES>  
Interested customers/prospects: <NAMES>

## Overview

Describe the problem, its impact on customers or the business, and the proposed
solution at a high level. Establish why this work matters and build the case for
addressing it.

For customer-affecting work, include representative user stories that connect
the affected user, their need, the current obstacle, and the intended outcome.

## Goals

- <Goal>
- <Goal>
- <Goal>

### Non-goals

- <Non-goal>
- <Non-goal>

### Stretch goals

- <Stretch goal>
- <Stretch goal>

## TLDR / Key Decisions

> Complete this section after writing the proposal.

- <Key decision>
- <Key decision>
- <Key decision>

## Proposal

Explain how the problem will be solved. Include enough detail for reviewers to
understand the design, important trade-offs, and expected outcomes.

Describe the important interfaces and how data moves between them, including
ownership, trust boundaries, state changes, and failure behavior.

For data-model changes, summarize only decision-relevant fields in a table with
their names, types, and descriptions. Include constraints or defaults when they
affect the design; do not reproduce the complete schema.

Use diagrams, tables, examples, or pseudocode where they aid understanding.
Avoid full DDL, YAML or API specifications, generated schemas, and detailed
production code; link to those artifacts when reviewers need exact syntax.

### User Experience

Describe how customers will discover and use the feature.

Cover the relevant:

- Dashboard experience
- CLI or API experience
- Self-service workflow
- Documentation changes
- Migration experience

### Billing

Explain whether the feature affects pricing, packaging, entitlements, usage
measurement, or billing.

If there is no impact, state that explicitly.

### Threat Modeling

Describe the trust boundaries and data flows introduced or changed by this
proposal.

| Threat   | Control      |
| -------- | ------------ |
| <Threat> | <Mitigation> |
| <Threat> | <Mitigation> |

Record any accepted risks and identify the Information Security reviewer.

## Alternatives Considered

### <Alternative>

Explain the alternative, its advantages, and why it was not selected.

### <Alternative>

Explain the alternative, its advantages, and why it was not selected.

## Rollout and Validation

Describe how the feature will be tested, released, monitored, and—if
necessary—rolled back.

Include concrete success criteria and important failure scenarios.

## Open Questions

- <Open question>
- <Open question>
