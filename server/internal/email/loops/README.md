# Transactional email templates

This directory is the source of truth for Gram transactional email content.
Every application template has:

- a stable logical key in `server/internal/email/templates.go`;
- metadata and its exact variable contract in `manifest.json`;
- an LMX body named `<logical_key>.lmx`.

## Adding or changing a template

1. Add or update the typed Go template and `RegisteredTemplates` entry.
2. Add or update its manifest entry and `.lmx` source.
3. Use only `{data.variable_name}` placeholders in LMX, subject, and preview.
4. Validate locally:

   ```bash
   ./server/internal/email/loops/sync.sh --validate-only
   mise run test:server ./internal/email/...
   ```

Validation requires the manifest variable list to match the Go `Variables()`
contract exactly. It checks every `.lmx` file recursively for XML
well-formedness and explicitly set Loops-supported attribute ranges, and rejects
undeclared or accidentally unused variables in registered templates.

## Reconciling Loops

`sync.sh` reconciles this manifest directly through the Loops Content API. Use
the read-only mode to verify local credentials and resolve the existing IDs:

```bash
./server/internal/email/loops/sync.sh --resolve-only --output /tmp/email-template-ids.json
```

Run a full reconciliation only with the development Loops API key. The script
updates drafts, checks Guardian, and publishes each template while respecting
the Content API mutation limit.

## Design system

The intended design reference is `transactional_base.mjml`: a light shell with
the Speakeasy lockup header, uppercase gray eyebrow, RGB gradient line under
the headline, square black action, neutral outlined details, and a plain gray
footer reason — no closing brand banner. The full spec, including the
Loops-hosted asset URLs and the theme that supplies Helvetica, lives in the
`craft-transactional-emails` skill (`.agents/skills/craft-transactional-emails/`).

`transactional_base.lmx` is the production translation. It references the
team's "Speakeasy Trial" Loops theme for the Helvetica body font (the LMX API
rejects non-Google fonts inline) and pins every other style attribute
explicitly. Copy its managed asset URLs unchanged into every production
template, and never use the name "Gram" in recipient-visible copy.
