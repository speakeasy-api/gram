# DLP policy flexibility beyond MCP

## Practical answer and scope

**Yes: Gram's risk-policy engine is not MCP-specific. It can gate governed Claude Enterprise inference (including browser conversations), selected model requests through LiteLLM, and prompts/native tools in instrumented agent harnesses. It is not a universal browser or HTTP DLP gateway, and policy-driven payload redaction is not implemented on these paths.** Coverage depends on the interception point, not merely on creating a policy.[^anthropic][^litellm][^native][^redaction]

Interpretation: “non-MCP” means (1) model API calls, (2) browser/app AI conversations, and (3) native tools or direct HTTP calls. “DLP” can mean detection, prevention of model use, prevention of disclosure to a provider, or prevention of external exfiltration; these are different guarantees. In particular, Anthropic inference hooks run **after content reaches Anthropic, before inference**—not before disclosure to Anthropic.[^vendor]

Evidence: static review of this repository at `13dc4edd94` plus the local working tree, and official Anthropic documentation retrieved during this research. No production configuration, rollout, or end-to-end runtime was verified. Existing unrelated edits were left untouched. “Supported today” below means an implementation exists, not that every deployment has it enabled.

## Capability matrix

| Surface / requirement                                                                              | Status                                                         | Actual capability and boundary                                                                                                                                                                                                                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Claude Enterprise: claude.ai, Cowork, Claude Code across supported web/desktop/mobile/CLI surfaces | **Supported today; conditional on vendor setup**               | Signed pre-inference transcript → Gram scanner → allow/deny. Official availability is Enterprise beta. Not a generic browser extension. Owner/Primary owner must enable enforcement; shadow mode, rollout sampling, and role exclusions can leave traffic unblocked/uninspected.[^anthropic][^vendor][^vendor-config] |
| Direct Claude Platform API, Bedrock, Google Cloud via Enterprise inference hooks                   | **Not supported by that integration**                          | Official scope excludes Claude Platform API organizations and availability excludes Bedrock/Google Cloud. A separate routed integration is needed.[^vendor]                                                                                                                                                           |
| Model APIs routed through LiteLLM                                                                  | **Supported today; conditional on proxy honoring verdicts**    | Gram returns `BLOCKED` on a pre-call policy deny. Current extraction scans the latest user text, not the entire request. External LiteLLM enforcement and complete routing were not runtime-verified.[^litellm]                                                                                                       |
| Arbitrary direct OpenAI/Anthropic/other model API calls bypassing integrations                     | **Unverified / no general interceptor found**                  | Neither the inference receiver nor LiteLLM endpoint intercepts an unrelated API connection. Do not promise prevention without a mandatory route or supported provider hook.[^anthropic][^litellm]                                                                                                                     |
| Native agent prompts and non-MCP tools, e.g. shell/fetch                                           | **Supported today on gated hook events**                       | Server scans prompt text/tool-input JSON; relay translates deny into host rejection. This inspects represented arguments, not necessarily the file bytes or final HTTP body produced by execution.[^native]                                                                                                           |
| General browser AI sites, arbitrary direct HTTP, uploads, clipboard                                | **Not established by this implementation**                     | No general browser/network inline DLP interceptor was found in the inspected hook/relay/LiteLLM/inference paths. A native browser tool can be gated as a tool; that does not cover independent browser traffic.[^native][^litellm][^anthropic]                                                                        |
| Model output / tool-result detection                                                               | **Conditional; generally not preventive at the same boundary** | Stored content can be analyzed asynchronously. Claude transcript history includes previous outputs at a later inference; LiteLLM response ingestion returns no action; native post-tool relay events are observational.[^output][^batch]                                                                              |
| Automatic masking/redaction before forwarding                                                      | **Not supported on these paths**                               | Actions are flag/warn/block/quarantine, not redact. Anthropic protocol supports allow/deny only; LiteLLM returns no replacement payload. Masked findings are evidence hygiene, not rewritten execution content.[^redaction]                                                                                           |

## Implemented enforcement, end to end

### 1. Claude Enterprise browser/app inference

