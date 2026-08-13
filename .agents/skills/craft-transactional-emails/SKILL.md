---
name: craft-transactional-emails
description: Use when creating, restyling, reviewing, validating, syncing, or previewing Gram/Speakeasy transactional emails in Loops, LMX, or MJML, including email templates, branded email, Loops Content API, email screenshots, desktop/mobile QA, logical template IDs, or files under server/internal/email/loops/.
---

# Craft transactional emails

LMX in `server/internal/email/loops/` is the production delivery source. The
inline MJML starter is the approved visual specification. Release CI creates or
updates Loops records and relays environment-specific IDs through gram-infra.

## Authorization and confidentiality

- Repository edits do not authorize manual Loops reads, mutations, previews,
  test-sends, or publishes. Resolve each external action separately.
- The checked-in gram-infra release workflow with `SYNC_LOOPS_EMAILS=true` is the
  approved automated publish path. Do not set it for local runs or call the live
  API while developing.
- Never use customer or private production names, IDs, domains, addresses,
  URLs, or figures in source, temporary previews, screenshots, logs, or test
  sends. Use `Example Organization`, `person@example.com`, and `<ORG_ID>`.
  The public Speakeasy font and favicon URLs in the approved starter are the
  only production-asset exception; copy them unchanged when using that starter.
- Keep transactional content operational. No marketing copy or unsubscribe UI.

## Discover the contract

1. Read `AGENTS.md` confidentiality rules.
2. Inspect `server/internal/email/template_<name>.go`, `templates.go`,
   `loops/manifest.json`, and the matching `.lmx` file.
3. For an existing email, reuse its logical key. For a new email, choose one
   snake-case key and use it unchanged in Go, manifest, managed name, and runtime
   lookup. Never add a variable only in LMX or only in Go.
4. Treat the inline MJML starter below as the approved visual source of truth.
   Preserve its spectrum rail, light canvas, Speakeasy wordmark, restrained mono
   labels, Tobias editorial headline, square black CTA, and dashed pale footer.
   Never change the starter to match a reduced implementation or invent a new
   palette.

## Author production LMX

New-template checklist:

1. Create `template_<key>.go`: define the typed fields; implement `Key()`,
   `Variables()`, and `AddToAudience()`.
2. Add a `TemplateKey` constant and the fully initialized zero value to
   `RegisteredTemplates` in `templates.go`.
3. Copy `loops/transactional_base.lmx` to `loops/<key>.lmx`, specialize the
   message, and replace every generic variable with the typed contract.
4. Add `manifest.json` metadata. This entry enrolls the template in merge-driven
   creation; no provider ID belongs in Gram.

`AddToAudience()` controls whether Loops also creates/updates a contact for the
recipient. Default to `false` for operational alerts and one-off recipients.
Use `true` only when the product event deliberately enrolls a known user in the
Speakeasy audience, matching `TeamInvite` or onboarding semantics; cover that
choice in a focused test.

Complete minimal `manifest.json` shape for a repository containing one
two-variable template:

```json
{
  "version": 1,
  "defaults": {
    "from_name": "Example Organization",
    "from_email": "example",
    "reply_to_email": "person@example.com"
  },
  "templates": {
    "example_notice": {
      "managed_name": "gram.transactional.v2.example_notice",
      "subject": "Action required for {data.resource_name}",
      "preview_text": "Review the requested change.",
      "source": "example_notice.lmx",
      "variables": ["resource_name", "action_url"]
    }
  }
}
```

In the existing repository manifest, preserve `version`, `defaults`, and all
existing templates; add only the new object under `templates`.
`from_email` is the sender local-part configured in Loops, so the neutral
`example` value is intentionally not a complete address.

Identity map:

| Identity     | Example                                | Owner              | Human edit?      |
| ------------ | -------------------------------------- | ------------------ | ---------------- |
| Logical key  | `example_notice`                       | Gram Go + manifest | Yes, when adding |
| Managed name | `gram.transactional.v2.example_notice` | Manifest/Loops     | Manifest only    |
| Remote ID    | `cm...`                                | Loops              | Never            |
| Key → ID map | `email-templates-dev.yaml`             | gram-infra CI      | Never            |

- New managed names are `gram.transactional.v2.<logical_key>`.
- Add the LMX file and a `manifest.json` entry with subject, preview, source, and
  the complete variable list. The command test enforces a one-to-one Go/manifest
  contract.
- Use `{data.variable_name}` everywhere; LMX names are case-sensitive. Never use
  `{DATA_VARIABLE:...}` in LMX.
- Loops accepts `Columns.gap` only from 12 through 150 and
  `Paragraph.fontSize` only from 12 through 64 when those attributes are set;
  omission uses Loops' accepted defaults. The local manifest validator enforces
  explicit values across every `.lmx` file recursively, including unregistered
  bases. Translate MJML 10px/11px labels to 12px in LMX; preserve their
  hierarchy, not prohibited literal sizes.
