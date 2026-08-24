---
name: craft-transactional-emails
description: Use when creating, restyling, reviewing, validating, or previewing Gram/Speakeasy transactional emails in LMX or MJML, including branded email, email screenshots, desktop/mobile QA, or files under server/internal/email/loops/.
---

# Craft transactional emails

LMX in `server/internal/email/loops/` is the production delivery source. The
inline MJML starter is the approved visual specification. Both follow the
Speakeasy brand email system: all live text in email-safe Helvetica/Arial, the
licensed brand fonts (Tobias, ABC Diatype, ABC Diatype Mono) appearing only
inside baked image assets. Never load webfonts in email — Gmail and Outlook
strip `@font-face`.

## Authorization and confidentiality

- Repository edits do not authorize manual Loops reads, mutations, previews,
  test-sends, or publishes. Resolve each external action separately.
- Never use customer or private production names, IDs, domains, addresses,
  URLs, or figures in source, temporary previews, screenshots, logs, or test
  sends. Use `Example Organization`, `person@example.com`, and `<ORG_ID>`.
  The only remote assets allowed anywhere are the canonical Loops-hosted brand
  images listed below; the lockup ships with this skill at
  `assets/speakeasy-lockup-black.png` for local previews.
- Keep transactional content operational. No marketing copy or unsubscribe UI.
- Recipient-visible copy names the product **Speakeasy**, never "Gram" — this
  covers subjects, preview text, body copy, CTA labels, image alt text, footer
  reasons, and the sender identity (`Speakeasy <platform@speakeasy.com>`).
  Internal identifiers keep their `gram` prefix: logical keys,
  `gram.transactional.v2.*` managed names, and file names.

## Discover the contract

1. Read the repository-root `AGENTS.md` confidentiality rules.
2. Inspect `server/internal/email/template_<name>.go`, `templates.go`,
   `loops/manifest.json`, and the matching `.lmx` file.
3. For an existing email, reuse its logical key. For a new email, choose one
   snake-case key and use it unchanged in Go, manifest, and managed name. Never
   add a variable only in LMX or only in Go.
4. Treat the inline MJML starter below as the approved visual source of truth.
   Preserve its Speakeasy lockup header, light canvas, uppercase gray eyebrow,
   RGB gradient line under the headline, square black CTA, and closing footer:
   a hairline divider followed by the 12px gray footer reason on the white
   body — no gray band and no marketing-style black brand banner.
   Transactional emails end there. Never change the starter to match a reduced
   implementation or invent a new palette.

## Author production LMX

New-template checklist:

1. Create `template_<key>.go`: define the typed fields; implement `Key()`,
   `Variables()`, and `AddToAudience()`.
2. Add a `TemplateKey` constant and the fully initialized zero value to
   `RegisteredTemplates` in `templates.go`.
3. Copy `loops/transactional_base.lmx` to `loops/<key>.lmx`, specialize the
   message, and replace every generic variable with the typed contract.
4. Add `manifest.json` metadata. No provider ID belongs in Gram.

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
    "from_name": "Speakeasy",
    "from_email": "platform",
    "reply_to_email": "platform@speakeasy.com"
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
Transactional email sends from `platform@speakeasy.com`: `from_email` is the
sender local-part configured in Loops (`platform`), `from_name` is `Speakeasy`,
and `reply_to_email` is the full address. Never change these defaults per
template.

Identity map:

| Identity     | Example                                | Owner              |
| ------------ | -------------------------------------- | ------------------ |
| Logical key  | `example_notice`                       | Gram Go + manifest |
| Managed name | `gram.transactional.v2.example_notice` | Manifest/Loops     |

- New managed names are `gram.transactional.v2.<logical_key>`.
- Add the LMX file and a `manifest.json` entry with subject, preview, source, and
  the complete variable list. The command test enforces a one-to-one Go/manifest
  contract.
- Use `{data.variable_name}` everywhere; LMX names are case-sensitive. Never use
  `{DATA_VARIABLE:...}` in LMX.
- Loops accepts `Columns.gap` only from 12 through 150 and
  `Paragraph.fontSize` only from 12 through 64 when those attributes are set;
  omission uses Loops' accepted defaults. The local manifest validator
  range-checks every explicitly set value across every `.lmx` file recursively,
  including unregistered bases. The starter's smallest text is already 12px;
  never go below it.
- State the event or action directly. Delete vague lead-ins such as “a clear read
  on how your organization is tracking.” Prefer one verb-led CTA in sentence
  case ("Review access request"), no trailing arrows or uppercase styling.