This is a concrete non-MCP path, not just planned UI. Server startup wires the receiver to the shared scanner. The signed endpoint resolves a trusted organization/project binding; the service resolves the actor, persists the supplied transcript, then scans each known block. Any `block`, `warn`, or `quarantine` match denies the current inference. Here **warn has no acknowledgement flow**, and **quarantine does not persist a session freeze**.[^anthropic][^anthropic-binding]

Inputs include user/assistant text, tool-use arguments, tool-result text, and extracted attachment text, mapped to native policy message kinds. They are scanned **block by block**, not as one whole-conversation semantic judge request. Unknown/empty blocks and non-user/assistant roles are not policy inputs. Vendor payloads omit system prompts, tool definitions, hidden reasoning, and raw file/image bytes. An image-only secret therefore has no demonstrated inspection path.[^anthropic-input][^vendor-endpoint]

This is pre-inference, not pre-tool execution: a returned tool result may already reflect an external side effect. Nor does it block/redact a freshly generated final response; that response becomes visible only if a subsequent frame includes it. Archival appends by incoming message count and does not reconcile edits/compaction, which limits later evidence fidelity even though the current supplied frame is scanned.[^anthropic-history][^vendor]

Operationally, Gram has a 9-second outer timeout that returns an HTTP-200 deny, and processor/storage errors also become deny. The receiver accepts up to 10 MiB. However, **the shared scanner can swallow individual policy-scan errors**; a fail-closed receiver does not make every detector fail closed. Vendor transport failures also require Anthropic's **Block the request** failure posture.[^anthropic-errors][^scanner-errors][^vendor-config]

### 2. LiteLLM model calls

The pre-call receiver takes the latest user-message text, falling back to the last `Texts` entry, and returns no action for empty input. It creates an authenticated prompt event, disables warning acknowledgement for this path, and converts a hook deny to `BLOCKED`. String/text content blocks are accepted; earlier conversation, system content, images, and arbitrary structured fields are not scanned by this extraction.[^litellm]

The generated proxy configuration enables a generic guardrail pre/post hook, but that is only configuration evidence. Actual prevention still requires every intended request to traverse the proxy and the external guardrail to honor the response. Post-call code emits `assistant.responded`, captures output tool calls, discards the enforcement outcome, and returns `NONE` without replacement texts or streaming holdback. **Do not sell post-call ingestion as output blocking or streaming redaction.**[^litellm-config][^output]

### 3. Native tools / direct HTTP through a harness

Canonical `prompt.submitted` and `tool.requested` events invoke the scanner. The hook handler can deny, challenge, or open a session quarantine; the relay's prompt/tool handlers return host-specific rejection. Post-tool events simply send telemetry and return observed. Claude's official hook contract confirms that `PreToolUse` can block before execution and `UserPromptSubmit` precedes processing.[^native][^claude-hooks]

This can prevent a shell command containing sensitive text or a tool call whose JSON arguments contain restricted data. It is **not evidence that `curl --data @file`, dynamically generated request bodies, or every network request from that process is inspected**: enforcement sees the hook's serialized input. Those scenarios need explicit integration tests or a separate network/endpoint control.[^native]

## How flexible are policies?

