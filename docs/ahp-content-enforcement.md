# AHP content enforcement

Speakeasy's `/ahp` endpoint implements Agent Hooks Protocol (AHP) boundaries using the protocol's event-specific capabilities, not a particular harness's support matrix. A client must emit the event, register an `intercept` subscription, grant the required effects, and apply the response before exposing the content. Supporting an effect on the server does not make an observation-only client enforce it.

## Registration

Keep interception and observation subscriptions separate. An interception subscription requires a timeout and failure policy as well as its event and content selection:

```json
{
  "events": ["tool.before", "user.message.inbound"],
  "mode": "intercept",
  "timeoutMs": 30000,
  "failurePolicy": "fail-closed",
  "content": { "default": "body" }
}
```

Select additional events only when the client exposes their interception boundary. The client must also advertise event-specific capabilities in each request. For output replacement, the `modify` effect family alone is insufficient: the matching nested replacement capability must be granted. Merge-only support is not sufficient, because merging message lists appends content rather than removing the original content.

Observation requests never return enforcement effects. Missing enforcement capabilities leave the event observational; they must not be reported as successful blocking. Protocol or transport errors follow the client's failure policy. A successful response with no effects means the server did not request a block or replacement; it is not a timeout.

Policies must be enabled, have an enforcing action, and apply to the authenticated actor and content category. Ordinary API-key hook calls use the key creator's identity unless the supported attribution path resolves another actor. A Platform MCP login does not establish the identity of a separate hook key. Organization-wide plugin keys do not automatically attribute all callers to their creator.

## Implemented server coverage

| Boundary                                        | Server action on a matching enforcing policy              | Required capability                    |
| ----------------------------------------------- | --------------------------------------------------------- | -------------------------------------- |
| `tool.before`, `tool.permission.request`        | Deny execution or permission                              | `deny`                                 |
| `user.message.inbound`, `user.message.outbound` | Deny admission or delivery                                | `deny`                                 |
| `model.request.before`                          | Deny the request                                          | `deny`                                 |
| `tool.after`                                    | Replace the output with an empty canonical message list   | `modify` and `modify.output.replace`   |
| `model.response.after`                          | Replace the response with an empty canonical message list | `modify` and `modify.response.replace` |
| `context.compact.before`                        | Deny compaction                                           | `deny`                                 |
| `context.compact.after`                         | Replace the summary with an empty text-part list          | `modify` and `modify.summary.replace`  |

The server also records boundary-only observations for model requests, outbound messages, compaction, permission requests and resolutions, tool batches, model errors, and turn end. These do not manufacture transcript messages or additional usage. Existing tool-result, model-response, and session capture remains separate from the enforcement decision.

`turn.start` interception remains an acknowledged no-op: it is not authoritative new user input. Turn-finish, elicitation, configuration/task/workspace changes, and model-switch interception remain unsupported rather than pretending to enforce them as ordinary prompts. Their protocol capabilities are listed below to distinguish an implementation boundary from a client or protocol limitation.

## Protocol boundary audit

The table below describes AHP v0.8.0's available enforcement mechanisms. It is not a claim that every harness implements them, or that every event is a Speakeasy content-policy boundary.

