---
name: authoring-platform-mcp-skills
description: Use when adding, editing, reviewing, testing, or locating a reviewed skill distributed with the Platform MCP plugin; triggers include "Platform MCP skill", "platform_mcp_skills", "add a catalog workflow", "bundle a skill with Platform MCP", and "where should this Platform skill live?".
---

# Authoring Platform MCP skills

Platform MCP skills are first-party Speakeasy AI Control Plane (AICP) product assets, not project-owned Skill rows and not repository-agent guidance. Their single source is `server/internal/plugins/platform_mcp_skills/<skill-name>/SKILL.md`. The plugin generator embeds every valid directory there into every Platform MCP package.

## What belongs in a Platform MCP skill

Prefer repeatable AICP workflows that a person commonly performs manually in the Speakeasy dashboard and that the authenticated Platform MCP can complete safely. Good candidates combine multiple reviewed tools into a recognizable outcome, preserve the same choices and confirmations as the dashboard, and verify the resulting live state.

Examples include selecting a reviewed MCP Catalogue entry, configuring it for an explicit project, completing secure setup, and adding the ready MCP to that project's Default plugin. A thin restatement of one tool, an internal engineering procedure, or a workflow outside the Platform MCP's reviewed capabilities does not belong here.

Use product language in every distributed skill: **Speakeasy AI Control Plane**, **AI Control Plane**, or **AICP**. Never expose the internal codename “Gram” in frontmatter, instructions, examples, prompts, or user-facing messages. Internal Go/TypeScript symbols and HTTP headers may retain existing contract names, but the skill must describe them in AICP terms.

## Source and output map

| Purpose                           | Path                                                                                     |
| --------------------------------- | ---------------------------------------------------------------------------------------- |
| Authoring source shipped to users | `server/internal/plugins/platform_mcp_skills/<skill-name>/SKILL.md`                      |
| Generator and validation          | `server/internal/plugins/generate.go` (`loadPlatformMCPSkills`, `emitPlatformMCPSkills`) |
| Package tests                     | `server/internal/plugins/generate_test.go`                                               |
| Claude package output             | `platform-mcp/skills/<skill-name>/SKILL.md`                                              |
| Portable Agent Plugin output      | `agent-plugins/platform-mcp/skills/<skill-name>/SKILL.md`                                 |
| Contributor guidance only         | `.agents/skills/authoring-platform-mcp-skills/SKILL.md` (this file)                      |

Do not put a user-facing Platform workflow under `.agents/skills/`: that directory teaches contributors and coding agents how to work on this repository. It is not copied into the customer Platform Plugin.

## Add or edit a distributed skill

1. Create `server/internal/plugins/platform_mcp_skills/<skill-name>/SKILL.md`.
2. Use a canonical lowercase kebab-case directory name, at most 64 characters. The frontmatter `name` must exactly match the directory.
3. Include `name` and `description` frontmatter:

```markdown
---
name: inspect-platform-state
description: Inspect an explicit AICP project's Platform MCP state before proposing a bounded repair.
---
```

4. Write for an agent using only the authenticated Platform MCP tools. Name real tools exactly; verify them in `server/internal/platformmcp/tool_*.go` and their registration in `server/internal/platformmcp/tools.go` rather than inventing APIs.
5. Anchor the skill in a common dashboard outcome. Preserve the dashboard's explicit target selection, secure handoffs, confirmations, and evidence checks rather than inventing a shortcut with weaker controls.
6. Keep authority explicit: package installation grants no access; live OAuth, entitlement, membership, `org:admin`, generation, and revocation checks remain authoritative.
7. Separate reads, user choices, secure browser handoffs, confirmation, mutation, and post-mutation verification. Never treat copy/download/install intent as runtime evidence.
8. Never ask for or expose API keys, passwords, tokens, OAuth codes, client secrets, secret headers, or MCP credentials. Present only server-returned non-secret setup/provider URLs.
9. For bounded feedback, ask for consent and use `send_platform_mcp_feedback`; do not add the hooks-backed `speakeasy-skill-feedback` sidecar.

Adding the directory is sufficient for distribution. Do not add another `go:embed` directive, skill-name constant, or renderer branch. The embedded registry loads every `platform_mcp_skills/*/SKILL.md` and emits the same bytes into Claude and portable Agent Plugin packages.

## Review checklist

- [ ] Directory and frontmatter names match and are canonical kebab-case.
- [ ] Description states the user outcome and when the workflow applies.
- [ ] The outcome is a common AICP dashboard workflow, not a wrapper around one tool or an internal engineering task.
- [ ] Shipped text uses Speakeasy AI Control Plane, AI Control Plane, or AICP—never the internal codename.
- [ ] Every named Platform MCP tool exists, is registered, and the sequence respects its contract.
- [ ] Exact project and target selection stay explicit; the skill does not infer scope.
- [ ] Mutations require the same confirmation expected by the server/onboarding flow.
- [ ] OAuth and secret entry stay in the server-returned secure browser handoff.
- [ ] Fresh readiness is checked before distribution; final live state is rechecked afterward.
- [ ] No organization/project identifiers, customer data, credentials, fixed environment URL, or tenant configuration is embedded.
- [ ] Claude and Agent Plugin outputs contain byte-identical skill content and no hooks runtime or skill-feedback sidecar.
- [ ] Do not bump `platformMCPGeneratorVersion` for skill text alone: embedded skill bytes already participate in the Platform package fingerprint. Bump it only for a rollout-significant generator behavior change that emitted-byte fingerprinting cannot represent.

## Validation

Run focused package tests through repository-pinned tooling:

```bash
mise exec -- go test ./server/internal/plugins -run 'TestGeneratePluginPackagesIncludesPlatformMCPOnlyWhenEnabled|TestGeneratePlatformMCPPluginPackageDirectDownloadsShareDefinition' -count=1
```

Then run the normal server gates for the complete change:

```bash
hk fix
mise lint:server
```

For a new skill, extend `TestGeneratePluginPackagesIncludesPlatformMCPOnlyWhenEnabled` (or a focused neighboring test) to assert the skill appears in both package layouts with identical bytes. Add assertions for the workflow's required tool names and forbidden credential/hook material.

## Initial reference skill

`server/internal/plugins/platform_mcp_skills/add-mcp-from-catalog/SKILL.md` mirrors the common AICP dashboard workflow represented by Platform MCP onboarding:

1. verify authenticated discovery;
2. search the reviewed catalog and list eligible projects;
3. obtain exact project and candidate choices;
4. inspect and register privately with non-secret configuration only;
5. use secure setup/authorization handoffs;
6. explicitly confirm identity-provider attachment when required;
7. force fresh readiness;
8. explicitly confirm and add the ready MCP to the selected project's existing Default plugin;
9. recheck and report live evidence.

Use it as the pattern for authority boundaries and evidence, not as a template for unrelated workflows.

## Common mistakes

- Editing generated package output instead of `platform_mcp_skills/`.
- Putting the shipped workflow in `.agents/skills/`, which only reaches repository contributors.
- Reusing ordinary `PluginInfo.Skills`; that path intentionally bundles the hooks-backed skill-feedback sidecar.
- Hardcoding one skill in Go, forcing every new skill to modify the generator.
- Claiming install, OAuth intent, or a stale status proves authorization/readiness.
- Asking the user to paste secrets into chat instead of using the secure server-returned handoff.