- **Detectors:** built-in Gitleaks secrets and Presidio entities; choose entity types, disable individual rule IDs, and set Presidio confidence. Default confidence is 0.5; the Go client treats zero/out-of-range as unset. Other risk categories exist, but destructive-tool, CLI-destructive, and account-identity sources are flag-only—not blocking DLP controls.[^detectors]
- **Custom rules:** project-selected CEL predicates, including RE2 regex, case-insensitive literal matching, exact/prefix/suffix/glob matching, and JSON-path access. Rules can inspect content, user/assistant text, tool results, and correlated tool-call name/server/function/arguments. The runtime loads/evaluates selected rules; legacy regex is wrapped as CEL. This supports structured business identifiers and combinations of tool and content predicates, not an arbitrary installed detector plugin interface.[^custom]
- **Natural-language policies:** `prompt_based` policies run an LLM judge for each in-scope message/block. They are feature-gated in real time. Configurable temperature and `fail_open` exist; default is fail-open, and an old `model` key is ignored rather than selecting a model. Judge errors can produce a match when `fail_open=false`. Evaluate latency, false positives, and sensitive-data handling before using this for critical blocking.[^judge]
- **Scoping:** project policies; everyone or targeted user/role principals; message kinds (`user_message`, `assistant_message`, `tool_request`, `tool_response`, `prompt_attachment`); policy-level CEL include/exempt; per-category detection scopes; per-rule disabling; policy/global finding exclusions. These filters are evaluated in the scanner, not merely displayed in the UI.[^scope]
- **Important default:** recommended scopes exclude assistant free text for secrets, PII/financial/government/healthcare, and prompt policies. “All message types” alone does not mean all content is scanned; deliberately override category scope if response-history detection is required.[^recommended]
- **Limits on scoping claims:** the CEL environment exposes message/tool data, not first-class model/provider/browser-domain/department/time-of-day variables. Real-time policy audience evaluation supplies empty server URL/identity dimensions. You can match a URL present in tool arguments; that is not destination-aware network enforcement. User/role targeting is not proof of arbitrary department predicates.[^custom][^scope]
- **Actions:** `flag` records findings rather than entering the enforcing-policy query. Enforcing matches prioritize quarantine > block > warn in the shared scanner, but the receiver determines actual action semantics. A scanner enum alone is not proof every surface supports interactive warnings or persistent quarantine.[^actions][^anthropic]

## Exceptions, audit, and failure limits

**Exceptions are powerful and surface-dependent.** Native warning acknowledgement uses an expiring challenge and a five-minute retry grace, matched to user/policy/tool/call fingerprint. Backend authorization supports whole-policy and Shadow-MCP-server bypass targets, defaulting to no bypass if grants cannot load; approval requests have audited creation. Do not assume a bypass grant or warning retry works identically in the direct Anthropic receiver, which calls the scanner directly and has no acknowledgement protocol. Its documented identity fallback preserves a previously resolved actor, while never-known actors receive organization-wide policies.[^exceptions][^anthropic-history]

**Evidence is not redaction.** The findings consumer stores partial-mask displays and one-way fingerprints rather than raw matched values in its analytical store. Raw source content remains available through retained transcripts/anchors; unmasking reconstructs it and requires a successful audit write. Policy create/update and bypass-request creation also have audit calls. Thus “masked findings” must not be presented as “the secret never left the client” or “the secret was removed from stored transcripts.”[^evidence]

**Do not promise uniform fail-closed DLP:**

- Native hook scanner errors return no risk deny; missing scanner/context can skip scanning. Individual shared policy-scan errors are logged and discarded, and broken custom rules are dropped while built-ins continue.[^scanner-errors]
- Relay transport posture is separate: never-authenticated state allows; broken/reauth tool state gates; unreachable/5xx may allow with cached organization opt-in. Cached posture expires after 14 days; the synchronous exchange budget is five seconds. Successful server allow-after-scan-error is still an allow.[^relay-failure]
- The Go Presidio client truncates text above 50 KiB at a UTF-8 boundary. This is not an exhaustive size-limit survey of every async/enforcement implementation, but it is enough to rule out a blanket whole-payload inspection guarantee.[^presidio-limit]
- Background analysis scans persisted content and writes/publishes findings; it does not undo a completed inference/tool call. Async Pub/Sub work can also be used inside a synchronously awaited enforcement path, so queue transport alone does not determine whether prevention is inline.[^batch][^scanner-errors]

## Recommended near-term setup

1. **Separate prevention objectives:** pre-model use, pre-provider disclosure, and external exfiltration. Use Claude Enterprise hooks for the first, not the second; separately mandate approved model routing and endpoint/network controls where required.
2. **For governed Claude:** connect and verify the signing setup; identify the bound Gram project and actor attribution. Pilot with vendor shadow mode, then enable verdict enforcement, 100% rollout, no unintended excluded roles, **Block the request**, and the documented 10-second timeout. Confirm that these controls are actually active; a successful test probe or healthy-looking vendor panel is insufficient.[^anthropic-binding][^vendor-config]
3. **Start narrow:** deterministic secret rules plus the required high-confidence PII entities and specific custom identifiers. Use `flag` to measure noise, then `block` for high-confidence violations. Reserve semantic judges and native `warn` for cases where uncertainty or human override is acceptable. Review assistant-message category exemptions explicitly.
4. **For model APIs:** configure the LiteLLM pre-call guardrail and enforce routing outside Gram. Treat current latest-user-text coverage as a documented gap, not full-request protection. For native tools, centrally deploy gated hooks and test exact shell/file/upload behaviors before claiming coverage.
5. **Acceptance tests:** synthetic allowed/denied identifiers in each supported surface; attachment extracted text versus images; multi-turn/system/output-only content; tool arguments versus referenced file contents; unidentified actors; role/exclusion/bypass behavior; oversized payloads; scanner outage versus endpoint outage; output streaming. Check that the upstream operation did not execute, not just that a finding appeared.
6. **Operational review:** restrict policy/exclusion/bypass administration; monitor scans/errors, verdicts, capture completeness, and audited unmasking. Review privacy/retention of archived raw transcripts and the judge's processing path.