- State the event or action directly. Delete vague lead-ins such as “a clear read
  on how your organization is tracking.” Prefer one verb-led CTA.
- Use conditional `<Section>` blocks for variants. Do not invent fallback syntax.
- Keep condition-only variables in the Go and manifest contract. Loops omits
  variables used only by a Section `if` attribute from its published
  `dataVariables` metadata; repository reconciliation accounts for that provider
  behavior while continuing to verify every rendered variable exactly.
- LMX cannot embed raw HTML. Send scalar variables and compose the layout in LMX.
- `<Image src>` must be a Loops-hosted upload. Do not use a repo-local or public
  URL as `src`; use a text masthead until an upload is deliberately managed.
- Loops templates do not inherit a parent. Copy the starter into each `.lmx` and
  specialize its message content.
- Keep labels, headline fragments, body copy, CTA labels, and footer reasons
  static unless they genuinely vary at send time. A two-variable Go contract
  means the LMX may reference exactly those two variables—not generic chrome
  variables from either starter.

### Approved LMX fallback

`transactional_base.lmx` is the only production shell. Copy it; do not derive a
new shell from another email. LMX cannot express the MJML’s raw spectrum table,
hosted ABCDiatype/Tobias font faces, or public favicon URL. Until those assets are
explicitly managed through Loops, the approved fallback is:

- omit the spectrum rail rather than replace it with a new accent;
- use the text-only `speakeasy` / `AI CONTROL PLANE` header;
- use Inter with the declared UI sans fallback while retaining the approved
  hierarchy, light palette, square black CTA, panels, and pale footer; translate
  label text below 12px to Loops' 12px minimum.

Those are constrained delivery fallbacks, not a second design. Do not add a dark
masthead, neon accent, rounded cards, gradient, or substitute palette.

## Inline transactional mail starter

