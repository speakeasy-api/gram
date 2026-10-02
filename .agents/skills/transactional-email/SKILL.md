---
name: transactional-email
description: Use when adding, changing, restyling, reviewing, validating, or previewing a Gram/Speakeasy transactional email, in Go or in LMX/MJML — a `template_<name>.go`, a `TemplateKey` constant, a `RegisteredTemplates` entry, a `Variables()` map, or `AddToAudience()`; calling `emailSvc.Send`/`SendIdempotent`; wiring `email.NewService` or `loops.New`; testing sends with a capture client; editing `.lmx`/`.mjml` layout, `loops/manifest.json`, email copy, subject/preview text, or brand chrome; taking email screenshots or desktop/mobile QA; or any file under `server/internal/email/` or `server/internal/thirdparty/loops/`. Also use for "email renders blank fields", "camelCase variable", `ErrUnregisteredTemplate`, `ErrUnknownTemplateKey`, "no Loops ID for template", or `TestManifestMatchesApplicationTemplateContract` failures.
metadata:
  relevant_files:
    - "server/internal/email/**/*.go"
    - "server/internal/email/loops/**"
    - "server/internal/thirdparty/loops/**/*.go"
---

# Transactional email

Transactional email goes through `server/internal/email`, a typed facade over the Loops client in `server/internal/thirdparty/loops`. Feature code depends on a consumer-owned interface implemented by `*email.Service`, and on concrete `email.Template` values; it never calls Loops directly and never holds a Loops transactional ID. Each template is one snake_case logical key shared by the Go struct, `server/internal/email/loops/manifest.json`, and `loops/<key>.lmx`; the Go `Variables()` keys and the manifest `variables` list must match exactly.

Release CI creates the Loops emails from the manifest; the deployment injects the key-to-ID JSON map, parsed by `email.ParseTemplateIDs`. No real provider ID belongs in source; synthetic IDs in tests are fine.

## Authorization and confidentiality

- Repository edits do not authorize manual Loops reads, mutations, previews, test-sends, or publishes. Resolve each external action separately.
- Never use customer or private production names, IDs, domains, addresses, URLs, or figures in source, tests, temporary previews, screenshots, logs, or test sends. Use `Example Organization`, `person@example.com`, and `<ORG_ID>`.
- Keep content operational. No marketing copy or unsubscribe UI.
- Recipient-visible copy names the product **Speakeasy**, never "Gram": subjects, preview text, body copy, CTA labels, image alt text, and footer reasons. Internal identifiers keep their existing names (logical keys such as `access_paused`, file names); only managed names carry the `gram.transactional.v2.<key>` prefix.

## Adding or changing a template

First check the `TemplateKey` constants in `server/internal/email/templates.go` for an existing template covering the event (for example `trial_ending_soon`, `access_paused`). Also check the send sites (`git grep -n 'email\.[A-Z][A-Za-z]*{' server/internal`): one template may serve several events, for example `access_paused` is also sent on trial demotion (`billingnotifications.AccessPausedTrialDemotion`). Extend an existing template (copy change or a condition variable) instead of adding a near-duplicate. What a change touches:

| Change                            | Update together                                                                                                 |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| New template                      | Every surface in the list below, in one change                                                                  |
| Add, rename, or remove a variable | The Go struct, `Variables()`, the manifest `variables` (and `unused_variables`), the LMX, and the template test |
| Subject or preview text           | The manifest entry                                                                                              |
| Body copy or layout               | The LMX, then a fresh [preview](#preview-screenshots)                                                           |

A new template needs all of these in one change:

1. `templates.go`: add a `TemplateKey` constant, and append a fully initialized zero value to `RegisteredTemplates` (the manifest contract test and `TemplateIDs.ValidateRegistered` iterate this list).
2. `template_<key>.go`: a struct whose exported fields are the variables, implementing `Key()`, `Variables()`, and `AddToAudience()`.
3. `template_<key>_test.go`: assert `Key()`, the complete `Variables()` map, and `AddToAudience()` (see `template_access_paused_test.go`).
4. `server/internal/email/loops/<key>.lmx`: copy `transactional_base.lmx` from the same directory and specialize it (see LMX rules below).
5. `loops/manifest.json`: add one object under `templates`; preserve `version`, `defaults`, and every existing template.

```json
"example_notice": {
  "managed_name": "gram.transactional.v2.example_notice",
  "subject": "Action required for {data.resource_name}",
  "preview_text": "Review the requested change.",
  "source": "example_notice.lmx",
  "variables": ["resource_name", "action_url"]
}
```

New managed names are `gram.transactional.v2.<key>`. The sender identity lives only in the manifest `defaults` (`from_name: "Speakeasy"`, `from_email: "platform"`, `reply_to_email: "platform@speakeasy.com"`); never override it per template. A declared variable that the subject, preview, and LMX intentionally never reference (including `if=` conditions) must also be listed in the entry's `unused_variables`, or manifest validation fails with `declares unused variable`.

`TestManifestMatchesApplicationTemplateContract` (`loops/manifest_contract_test.go`) requires one manifest entry per `RegisteredTemplates` entry, matching variable lists, exactly one canonical lockup and gradient `<Image>` in the LMX (matched as the full tag string, so copy both lines from `transactional_base.lmx` verbatim), and no "Gram" in subject, preview text, or LMX.

### `Variables()` keys

Return **snake_case** keys. Loops substitutes them directly into `{data.<key>}`; camelCase keys silently render as blank fields in the delivered email. Return every declared key, even when the value is empty, including condition-only variables.

```go
func (t MyTemplate) Variables() map[string]string {
    return map[string]string{
        "approval_url":    t.ApprovalURL,    // not "approvalUrl"
        "requester_email": t.RequesterEmail, // not "requesterEmail"
    }
}
```

Never send a blank field that produces broken copy. Apply a fallback with `conv.Default` where the caller builds the struct, not in `Variables()` or the LMX (from `background/activities/weekly_usage_summary.go`):

```go
tmpl := email.WeeklyUsageSummary{
    OrganizationName: conv.Default(target.OrganizationName, "your organization"),
    // ...
}
```

### `AddToAudience()`

Controls whether Loops upserts the recipient as a contact when the email is sent. Default to `false` (operational, billing, and admin alerts, one-off or incidental recipients). Return `true` only when the event deliberately enrolls a known user in the Speakeasy audience, matching `TeamInvite` or onboarding semantics.

## Sending

Feature packages depend on a narrow interface declared at the consumer (see `billingnotifications.Sender` in `server/internal/billingnotifications/service.go`, `organizations.EmailSender` in `server/internal/organizations/impl.go`); `*email.Service` satisfies both.

Send with `Send(ctx, recipient, tmpl)`. Use `SendIdempotent(ctx, recipient, idempotencyKey, tmpl)` when retries (Temporal activities, sweeps) could send twice. The key must be stable for the same event and recipient across retries, and distinct for an intentional resend. Loops dedupes it for 24 hours only and caps it at 100 characters, so hash long inputs; the billing path does this with `billingnotifications.RecipientIdempotencyKey(recipient, parts...)` (a 64-character sha256 hex digest). Keep one key helper: if a non-billing caller needs one, generalize it into `email` in that change rather than adding a second helper beside it; moving the billing callers onto it may be a follow-up.

`email.NewService(logger, sender loops.Client, ids email.TemplateIDs, enabled bool)`: an empty recipient returns `email.ErrEmptyRecipient` even when disabled. With a nonempty recipient, a disabled service returns nil before template resolution or any provider call; an enabled one returns `email.ErrUnregisteredTemplate` for a key with no ID in `ids`. Never pass a nil `*email.Service`: production wiring (`newEmailService` in `server/cmd/gram/deps.go`) already builds a disabled service when Loops is unconfigured, and tests use the no-op service in [Go tests](#go-tests). Some existing callers still guard against nil; do not add new nil guards.

## Go tests

No-op service (the code under test sends, but the test does not assert on it):

```go
loopsClient := loops.New(t.Context(), testenv.NewLogger(t), nil, "") // empty key returns a noop client; nil guardian policy is safe
emailSvc := email.NewService(testenv.NewLogger(t), loopsClient, email.NewTemplateIDs(nil), false)
```

Asserting on sends: reuse the test double already in the current package if it can record the payload, including an existing `testify/mock` client (record through `.Run`); do not add a second double beside it. If the package has none, add a small local capture of `loops.Client`, a one-method interface we own, rather than a new `testify/mock`. Do not copy or move doubles from other packages for a narrow change. Build the service with `enabled` true and an ID for each template under test, or nothing reaches the client:

```go
type captureLoopsClient struct {
    mu   sync.Mutex
    sent []loops.SendTransactionalInput
}

func (c *captureLoopsClient) SendTransactional(_ context.Context, input loops.SendTransactionalInput) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.sent = append(c.sent, input)
    return nil
}

func (c *captureLoopsClient) Sent() []loops.SendTransactionalInput {
    c.mu.Lock()
    defer c.mu.Unlock()
    return append([]loops.SendTransactionalInput(nil), c.sent...)
}

captured := &captureLoopsClient{mu: sync.Mutex{}, sent: nil}
emailSvc := email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
    "access_request": "access-request-test-id",
}), true)
```

`require.NoError` on the send, then assert the whole payload with one `require.Equal` so a missing or renamed variable fails:

```go
require.Equal(t, []loops.SendTransactionalInput{{
    TransactionalID: "access-request-test-id",
    Email:           "person@example.com",
    DataVariables:   map[string]string{"requester_name": "Example User" /* every key */},
    AddToAudience:   false,
    IdempotencyKey:  "",
}}, captured.Sent())
```

Existing captures to learn from: `server/internal/access/setup_test.go` (`recordingEmailSender`) and `server/internal/background/activities/setup_test.go` (`captureLoopsClient`, with failure injection). When the feature package sends through its own consumer interface, its tests can capture at that interface and type-assert the template instead (`captureSender` in `server/internal/billingnotifications/service_test.go`).

## LMX rules

`loops/transactional_base.mjml` is the approved visual specification; `loops/transactional_base.lmx` is its production translation and the only shell to copy (Loops templates do not inherit a parent). Preserve the Speakeasy lockup header, light canvas, uppercase gray eyebrow, RGB gradient line under the headline, square black CTA, and closing footer (hairline divider, then the 12px gray footer reason on the white body). Transactional emails end there: no gray band, no black "Connect. Secure. Control. Observe." banner (`cmshjs1x705gp0jy23gmy6s06.png`), no dark masthead, neon accent, rounded corners, CSS gradients, or substitute palette. Never load webfonts (Gmail and Outlook strip `@font-face`); live text is Helvetica/Arial and the brand fonts appear only inside baked images.

- Use `{data.variable_name}` everywhere (case-sensitive), never `{DATA_VARIABLE:...}`. Reference exactly the variables in the Go contract, not the starter's generic chrome variables.
- Keep labels, headline fragments, body copy, CTA labels, and footer reasons static unless they genuinely vary at send time.
- State the event directly; delete vague lead-ins. One verb-led CTA in sentence case ("Review access request"), no arrows or uppercase.
- Delete the detail block or CTA when not needed; never render empty chrome. Use conditional sections for variants, e.g. `<Section if="{data.exhausted}" ifOperation="equal" ifValue="true">` (see `openrouter_chat_credits_threshold.lmx`); do not invent fallback syntax.
- LMX cannot embed raw HTML. Send scalar variables and compose the layout in LMX.
- `<Image src>` must be a Loops-hosted upload, never a repo-local or public URL. Copy the canonical assets unchanged:
  - Lockup (first block): `https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmt7eueee05e20izu0frdx1jq.png`, `width="160"`, `align="left"`. Hosted render of `assets/speakeasy-lockup-black.png`; never pair the isotype with live wordmark text.
  - Gradient line (directly under the headline, once per email): `https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmshilgvx01u30j6t4t0211tl.png`, `width="536"` in LMX (no width in MJML; it fills the 536px column).
  - Deprecated, never copy: the flat eight-block rail `cmsrzwke702cu0j3bz2gats4u.png` and the bare isotype `cmsrzv81y00z60i1dmtf9twha.png`. If a shell still has these, an Inter body font, or live wordmark text, update your copy to this spec and flag the shell for migration.
- `<Style>` gets Helvetica only through the "Speakeasy Trial" theme: keep `themeId="cmsnm620801ug0j2jdxiw79j4"` and do NOT set `bodyFontFamily`/`bodyFontCategory` (the LMX API rejects it: `"bodyFontFamily" is not a supported font family (got "Helvetica")`). Never substitute a Google look-alike (Inter, ABeeZee, Roboto). Keep every other attribute explicit as in the base shell.
- Grays are the brand set only: `#000000`, `#6E6E6E` (eyebrows, footer reason), `#979797`, `#DCDCDC`; links `#2873D7`. Status warnings may use the red set already in `openrouter_*_credits_threshold.lmx` (`#C83228`, `#FFF1ED`, `#E8A18D`); add no colors beyond these and the base shell's `#FAFAFA`/`#FFFFFF` backgrounds. Put details in the base shell's outlined `<Section>`; `<CodeBlock blockColor="#FAFAFA" fontSize="13">` is allowed for long identifiers but has no existing example, so preview it. Prose details stay in paragraphs.
- Loops accepts `Columns.gap` only from 12 through 150 and `Paragraph.fontSize` only from 12 through 64 when set; `manifest.go` validation range-checks every explicit value in every `.lmx` recursively, including unregistered bases. Never go below 12px text.

## Validate

Use repository tooling, never bare `go`, `npm`, or `npx`:

```bash
./server/internal/email/loops/sync.sh --validate-only
mise run test:server ./internal/email/...
mise lint:server
git diff --check
```

`--validate-only` only checks manifest structure, managed names, and that each source file exists. LMX well-formedness, attribute ranges, and declared vs. used variables are enforced by `loops.LoadManifest` (`manifest.go`), which runs in the Go tests along with the contract test. Run both.

## Preview screenshots

For every new template, layout change, or copy change that alters text length or content, preview a temporary MJML specialization of `transactional_base.mjml` with the same copy and sections and generic sample values. Earlier screenshots cannot show how changed copy wraps, so re-render rather than reuse them. These are design-spec previews, not renders of the production LMX. Keep them under `/tmp/<task>/` and do not commit them; `mise run playwright` may also write ignored artifacts under `.playwright-cli/`.

Copy the bundled lockup next to the preview HTML so the MJML's relative `src` resolves. `mise run playwright` depends on `ensure-stack`, so it wakes this worktree's dev stack; run `mise run pause` afterwards if it was paused.

```bash
mkdir -p /tmp/<task>
cp .agents/skills/transactional-email/assets/speakeasy-lockup-black.png /tmp/<task>/
aube dlx mjml --config.validationLevel strict /tmp/<task>/<key>.preview.mjml -o /tmp/<task>/<key>.preview.html
python3 -m http.server 8765 --bind 127.0.0.1 --directory /tmp/<task> >/tmp/<task>/preview-server.log 2>&1 &
preview_pid=$!
mise run playwright open http://127.0.0.1:8765/<key>.preview.html
mise run playwright resize 1100 900
mise run playwright screenshot --filename=/tmp/<task>/<key>-desktop.png --full-page --hires
mise run playwright resize 390 900
mise run playwright screenshot --filename=/tmp/<task>/<key>-mobile.png --full-page --hires
mise run playwright close
kill "$preview_pid"
```

Inspect both PNGs. Reject overflow, weak hierarchy, empty blocks, broken images, unresolved variables, non-placeholder identity, "Gram" in recipient-visible copy, or excess whitespace. Passing this is **local visual-spec QA** only. LMX has no local renderer, so report **production-render QA** as unverified unless a separately authorized Loops draft preview was inspected.

## Common mistakes

| Symptom                                                                          | Cause                                                                               | Check                                                     |
| -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | --------------------------------------------------------- |
| Blank fields in the delivered email, no error                                    | camelCase `Variables()` keys                                                        | Keys are snake_case and match the manifest `variables`    |
| Contract test fails `require.Len`, or startup fails with `ErrUnknownTemplateKey` | Missing `RegisteredTemplates` entry                                                 | The zero value is appended in `templates.go`              |
| A test captures nothing, or sends fail with `ErrUnregisteredTemplate`            | Service built with `enabled` false, or without the template's ID                    | `email.NewService(..., ids, true)` with an ID for the key |
| Incidental recipients become Loops contacts                                      | `AddToAudience()` returns `true` for an operational alert                           | Default to `false`                                        |
| Loops rejects the LMX, or the email carries a banner or foreign styling          | `bodyFontFamily` set, a closing banner added, or a shell derived from another email | Start from `transactional_base.lmx`                       |