- Use conditional `<Section>` blocks for variants. Do not invent fallback syntax.
- Keep condition-only variables in the Go and manifest contract.
- LMX cannot embed raw HTML. Send scalar variables and compose the layout in LMX.
- `<Image src>` must be a Loops-hosted upload. Do not use a repo-local or public
  URL as `src`. The canonical brand assets below are deliberately managed Loops
  assets and must be copied unchanged.
- Loops templates do not inherit a parent. Copy the starter into each `.lmx` and
  specialize its message content.
- Keep labels, headline fragments, body copy, CTA labels, and footer reasons
  static unless they genuinely vary at send time. A two-variable Go contract
  means the LMX may reference exactly those two variables—not generic chrome
  variables from either starter.

### Approved production LMX translation

`transactional_base.lmx` is the only production shell. Copy it; do not derive a
new shell from another email. Canonical Loops-hosted brand assets (shared with
the Speakeasy marketing email system):

- RGB gradient line (smooth warm-to-cool blend of the eight brand stops
  `#320F1E → #C83228 → #FB873F → #D2DC91 → #5A8250 → #002314 → #00143C → #2873D7`):
  `https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmshilgvx01u30j6t4t0211tl.png`.
  Set `width="536"` on the LMX `<Image>`; in the MJML starter it needs no
  width attribute — it fills the column's 536px content width (600px body
  minus 32px side padding). Placed directly under the headline — once per
  email, never anywhere else.
- The marketing emails close with a black "Connect. Secure. Control. Observe."
  banner (`cmshjs1x705gp0jy23gmy6s06.png`); transactional emails deliberately
  do not — they end on the gray footer reason. Do not add that banner or any
  closing brand block to a transactional template.
