---
name: author-signals-and-sensors
description: Create or update reusable signals and message sensors in an explicit Speakeasy AI Control Plane project, with confirmed atomic authoring and live verification.
---

# Author signals and sensors

Use this workflow when someone wants the Speakeasy AI Control Plane (AICP) to
classify conversation messages into reusable labels, exclusive choices, or ordered
scores. Sensors perform classification; they do not enforce access policies.

## Discover

1. Successfully call `list_projects` through this client's authenticated Speakeasy
   connection. Have the user select the exact project. Package installation alone
   grants no authority. These tools require signals intelligence enabled and live
   project permissions; they are currently external-user-only.
2. Call `find_sensors` and `find_signals` for that project, searching and following
   pagination as needed. Present candidates rather than silently selecting a name
   match. Reuse a signal only after the user agrees its full criteria fit.
3. For an existing sensor, call `get_sensor` and inspect its complete ordered
   definitions. For a shared signal change, use `update_signal` with
   `confirmed:false` to inspect the affected sensors before asking for confirmation.

## Choose the definition

4. Agree the mode: `multi_label` evaluates independent labels; `exclusive` chooses
   one option; `ordered_score` uses two to ten levels ordered from low to high,
   each with classifier criteria. Agree instructions, display names, optional
   slugs, and reusable signal definitions.
5. Agree the matching expression. Omission on creation defaults to
   `message.role == "user"`. Only `message.role` is exposed; do not invent actor,
   department, cohort, replay, or tool fields. Conversation ingestion currently
   evaluates user/assistant creation events, including historical imports.
6. Optionally call `preview_sensor_match` with bounded, caller-supplied role
   examples. Report matched, not matched, and errors separately. Missing message
   context is unbound, not an empty role. This tests metadata eligibility, not
   classification quality, and neither retrieves transcripts nor runs inference.

## Preview and confirm

7. Prefer `create_sensor` with a `proposal` containing existing signal IDs and/or
   inline `new_signal` definitions. The operation creates new signals and the
   sensor together atomically. Use `create_signal` when the user only wants a
   reusable catalog definition. Use `update_sensor` for a selected sensor's own
   fields or ordered membership, and `update_signal` to deliberately change a
   shared definition. Set `proposal.operation` to the tool name.
8. Call with `confirmed:false`. Show the normalized configuration, matching
   expression, complete signal order, draft reasons, and every affected sensor
   returned for a shared-signal update. Preview creation IDs are provisional and
   are not committed resources. Omitted update fields are preserved; a supplied
   signals list replaces the entire membership, and an empty list clears it.
9. Obtain explicit confirmation of that exact proposal and its shared impact.
   Resend the identical proposal with `confirmed:true`, the returned
   `expected_version` (from `version`) and `preview_token`, and a stable
   `idempotency_key`. Keep that key and all inputs unchanged when retrying an
   uncertain request. A version conflict requires another read, preview, and
   confirmation; the version covers the whole project's configuration.

## Verify

10. For a sensor, call `get_sensor` using the committed ID. For a standalone
    signal, call `find_signals` and verify its exact ID and definition. Report
    committed state and configuration readiness separately from active inference
    or observed readings, which these tools cannot prove.
11. A replayed receipt proves a historical write, not current existence. Check
    `target_available` and the fresh target state; do not recreate a deleted
    target by changing the retry key. Present the returned dashboard path when
    available. If the capability is unavailable, stop rather than bypassing its
    permissions or inventing another tool.

Never ask for API keys, passwords, tokens, OAuth codes, client secrets, or secret
headers. Use synthetic examples rather than requesting private transcripts. If
the user wants to report workflow feedback, obtain consent before calling
`send_platform_mcp_feedback`.