### Remaining questions / gaps

- Which “non-MCP” clients and deployment routes are actually in use? Which are mandatory versus user-removable?
- Is the requirement to keep data away from the model, away from its provider, or away from arbitrary destinations?
- Are Claude Enterprise hooks available/enforced for the intended tenant, without sampling/exclusions? Are provider API calls separately governed?
- Must DLP cover raw images/files, complete request history/system content, newly generated outputs, or actual egress bytes? Those exceed one or more verified paths above.
- What failure posture, maximum latency, retention, and exception authority are acceptable? Which scanner errors must become hard denies?
- Does the deployed build match this source? No live enforcement, external LiteLLM behavior, or comprehensive scanner-limit test was performed.

## Sources

[^anthropic]: [Server wiring](../../server/cmd/gram/start.go#L1515-L1519); [receiver processing and action translation](../../server/internal/anthropicinference/service.go#L47-L102); [receiver semantics](../../server/internal/anthropicinference/README.md#L68-L80).

[^anthropic-binding]: [Signed integration setup and trusted binding](../../server/internal/anthropicinference/README.md#L9-L42); [receiver route/config resolution](../../server/internal/anthropicinference/handler.go#L75-L105).

[^anthropic-input]: [Actual block-to-policy mapping](../../server/internal/anthropicinference/service.go#L112-L149).

[^anthropic-history]: [History, actor attribution, append semantics, and action limits](../../server/internal/anthropicinference/README.md#L53-L80).

[^anthropic-errors]: [Nine-second deny fallback](../../server/internal/anthropicinference/handler.go#L58-L105); [processor-error response](../../server/internal/anthropicinference/handler.go#L140-L160); [body ceiling](../../server/internal/anthropicinference/protocol.go#L19-L21).

[^vendor]: Anthropic, [Inference hooks: operation, limitations, availability](https://platform.claude.com/docs/en/manage-claude/inference-hooks).

[^vendor-config]: Anthropic, [Configuration: enforcement states, sampling, role exclusions, failure handling, and monitoring](https://platform.claude.com/docs/en/manage-claude/inference-hooks-configuration).

[^vendor-endpoint]: Anthropic, [Endpoint contract: transcript blocks, omitted content, and verdicts](https://platform.claude.com/docs/en/manage-claude/inference-hooks-endpoint).

[^litellm]: [Input selection](../../server/internal/litellm/impl.go#L145-L152); [hook call and BLOCKED result](../../server/internal/litellm/impl.go#L213-L235); [latest-user/text-only extraction](../../server/internal/litellm/impl.go#L376-L410).

[^litellm-config]: [Generated external guardrail configuration—not runtime proof](../../client/dashboard/src/pages/org/litellm-config.ts#L18-L46).

[^native]: [Prompt/tool scan inputs](../../server/internal/hooks/risk_scan.go#L38-L64); [prompt enforcement](../../server/internal/hooks/ingest_hooks.go#L583-L606); [native tool enforcement](../../server/internal/hooks/ingest_hooks.go#L668-L685); [relay gating versus post-tool observation](../../hooks/relay/runner.go#L340-L399).

[^claude-hooks]: Anthropic, [Claude Code hook lifecycle and event-specific blocking semantics](https://code.claude.com/docs/en/hooks).

[^output]: [LiteLLM response ingestion and NONE return](../../server/internal/litellm/impl.go#L324-L373); [post-tool observation](../../hooks/relay/runner.go#L394-L399); [later-frame response capture](../../server/internal/anthropicinference/README.md#L53-L56).

[^redaction]: [Validated action set](../../server/internal/risk/policycore/validation.go#L45-L51); [no replacement fields](../../server/internal/litellm/impl.go#L365-L373); [masked evidence contract](../../server/internal/risk/finding_ch.go#L31-L35); Anthropic [allow/deny only, no rewriting](https://platform.claude.com/docs/en/manage-claude/inference-hooks#current-limitations).

[^detectors]: [Source/action validation](../../server/internal/risk/policycore/validation.go#L54-L81); [entity/threshold API](../../server/design/risk/design.go#L29-L39); [actual Gitleaks/Presidio calls](../../server/internal/risk/scanner.go#L755-L810); [threshold defaults](../../server/internal/background/activities/risk_analysis/presidio.go#L123-L137).

[^custom]: [CEL fields and matchers](../../server/internal/risk/celenv/reference.go#L49-L73); [rule loading and legacy regex](../../server/internal/risk/customrules/customrules.go#L14-L74); [runtime custom scan](../../server/internal/risk/scanner.go#L718-L732); [custom findings enforce](../../server/internal/risk/scanner.go#L882-L901).

[^judge]: [Real-time feature gate and judge call](../../server/internal/risk/scanner.go#L923-L941); [config semantics](../../server/internal/scanners/promptpolicy/judge.go#L92-L121); [fail-open/closed findings](../../server/internal/scanners/promptpolicy/scanner.go#L34-L71).

[^scope]: [API policy scopes/audience](../../server/design/risk/design.go#L39-L53); [user/role audience validation](../../server/internal/risk/policy_audience.go#L26-L54); [runtime audience/message/scope selection](../../server/internal/risk/scanner.go#L414-L458); [category scopes, exclusions, disabled rules](../../server/internal/risk/scanner.go#L680-L715).

[^recommended]: [Default category exemptions](../../server/internal/risk/recommendedscopes/registry.go#L20-L87).

[^actions]: [Enforcing-policy query](../../server/internal/risk/queries.sql#L1446-L1454); [action priority](../../server/internal/risk/scanner.go#L489-L530).

[^exceptions]: [Warning grace and fingerprint-bound acknowledgement](../../server/internal/risk/policy_challenge.go#L23-L65); [challenge link lifetime](../../server/internal/risk/policy_ack_token.go#L50-L56); [bypass target types](../../server/internal/risk/policy_bypass_evaluator.go#L25-L65); [authorization and deny-by-default loading](../../server/internal/risk/policy_bypass_evaluator.go#L94-L135); [request audit](../../server/internal/risk/policy_bypass.go#L237-L242).

[^evidence]: [Finding storage contract](../../server/internal/risk/finding_ch.go#L31-L35); [reconstruction and audited unmask](../../server/internal/risk/unmask_ch.go#L63-L90); [policy mutation audit](../../server/internal/risk/policy_mutation.go#L35-L65); [bypass audit](../../server/internal/risk/policy_bypass.go#L237-L242).

[^scanner-errors]: [Hook skip/error behavior](../../server/internal/hooks/risk_scan.go#L66-L83), [hook fail-open return](../../server/internal/hooks/risk_scan.go#L119-L129); [per-policy error dropping](../../server/internal/risk/scanner.go#L493-L506); [custom-rule errors](../../server/internal/risk/scanner.go#L724-L732); [real-time inline/PubSub detector selection](../../server/internal/risk/scanner.go#L755-L810).

[^relay-failure]: [Authentication/transport decisions](../../hooks/relay/runner.go#L274-L325); [prompt reauthentication exception](../../hooks/relay/runner.go#L340-L353); [cached posture expiry](../../hooks/relay/orgsettings.go#L29-L75); [exchange budgets](../../hooks/relay/client.go#L23-L35).

[^presidio-limit]: [Go Presidio truncation ceiling](../../server/internal/background/activities/risk_analysis/presidio.go#L139-L154).

[^batch]: [Background scope/scanning and finding writes](../../server/internal/background/activities/risk_analysis/analyze_batch.go#L273-L333); [persisted inference messages enter analysis](../../server/internal/anthropicinference/README.md#L73-L80).
