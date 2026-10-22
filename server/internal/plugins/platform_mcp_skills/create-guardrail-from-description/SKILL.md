---
name: create-guardrail-from-description
description: Use when an administrator of a Speakeasy AI Control Plane project says what they want to catch or prevent, in the dashboard's Guardrails terms or their own words, rather than which detectors to enable. Creates one risk policy in an explicit project from a use-case preset or a bespoke prompt-based guardrail through the Platform MCP.
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
   - Settle where the policy applies whenever the draft's action is `block`, or the description names an MCP server or tool. Without `mcp_scope` the policy is enforced inside the agent sessions the project observes, including the tool calls those agents make, and is not enforced at an MCP server for callers outside such a session. With `mcp_scope` it is checked at the gateway before the tool runs, and only checks calls through the chosen servers. Say both and ask which they want; if they want both, treat it as two policies. When `can_scope_to_mcp` is false the policy applies to agent sessions only; say so instead of offering the choice.
   - For a scoped policy, call `find_mcp` in the same project to resolve each server the administrator named to its id. Stop and ask if a name matches no server or more than one. A scoped policy supports only `flag` and `block`. Add `tool_annotations: ["destructiveHint"]` only when they asked for destructive tools specifically, and tell them tools without annotations will not match.
   - The `non_corporate_accounts` preset detects nothing until `approved_email_domains` is supplied. Ask for the domains before creating it.
5. Present the draft in plain words: what it detects, where it applies (agent sessions, or the exact MCP servers by name), what happens when it fires, how severe findings are, and that it starts enforcing as soon as it is created unless they want it created disabled. Never show tool names, keys or receipts.
6. After the administrator explicitly confirms, call `create_risk_policy` with the exact `project_slug` and a fresh `idempotency_key`. With a preset, pass the `preset` id plus any agreed overrides (`name`, `action`, `score`, `user_message`, `enabled`, `approved_email_domains`, `presidio_entities` for standard presets, `prompt` for prompt-based presets, `mcp_scope` when the administrator chose MCP servers); leaving out `enabled` creates it enabled. Without a preset, pass the draft's fields with `policy_type`, `name` and `enabled` set explicitly, since those branches require them.
7. Read the result. A replayed receipt means the same policy already existed; say so rather than reporting a new policy. A refusal naming prompt policies means prompt-based policies are not switched on for the project; offer a standard preset instead.
8. Call `get_risk_policy` with the returned policy id and confirm the stored name, action, severity, enabled state and `mcp_scope` match what the administrator approved. Report what changed and where findings will appear.

## Interpretation and safety

- A preset is a starting point, not a verdict. Prefer the preset over a bespoke prompt when both fit: it is deterministic and cheaper to evaluate.
- Keep the administrator's words in the bespoke prompt. Add scope ("production", "customer records") only when they said it.
- Do not create more than one policy per confirmation. If the description covers two risks, propose two drafts and confirm each.
- Findings from a `flag` policy are visible in Watchdog; `warn` and `block` interrupt the session. Say which one the policy will do before creating it.
