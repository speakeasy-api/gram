# Agent Skills specification conformance

The [Agent Skills specification](https://github.com/agentskills/agentskills/blob/main/docs/specification.mdx) defines a skill as a **directory** containing a required `SKILL.md` plus optional `scripts/`, `references/`, `assets/`, and any other files. Gram models a skill as a **single `SKILL.md`**.

This document records where Gram conforms to the specification today, where it does not, and what closing the remaining gap requires. It is the audit that scoping the file-tree work depends on.

## Frontmatter: conformant

All frontmatter parsing and validation runs through one canonical parser, `parseSkillManifest` in `server/internal/skills/manifest.go`. Every write path uses it — the management API (`skills.create`, `skills.addVersion`), hook capture (`CaptureSkillContent`), suggestion approval, and the embedded Platform MCP skills checked at build time. There is no second implementation, so validation cannot drift between ingest routes.

Every field the specification defines is parsed and validated:

| Field           | Specification                                                                                                 | Gram                                                                                                                                                |
| --------------- | ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`          | Required. 1–64 characters, lowercase `a-z`, `0-9`, and hyphens. No leading, trailing, or consecutive hyphens. | Enforced by `ValidSpecName`. A name that needs folding (`My_Skill`) still normalizes to a usable registry name but is reported as `invalid_format`. |
| `description`   | Required. 1–1024 characters, non-empty.                                                                       | Enforced. Length is counted in Unicode code points.                                                                                                 |
| `license`       | Optional. Free-form string.                                                                                   | Type-checked as a string.                                                                                                                           |
| `compatibility` | Optional. 1–500 characters.                                                                                   | Enforced, including the non-empty rule.                                                                                                             |
| `metadata`      | Optional. Map of string keys to string values.                                                                | Enforced, with a per-key `metadata.<key>` error for non-string values.                                                                              |
| `allowed-tools` | Optional, experimental. Space-separated string.                                                               | Type-checked as a string.                                                                                                                           |

Unknown top-level keys are tolerated rather than rejected, which matches the specification's silence on vendor extensions. They are still returned to callers on `SkillVersion.frontmatter` and rendered on the skill detail page, so an extension field is visible for review rather than silently dropped.

Validation failures are non-fatal by design: the version is stored with `spec_valid = false` and a sorted `validation_errors` array. Only content Gram cannot parse at all — malformed YAML, a missing delimiter, a non-string `name` — is rejected outright. Invalid versions are excluded from distribution, so a bad manifest is visible in the registry without being shipped to a developer machine.

The parser also hardens YAML beyond what the specification requires, because manifests are untrusted input: anchors, aliases, merge keys, custom tags, duplicate keys, and non-finite floats are refused, and nesting depth, node count, and document size are bounded.

**Conclusion: the frontmatter gap the ticket suspected does not exist.** `license`, `compatibility`, `metadata`, and `allowed-tools` are all parsed, validated, persisted or re-derived, and surfaced.

## Name-to-directory matching: partially conformant

The specification requires `name` to match the parent directory name. Gram enforces this only where a directory actually exists:

- Embedded Platform MCP skills are loaded from disk, and `plugins.generate` requires the directory name to equal the normalized manifest name.
- Uploaded and captured skills have no directory. The manifest name is authoritative, and the skill row is keyed on the normalized name within the project.

This check cannot be completed for uploaded skills without a directory model.

## Supporting files: reported, not stored

Gram stores one `SKILL.md` per version (`skill_versions.content`, capped at 65,536 bytes) and distributes exactly that file to `skills/<name>/SKILL.md` inside a plugin package. A skill that depends on `references/`, `scripts/`, or `assets/` is therefore incomplete once Gram distributes it, and the failure surfaces on the developer's machine rather than at upload.

Gram now reports the supporting files a manifest declares instead of dropping them silently. `parseSkillResourceReferences` (`server/internal/skills/resources.go`) reads the Markdown body and returns each referenced path with its kind (`script`, `reference`, `asset`, or `other`), exposed on `SkillVersion.resource_references` and rendered as a "Supporting files" panel on the skill detail page.

Detection covers what the specification's own examples produce:

- Markdown inline link and image destinations, including the angle-bracket form and link reference definitions.
- Any path under `scripts/`, `references/`, or `assets/` appearing anywhere else — prose, inline code spans, or fenced code blocks such as `python scripts/extract.py`.

Destinations are resolved to a path relative to the skill root: percent escapes are decoded, fragments and queries dropped, and `.`/`..` segments collapsed. Absolute paths, in-page anchors, and anything carrying a URI scheme are external and excluded. A relative destination outside the reserved directories is reported as `other` only when it plausibly names a file, meaning its last segment has an extension.

This makes the incompleteness visible and gives a reviewer the one governance signal that matters most today — that a skill runs executable content Gram cannot show them — but it is a report, not a fix. The referenced bytes are still neither ingested, versioned, nor distributed.

## Remaining gap: the file tree

Closing the rest requires modeling a skill version as a set of files rather than one string. The pieces, in dependency order:

1. **Storage.** A `skill_version_files` table keyed on `(skill_version_id, path)` holding the bytes, plus per-file size and total-tree limits. `skill_versions.content` stays as the `SKILL.md` fast path so existing reads do not regress.

2. **Hashing.** The version digest already anticipates this. Its preimage is `"skill-manifest-v1" \0 "SKILL.md" \0 <canonical file digest> \0` — a single-entry Merkle list with the path baked in. Extending it to a tree means folding every `(path, digest)` pair in sorted path order under a `skill-tree-v1` domain tag. Because the tag changes, existing `canonical_sha256` values keep their meaning and the `(skill_id, canonical_sha256)` dedup constraint stays correct; a tree digest belongs in a new column rather than overwriting the manifest digest. This is what makes a change to a script produce a new version, which drift detection and attribution currently miss.

3. **Ingest.** A directory upload (zip or git path) on the management API, and a multi-file capture payload from the device agent. Capture already separates observation from content upload — the agent reports `raw_sha256` and uploads bytes when the server asks — so the same handshake extends to a tree digest and a file set.

4. **Distribution.** `emitPluginSkills` writes one file per skill; it becomes a loop over the tree. Executable bits and path traversal need checking at write time.

5. **Surfaces.** File tree plus per-file diff in version history; efficacy scoring and edit suggestions extended past `SKILL.md`; `scripts/` contents shown for review.

Steps 1 and 2 are the load-bearing ones — ingest, distribution, and the surfaces are all straightforward once a version can hold more than one file, and none of them can be built before it can.

## Where to look

| Concern                                                    | Location                                                                             |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Frontmatter parsing, validation, canonicalization, hashing | `server/internal/skills/manifest.go`                                                 |
| Supporting-file detection                                  | `server/internal/skills/resources.go`                                                |
| Version write path                                         | `server/internal/skills/impl.go` (`recordVersion`)                                   |
| Hook capture                                               | `server/internal/skills/capture.go`, `server/internal/hooks/upload_skill_content.go` |
| Storage                                                    | `server/database/schema.sql` (`skills`, `skill_versions`)                            |
| API shape                                                  | `server/design/skills/design.go` (`SkillVersion`)                                    |
| Plugin bundling                                            | `server/internal/plugins/generate.go` (`emitPluginSkills`)                           |
| Detail page                                                | `client/dashboard/src/pages/skills/SkillContent.tsx`                                 |