```mjml
<mjml>
  <mj-head>
    <mj-title>{DATA_VARIABLE:email_title}</mj-title>
    <mj-preview>{DATA_VARIABLE:email_preview}</mj-preview>

    <mj-attributes>
      <mj-all font-family="ABCDiatype, Arial, Helvetica, sans-serif" />
      <mj-body background-color="#fafafa" width="640px" />
      <mj-section background-color="#ffffff" padding="0" />
      <mj-text
        color="#121212"
        font-size="16px"
        line-height="1.65"
        padding="0"
      />
      <mj-button
        background-color="#121212"
        border-radius="0"
        color="#ffffff"
        font-family="ABCDiatypeMono, 'Courier New', monospace"
        font-size="13px"
        font-weight="400"
        inner-padding="14px 22px"
        letter-spacing="0.08em"
        padding="0"
        text-transform="uppercase"
      />
    </mj-attributes>

    <mj-style>
      @font-face {
        font-family: "ABCDiatype";
        font-style: normal;
        font-weight: 400;
        src: url("https://www.speakeasy.com/fonts/diatype/ABCDiatype-Regular.woff2")
          format("woff2");
      }
      @font-face {
        font-family: "ABCDiatypeMono";
        font-style: normal;
        font-weight: 400;
        src: url("https://www.speakeasy.com/fonts/diatype-mono/ABCDiatypeMono-Regular.woff2")
          format("woff2");
      }
      @font-face {
        font-family: "Tobias";
        font-style: normal;
        font-weight: 100;
        src: url("https://www.speakeasy.com/fonts/tobias/Tobias-Thin.woff2")
          format("woff2");
      }

      .display-copy {
        font-family: Tobias, Georgia, "Times New Roman", serif !important;
        font-weight: 100 !important;
        letter-spacing: -0.035em !important;
      }

      .eyebrow {
        font-family: ABCDiatypeMono, "Courier New", monospace !important;
        letter-spacing: 0.1em !important;
        text-transform: uppercase !important;
      }

      @media only screen and (max-width: 480px) {
        .mobile-pad > table > tbody > tr > td {
          padding-left: 24px !important;
          padding-right: 24px !important;
        }
      }
    </mj-style>
  </mj-head>

  <mj-body>
    <mj-raw>
      <table width="100%" cellpadding="0" cellspacing="0" role="presentation"
      style="border-collapse:collapse;background:#ffffff;"> <tr> <td
      width="12.5%" height="5" bgcolor="#330f1f"
      style="font-size:0;line-height:0;">&nbsp;</td> <td width="12.5%"
      height="5" bgcolor="#c83228"
      style="font-size:0;line-height:0;">&nbsp;</td> <td width="12.5%"
      height="5" bgcolor="#fb8841"
      style="font-size:0;line-height:0;">&nbsp;</td> <td width="12.5%"
      height="5" bgcolor="#d3dd92"
      style="font-size:0;line-height:0;">&nbsp;</td> <td width="12.5%"
      height="5" bgcolor="#59824f"
      style="font-size:0;line-height:0;">&nbsp;</td> <td width="12.5%"
      height="5" bgcolor="#002414"
      style="font-size:0;line-height:0;">&nbsp;</td> <td width="12.5%"
      height="5" bgcolor="#00143d"
      style="font-size:0;line-height:0;">&nbsp;</td> <td width="12.5%"
      height="5" bgcolor="#2874d7"
      style="font-size:0;line-height:0;">&nbsp;</td> </tr> </table>
    </mj-raw>

    <mj-section css-class="mobile-pad" padding="26px 40px 24px">
      <mj-column>
        <mj-raw>
          <table width="100%" cellpadding="0" cellspacing="0"
          role="presentation" style="border-collapse:collapse;"> <tr> <td
          width="36" valign="middle"> <img
          src="https://www.speakeasy.com/favicon-96x96-light.png" width="28"
          height="28" alt=""
          style="display:block;border:0;outline:none;text-decoration:none;" />
          </td> <td valign="middle"
          style="color:#121212;font-family:ABCDiatype,Arial,Helvetica,sans-serif;font-size:21px;line-height:1;font-weight:400;">
          speakeasy </td> <td align="right" valign="middle"
          style="color:#757575;font-family:ABCDiatypeMono,'Courier
          New',monospace;font-size:10px;line-height:1.4;letter-spacing:0.1em;text-transform:uppercase;">
          AI control plane </td> </tr> </table>
        </mj-raw>
      </mj-column>
    </mj-section>

    <!-- MESSAGE SLOT: replace or extend only this slot for event-specific data. -->
    <mj-section css-class="mobile-pad" padding="44px 40px 26px">
      <mj-column>
        <mj-text
          css-class="eyebrow"
          color="#757575"
          font-size="11px"
          line-height="1.4"
          padding="0 0 18px"
        >
          {DATA_VARIABLE:email_eyebrow}
        </mj-text>
        <mj-text
          css-class="display-copy"
          font-size="44px"
          line-height="1.12"
          padding="0"
        >
          {DATA_VARIABLE:email_headline}
        </mj-text>
      </mj-column>
    </mj-section>

    <mj-section css-class="mobile-pad" padding="0 40px 28px">
      <mj-column>
        <mj-text
          color="#545454"
          font-size="16px"
          line-height="1.65"
          padding="0"
        >
          {DATA_VARIABLE:email_body}
        </mj-text>
      </mj-column>
    </mj-section>

    <!-- Optional detail block: delete when the message has no key detail. -->
    <mj-section css-class="mobile-pad" padding="0 40px 32px">
      <mj-column border="1px solid #dbdbdb" padding="22px 24px 20px">
        <mj-text
          css-class="eyebrow"
          color="#757575"
          font-size="10px"
          line-height="1.4"
          padding="0 0 8px"
        >
          {DATA_VARIABLE:detail_label}
        </mj-text>
        <mj-text
          color="#121212"
          font-family="ABCDiatypeMono, 'Courier New', monospace"
          font-size="14px"
          line-height="1.55"
          padding="0"
        >
          {DATA_VARIABLE:detail_value}
        </mj-text>
      </mj-column>
    </mj-section>

    <!-- Optional CTA: delete when the recipient has no useful next action. -->
    <mj-section css-class="mobile-pad" padding="0 40px 48px">
      <mj-column>
        <mj-button align="left" href="{DATA_VARIABLE:action_url}">
          {DATA_VARIABLE:action_label}&nbsp;&nbsp;→
        </mj-button>
      </mj-column>
    </mj-section>

    <mj-section
      css-class="mobile-pad"
      background-color="#fafafa"
      border-top="1px dashed #bababa"
      padding="24px 40px 30px"
    >
      <mj-column>
        <mj-text
          css-class="eyebrow"
          color="#757575"
          font-size="10px"
          line-height="1.6"
          padding="0 0 8px"
        >
          Speakeasy · AI control plane
        </mj-text>
        <mj-text color="#757575" font-size="12px" line-height="1.6" padding="0">
          {DATA_VARIABLE:footer_reason}
        </mj-text>
      </mj-column>
    </mj-section>
  </mj-body>
</mjml>
```

Copy this starter without redesigning it. Delete the detail section or button
when the message does not need it; never render empty chrome. Replace all
starter variables with the real typed contract. Production LMX must translate
this design using documented LMX primitives; it must not become a competing
visual direction.

## Validate

Run repository tooling, never bare `go`, `npm`, or `npx`:

```bash
mise exec -- go run ./server/cmd/sync-loops-email-templates --validate-only
mise exec -- go test ./server/internal/email/... ./server/cmd/sync-loops-email-templates
mise lint:server
git diff --check
```

This checks manifest structure, XML well-formedness and explicit provider
attribute ranges across every `.lmx` file recursively, declared/used variables,
Go contracts, API reconciliation behavior, and runtime ID resolution. Merge CI
then uses Loops compilation, optimistic revisions, Guardian, and publish; any
Guardian error blocks the release preparation.

## Screenshots and visual QA

For every new template or layout change, create a temporary MJML specialization
from the approved `transactional_base.mjml`. It is the canonical visual—not a
proxy derived from LMX. Use the same copy/sections and generic sample values.
Do not commit the proxy. A copy-only change using an already-reviewed
layout may reuse existing shell references in `.playwright-cli/email-previews/`.
If repo artifact writes are not authorized, keep everything under `/tmp/<task>/`.

```bash
aube dlx mjml --config.validationLevel strict /tmp/<key>.preview.mjml -o /tmp/<key>.preview.html
python3 -m http.server 8765 --bind 127.0.0.1 --directory /tmp >/tmp/<key>-preview-server.log 2>&1 &
preview_pid=$!
mise run playwright open http://127.0.0.1:8765/<key>.preview.html
mise run playwright resize 1100 900
mise run playwright screenshot --filename=/tmp/<key>-desktop.png --full-page --hires
mise run playwright resize 390 900
mise run playwright screenshot --filename=/tmp/<key>-mobile.png --full-page --hires
mise run playwright close
kill "$preview_pid"
```

This is **local visual-spec QA**. Inspect both PNGs with the image viewer. Reject overflow, weak hierarchy, empty
blocks, broken images, unresolved variables, non-placeholder identity, or excess
whitespace. MJML is a visual proxy, not proof that production LMX is identical.
Passing local QA means the proxy compiles and both screenshots pass inspection;
this satisfies repository-source QA. Report **production-render QA** as
unverified unless a separately authorized Loops preview was also inspected.

LMX has no local renderer. Production-render QA requires a separately authorized
Loops draft preview; if unavailable, report it as incomplete. Guardian proves
semantic validity, not Gmail/Outlook appearance.

## CI and runtime behavior

A push to Gram `main` runs `.github/workflows/pr.yaml`, which dispatches
gram-infra’s `Prepare Release`. The checked-out manifest is sufficient to trigger
create/update, Guardian, publish, ID-overlay generation, and the release PR.

Delivery timeline:

1. Gram `main` dispatches a dev prepare after its CI and images pass.
2. Prepare reconciles/publishes in dev and opens/updates the gram-infra dev
   release PR with `email-templates-dev.yaml`.
3. Merging that PR lets ArgoCD deploy the image and ID map to dev GKE.
4. The merge triggers promotion. Each target prepare independently reconciles
   its Loops environment, writes that environment's IDs, and opens its promotion
   PR; merging it lets ArgoCD deploy the target map.

Confirm success from the workflow summary, generated overlay diff, green pod
startup, and the expected `GRAM_EMAIL_TEMPLATE_IDS` key count. A template-source
task stops here. If the request also introduces a new sending event, wire its
`email.Service.Send` call and add a focused event-path test in the same change.

- First sync creates/reuses only the exact v2 managed name; legacy emails remain
  untouched for rollback. Duplicate managed names fail closed.
- A committed ID is authoritative. If it belongs to another name, CI stops.
- A partial run is recoverable by exact managed name before its ID is committed.
- gram-infra writes non-secret IDs to `email-templates-<env>.yaml`; Helm injects
  JSON as `GRAM_EMAIL_TEMPLATE_IDS` into server and worker.
- With Loops enabled, application startup rejects missing or unknown keys.
- Do not hand-edit generated IDs or store them in Postgres.

Recovery is safe and rerunnable:

| Failure                       | Result / action                                              |
| ----------------------------- | ------------------------------------------------------------ |
| Created before ID commit      | Rerun; exact managed name recovers it                        |
| Update/publish failure        | Rerun; existing draft is reconciled                          |
| Guardian error                | Fix source; rerun release                                    |
| Duplicate managed name        | Remove/rename duplicate in Loops with authorization          |
| Revision conflict             | Rerun to fetch current revision; reconcile intentional edits |
| Committed ID points elsewhere | Stop; repair generated mapping/provider state deliberately   |

## Common failures

- Uploading MJML through the Content API: automation requires LMX.
- Adopting or renaming legacy Loops records instead of creating v2 records.
- Treating the starter as runtime inheritance.
- Committing an LMX variable absent from the Go template, or leaving dead backend
  merge variables for raw HTML LMX cannot render.
- Running live sync locally or exposing the API key in argv, output, or Git.
