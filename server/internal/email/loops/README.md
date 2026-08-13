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
   mise exec -- go run ./server/cmd/sync-loops-email-templates --validate-only
   mise exec -- go test ./server/internal/email/... ./server/cmd/sync-loops-email-templates
   ```

Validation requires the manifest variable list to match the Go `Variables()`
contract exactly. It checks every `.lmx` file recursively for XML
well-formedness and explicitly set Loops-supported attribute ranges, and rejects
undeclared or accidentally unused variables in registered templates.

## Design system

The intended design reference is `transactional_base.mjml`: a light editorial shell
with the Speakeasy spectrum rail, wordmark, compact mono labels, Tobias display
headlines, square black actions, neutral outlined details, and a dashed pale
footer. `weekly_usage_summary.mjml` specializes that system for a data-heavy
report. Do not replace this language with a separate LMX-derived theme.

`transactional_base.lmx` is the production translation. LMX cannot express the
raw spectrum table, hosted font faces, or public favicon from MJML, so it uses
the approved text-only header and Inter fallback while preserving the same
hierarchy, palette, square action, panels, and pale footer.