| Event                                                                                                                                                                                                                                                | Relevant protocol mechanism                          | Meaning and limits                                                                                                                                    |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tool.before`                                                                                                                                                                                                                                        | `deny`                                               | Prevent tool execution.                                                                                                                               |
| `tool.permission.request`                                                                                                                                                                                                                            | `deny`                                               | Refuse permission; permission is not proof of execution.                                                                                              |
| `tool.after`                                                                                                                                                                                                                                         | `modify`, target `output`, operation `replace`       | Replace the canonical output message list before subsequent delivery. No `deny` or returned-result effect exists here; completed side effects remain. |
| `model.request.before`                                                                                                                                                                                                                               | `deny`                                               | Prevent the model request. A context snapshot is not a new user prompt.                                                                               |
| `model.response.after`                                                                                                                                                                                                                               | `modify`, target `response`, operation `replace`     | Withhold response content before subsequent use. Does not retract already streamed text.                                                              |
| `user.message.inbound`                                                                                                                                                                                                                               | `deny`                                               | Prevent admission of a new user message.                                                                                                              |
| `user.message.outbound`                                                                                                                                                                                                                              | `deny` or content replacement                        | Prevent delivery to the user at the explicit outbound boundary.                                                                                       |
| `context.compact.before`                                                                                                                                                                                                                             | `deny`; instruction modification or returned summary | Prevent compaction, not remove unsafe content from the existing context.                                                                              |
| `context.compact.after`                                                                                                                                                                                                                              | `modify`, target `summary`, operation `replace`      | Replace a summary before installation. There is no deny effect.                                                                                       |
| `turn.start`                                                                                                                                                                                                                                         | Deny or prompt modification                          | A lifecycle snapshot is not necessarily authoritative new user input.                                                                                 |
| `turn.finish.before`                                                                                                                                                                                                                                 | Response replacement or flow control                 | May run after content was already exposed; it is not interchangeable with outbound delivery.                                                          |
| `tool.batch.after`                                                                                                                                                                                                                                   | Flow control, injection, messages                    | Cannot replace individual results or undo completed calls. Use per-call `tool.after` interception.                                                    |
| `session.start`                                                                                                                                                                                                                                      | Injection and messages                               | Not a content-denial boundary.                                                                                                                        |
| `model.switch.before`                                                                                                                                                                                                                                | Deny model selection                                 | Not conversational-content sanitization.                                                                                                              |
| `config.change.before`, `task.change.before`, `workspace.change.before`                                                                                                                                                                              | Deny the corresponding change                        | Require their own configuration/task/workspace authorization semantics. Generic optional content is not a new prompt.                                 |
| `user.elicitation.request`                                                                                                                                                                                                                           | Deny or return an elicitation result                 | Form and URL requests have additional capability requirements.                                                                                        |
| `user.elicitation.result`                                                                                                                                                                                                                            | Accepted form-content modification                   | Not an ordinary assistant message or tool completion.                                                                                                 |
| `session.end`, `turn.end`, `turn.progress`, `model.error`, `model.switch.after`, `tool.permission.resolved`, `tool.progress`, `config.change.after`, `task.change.after`, `workspace.change.after`, `user.attention`, `file.changed`, `hook.failure` | Observation only                                     | No synchronous enforcement response. Streaming progress is not retractable.                                                                           |

A flow-stop effect changes continuation; it does not sanitize the current result. A block on returned content must replace or withhold that content, not append a warning to it.

## Content and provenance

- Inspect the actual boundary's inline content, not a tool's original input as a substitute for missing output.
- Preserve user, assistant, tool-request, and tool-response distinctions. Tool results are untrusted content even when included in a later model request.
- Inspect supplied tool results even if the tool never executed, without fabricating an executed tool-completion audit record.
- Preserve response usage and transcript capture independently of enforcement. Scanning a model context snapshot must not duplicate the conversation or its usage.
- The same content can legitimately be checked at multiple exposure boundaries. Event identities must keep replay and capture behavior stable.
- System and developer request items are not scanned: current canonical policy message types do not represent those instruction roles. Provider-request enforcement therefore does not cover secrets or PII in those items.
- Inline assistant tool-call parts use tool-request policy scope, but lack reliable tool-name and call-ID attribution; authoritative `tool.before` remains necessary for tool-specific policies.
- Inline text coverage does not imply inspection of referenced files, attachment bodies, omitted content, or media. The endpoint does not fetch arbitrary URLs or resolve client-local handles.
- Returned-output enforcement does not reverse tool side effects, provider processing, or content already streamed to a user.

## Integration scope

This endpoint uses the existing project risk policies and actor grants. Policy administrators continue to use the existing Platform MCP policy tools; no new management operation or tool contract is required for a harness to deliver these events. The change does not introduce a new dashboard entity or telemetry dimension, so existing demo policies and findings remain representative.

## Verification

Use handler-level tests with a client capability matrix, not only a single harness end-to-end test. Cover matching and clean content, observation delivery, missing effect families, missing nested replacement grants, merge-only grants, multiple output items, failures, and supplied results. Validate effects against the SDK's typed response contract.

For a live pre-call test, put synthetic sensitive-looking data literally in the tool input. Generating a value inside a command instead tests the output boundary. Verify an explicit denial or replacement separately from a later background risk finding.
