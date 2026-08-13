# Transactional email templates

This directory is the source of truth for Gram transactional email content.
Every application template has:

- a stable logical key in `server/internal/email/templates.go`;
- metadata and its exact variable contract in `manifest.json`;
- an LMX body named `<logical_key>.lmx`.

Loops assigns different transactional IDs in dev, test, and prod. Application
code never owns those IDs. Release CI reconciles this source into the target
Loops environment and commits the resolved key-to-ID map to gram-infra.

## Lifecycle

On the first release containing a manifest entry, CI:

1. looks only for its managed name, `gram.transactional.v2.<logical_key>`;
2. creates a new Loops transactional email when that name does not exist;
3. updates the draft from the LMX source with optimistic revision protection;
4. runs Loops Guardian and stops on any error;
5. publishes the draft;
6. writes the resolved non-secret ID to
   `gram-infra/infra/helm/gram/email-templates-<env>.yaml`.

Legacy Loops emails are not adopted, renamed, or deleted. They remain available
for rollback. If CI creates a v2 email but fails before committing the mapping,
the next run recovers it by exact managed name instead of creating a duplicate.

After the first release, the committed ID is authoritative. CI verifies that it
still belongs to the expected managed name, then updates it in place. An existing
published message whose subject, preview, and LMX already match is left unchanged.

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
contract exactly. It also checks every `.lmx` file in this directory for XML
well-formedness and Loops-supported attribute ranges, and rejects undeclared or
accidentally unused variables in registered templates.

## Design system

The approved visual source is `transactional_base.mjml`: a light editorial shell
with the Speakeasy spectrum rail, wordmark, compact mono labels, Tobias display
headlines, square black actions, neutral outlined details, and a dashed pale
footer. `weekly_usage_summary.mjml` specializes that system for a data-heavy
report. Do not replace this language with a separate LMX-derived theme.

`transactional_base.lmx` is the canonical production translation. It uses the
approved text-only header and Inter fallback because LMX cannot express the raw
spectrum table, hosted font faces, or public favicon from MJML. It preserves the
same hierarchy, palette, square action, panels, and pale footer rather than
inventing replacements. Automated sync uses LMX because the Loops Content API
does not accept MJML; the MJML files remain canonical for design and visual QA.

## Release and runtime handoff

Only the gram-infra GitHub release workflows set `SYNC_LOOPS_EMAILS=true`.
The sync task reads the target environment's `LOOPS_API_KEY` directly from
Secret Manager, masks it, and passes it only to the child process. The key is
never written to Git, a command argument, or workflow output.

The generated overlay is layered by ArgoCD and Helm serializes its `ids` map into
`GRAM_EMAIL_TEMPLATE_IDS` for server and worker. When Loops is enabled, both
processes fail startup if any registered key is missing or any unknown key is
present. Local development accepts an empty map because the Loops transport is
disabled there.
