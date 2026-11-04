# Internal audit PR split

Approved design: `../specs/2026-11-04-internal-audit-events-design.md`.

Sources: schema `ecca85bf82ac85e3db832c11c9b110b290957f52`; application `d2adba756de28e1264d22646262604ba0dfb5e4d`. Original branches remain unchanged.

1. **Core**, atop schema: persisted query contracts, explicit writer classification, outbox suppression, customer list/facet filtering, staff inclusion, row moves, behavioral regressions, audit guidance and original design/plan. Retain old API models: defer only response-field assignments and response-field assertions/tests.
2. **Visibility**, atop core: Goa/API/OpenAPI/SDK contracts, response mappings and tests, staff Internal badge, Admin MCP classification outputs and fixtures. Restore boundary files verbatim from source.
3. **Backfill**, atop visibility: standalone manual command, SQLc fixture/configuration, tests and runbook. No execution or infrastructure changes.

Verify each layer with focused tests and applicable lint; compare final tree to application source (only this plan may differ). Extract generated artifacts whole; never hand-edit them. Commit normally, with no push or PR creation until parent review.

Customer Platform MCP needs no new tool: existing audit reads inherit filtering. Staff Admin MCP parity belongs in visibility. Runtime/security events remain visible. Historical visibility gap until manual backfill is accepted.

Outstanding decisions unchanged: backfill TLS versus local proxy; repeated scan versus keyset; required SDK field compatibility with older servers.