- Speakeasy lockup (stacked-layers isotype plus wordmark, black on light):
  rendered image only, never the isotype beside live text:
  `https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmt7eueee05e20izu0frdx1jq.png`
  (hosted render of this skill's `assets/speakeasy-lockup-black.png`).
  Display width `160`, `align="left"`, first block of the email.

Deprecated assets — never copy these into new or edited templates: the flat
eight-block rail `cmsrzwke702cu0j3bz2gats4u.png` (discrete blocks, off-brand
hexes) and the bare isotype header `cmsrzv81y00z60i1dmtf9twha.png` (was paired
with a live-text wordmark). If the checked-in shell still contains a deprecated
asset, an Inter body font, or live wordmark text, it predates this spec:
bring your copy up to this section before specializing it, and flag the shell
for migration rather than propagating the old chrome.

The LMX `<Style>` gets Helvetica through the team's "Speakeasy Trial" theme:
set `themeId="cmsnm620801ug0j2jdxiw79j4"` and do NOT set
`bodyFontFamily`/`bodyFontCategory` — the LMX API rejects non-Google fonts
inline (error: `"bodyFontFamily" is not a supported font family (got
"Helvetica")`), so the theme is the only Helvetica path; never substitute a
Google look-alike (Inter, ABeeZee, Roboto). Keep every other attribute
explicit so only the font flows from the theme: background `#FAFAFA`, body
`#FFFFFF` with `borderColor="#DCDCDC"`
`borderWidth="1"` `borderRadius="0"`, button `#000000` with `#FAFAFA` 15px
text, links `#2873D7`, base text `#000000` 15px at line-height 150,
H1 26px/115/-1. Grays are the brand set only: `#000000`, `#6E6E6E` (eyebrows,
footer reason), `#979797`, `#DCDCDC`. For identifier-like detail values (IDs,
domains, amounts) use `<CodeBlock blockColor="#FAFAFA" fontSize="13">`; prose
details stay in paragraphs.

These are constrained delivery translations, not a second design. Do not add a
dark masthead, neon accent, rounded corners, CSS gradients, or a substitute
palette.

## Inline transactional mail starter

```mjml
<mjml>
  <mj-head>
    <mj-title>{DATA_VARIABLE:email_title}</mj-title>
    <mj-preview>{DATA_VARIABLE:email_preview}</mj-preview>

    <mj-attributes>
      <mj-all font-family="Helvetica, Arial, sans-serif" />
      <mj-body background-color="#fafafa" width="600px" />
      <mj-section background-color="#ffffff" padding="0" />
      <mj-text color="#000000" font-size="15px" line-height="1.5" padding="0" />
      <mj-button
        background-color="#000000"
        border-radius="0"
        color="#fafafa"
        font-size="15px"
        font-weight="400"
        inner-padding="12px 20px"
        padding="0"
      />
    </mj-attributes>

    <mj-style>
      .eyebrow {
        letter-spacing: 0.08em !important;
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
    <mj-section css-class="mobile-pad" padding="28px 32px 8px">
      <mj-column>
        <mj-image
          src="speakeasy-lockup-black.png"
          alt="Speakeasy"
          width="160px"
          align="left"
          padding="0"
        />
      </mj-column>
    </mj-section>

    <!-- MESSAGE SLOT: replace or extend only this slot for event-specific data. -->
    <mj-section css-class="mobile-pad" padding="36px 32px 0">
      <mj-column>
        <mj-text
          css-class="eyebrow"
          color="#6e6e6e"
          font-size="12px"
          line-height="1.4"
          padding="0 0 8px"
        >
          {DATA_VARIABLE:email_eyebrow}
        </mj-text>
        <mj-text
          font-size="26px"
          font-weight="700"
          letter-spacing="-0.5px"
          line-height="1.15"
          padding="0"
        >
          {DATA_VARIABLE:email_headline}
        </mj-text>
        <mj-image
          src="https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmshilgvx01u30j6t4t0211tl.png"
          alt=""
          padding="16px 0 0"
        />
      </mj-column>
    </mj-section>

    <mj-section css-class="mobile-pad" padding="20px 32px 24px">
      <mj-column>
        <mj-text padding="0"> {DATA_VARIABLE:email_body} </mj-text>
      </mj-column>
    </mj-section>

    <!-- Optional detail block: delete when the message has no key detail. -->
    <mj-section css-class="mobile-pad" padding="0 32px 28px">
      <mj-column border="1px solid #dcdcdc" padding="18px 20px 16px">
        <mj-text
          css-class="eyebrow"
          color="#6e6e6e"
          font-size="12px"
          line-height="1.4"
          padding="0 0 6px"
        >
          {DATA_VARIABLE:detail_label}
        </mj-text>
        <mj-text
          font-family="'Courier New', Courier, monospace"
          font-size="14px"
          line-height="1.55"
          padding="0"
        >
          {DATA_VARIABLE:detail_value}
        </mj-text>
      </mj-column>
    </mj-section>

    <!-- Optional CTA: delete when the recipient has no useful next action. -->
    <mj-section css-class="mobile-pad" padding="0 32px 40px">
      <mj-column>
        <mj-button align="left" href="{DATA_VARIABLE:action_url}">
          {DATA_VARIABLE:action_label}
        </mj-button>
      </mj-column>
    </mj-section>

    <mj-section css-class="mobile-pad" padding="0 32px 24px">
      <mj-column>
        <mj-divider
          border-width="1px"
          border-color="#dcdcdc"
          padding="0 0 16px"
        />
        <mj-text color="#6e6e6e" font-size="12px" line-height="1.6" padding="0">
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
mise run test:server ./internal/email/... ./cmd/sync-loops-email-templates
mise lint:server
git diff --check
```

This checks manifest structure, XML well-formedness and explicit provider
attribute ranges across every `.lmx` file recursively, declared/used variables,
and Go contracts.

## Screenshots and visual QA

For every new template or layout change, create a temporary MJML specialization
from the approved starter to review the intended design. These screenshots are
design-spec previews, not renders of the production LMX in Loops. Use the same
copy/sections and generic sample values. Do not commit the preview. A copy-only
change using an already-reviewed layout may reuse existing shell references in
`.playwright-cli/email-previews/`. If repo artifact writes are not authorized,
keep everything under `/tmp/<task>/`.

Copy the bundled lockup next to the preview HTML so the relative `src`
resolves; the two Loops-hosted images load from the public CDN.

```bash
cp .agents/skills/craft-transactional-emails/assets/speakeasy-lockup-black.png /tmp/
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

This is **local visual-spec QA**. Inspect both PNGs with the image viewer.
Reject overflow, weak hierarchy, empty blocks, broken images, unresolved
variables, non-placeholder identity, "Gram" in recipient-visible copy, or
excess whitespace. Passing local QA means the design preview compiles and both
screenshots pass inspection; it does not prove that production LMX renders
identically. Report **production-render QA** as unverified unless a separately
authorized Loops preview was also inspected.

LMX has no local renderer. Production-render QA requires a separately authorized
Loops draft preview; if unavailable, report it as incomplete. Guardian proves
semantic validity, not Gmail/Outlook appearance.
