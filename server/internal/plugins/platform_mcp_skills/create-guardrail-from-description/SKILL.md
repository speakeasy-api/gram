---
name: create-guardrail-from-description
description: Create a risk policy in an explicit Speakeasy AI Control Plane project from a plain-language description of the risk, starting from a use-case preset or a bespoke prompt-based guardrail. Use when an administrator says what they want to catch or prevent rather than which detectors to enable.
---

# Create a guardrail from a description

Turn "stop agents deleting anything in production" into a reviewed policy: map the description to a preset, show the administrator exactly what the policy will do, and create it only after they confirm.

## Scope and prerequisites

- Use the executing client's authenticated Platform MCP connection. Installing this skill or signing in to the dashboard does not establish MCP authorization.
- Policy writes need `org:admin` in the connected organization and require an explicit project slug. Do not silently choose the Default project; a request that explicitly names it is sufficient.
- Never request or expose credentials. If authentication or permissions fail, stop and ask the user to reconnect or obtain access through the approved setup flow.
- This skill creates one policy. It does not edit existing policies, create exclusions, or change MCP configuration.

## Workflow

1. Call `get_platform_context` and confirm the organization with the user. Stop on a mismatch.
2. Establish the exact project slug. If the user has not named one, call `list_projects` and ask them to choose. Do not infer scope from a partial list.
3. Call `suggest_risk_policy` with the administrator's description in their own words. Do not paraphrase it first.
4. Review the result before showing it:
   - When `preset` is set, the draft is that preset's detectors, action and severity. Call `list_risk_presets` if you need the full catalog to explain the choice or to offer the runner-up presets in `alternatives`.
   - When `preset` is empty, the draft is a prompt-based guardrail whose `prompt` is the instruction the policy model will judge each message against. Read it back to the user; tighten it with them if it is broader or narrower than they meant.
   - If the description asks to stop, block or deny something but the draft's action is `flag`, say so and ask whether to block. Sources marked flag-only cannot block.
   - The `non_corporate_accounts` preset detects nothing until `approved_email_domains` is supplied. Ask for the domains before creating it.
5. Present the draft in plain words: what it detects, on which surfaces, what happens when it fires, and how severe findings are. Never show tool names, keys or receipts.
6. After the administrator explicitly confirms, call `create_risk_policy` with the exact `project_slug`, a fresh `idempotency_key`, and either the `preset` id plus any agreed overrides (`name`, `action`, `score`, `user_message`, `approved_email_domains`, `presidio_entities`, `prompt`) or the draft's fields with `policy_type`. Omit `enabled` to create it enabled.
7. Read the result. A replayed receipt means the same policy already existed; say so rather than reporting a new policy. A refusal naming prompt policies means prompt-based policies are not switched on for the project; offer a standard preset instead.
8. Call `get_risk_policy` with the returned policy id and confirm the stored name, action, severity and enabled state match what the administrator approved. Report what changed and where findings will appear.

## Interpretation and safety

- A preset is a starting point, not a verdict. Prefer the preset over a bespoke prompt when both fit: it is deterministic and cheaper to evaluate.
- Keep the administrator's words in the bespoke prompt. Add scope ("production", "customer records") only when they said it.
- Do not create more than one policy per confirmation. If the description covers two risks, propose two drafts and confirm each.
- Findings from a `flag` policy are visible in Watchdog; `warn` and `block` interrupt the session. Say which one the policy will do before creating it.
