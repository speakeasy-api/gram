# Loading skills over MCP

Speakeasy serves the skills linked to a hosted MCP server over that server, using the MCP Skills extension (`io.modelcontextprotocol/skills`, SEP-2640). An agent connected to one of those servers lists the skills linked to it, verifies each one by digest, and reads its `SKILL.md` when it needs it, without installing the plugin package. Package delivery is separate: a published plugin still carries its distributed skills under `skills/`.

The feature is behind the `skills-over-mcp` flag while client support matures. With the flag off, nothing in this page is reachable.

## What is served

Two surfaces serve skills.

**Hosted MCP servers.** A hosted MCP server, the organization MCP server and the organization gateway serve the managed skills linked to them (see [Who receives which skills](#who-receives-which-skills)). Each entry is the skill's current served version: the version the link is pinned to, or else the latest version that passes Agent Skills validation. Versions that fail validation are never served, even when they are newer.

**The Platform MCP.** Both Platform MCP endpoints serve the reviewed Platform MCP skills that ship inside the Platform plugin, straight from the binary: the external Platform MCP at `/platform-mcp`, which administrators reach through the Platform plugin with OAuth, and the platform toolset at `/platform/mcp/{toolsetSlug}`, which managed assistants call with their runtime token. Both build their catalog from `mcpskills.NewEmbeddedCatalog`, and its entries are the same `SKILL.md` files kept under `server/internal/plugins/platform_mcp_skills/<name>/SKILL.md`, so what the plugin package carries and what the endpoints serve are byte-identical. Embedded skills belong to no project, so their URIs use the fixed slug `speakeasy`: `skill://speakeasy/<name>/SKILL.md`. Every caller whose organization has the flag receives the whole catalog; there is no per-caller filtering.

A skill is served as a single file. Its entry has exactly one resource:

| Field                 | Value                                                                                                                                                                                        |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `uri`                 | `skill://<project-slug>/<name>/SKILL.md`, where `<project-slug>` is the slug of the skill's project and `<name>` is its canonical name. On the Platform MCP, `<project-slug>` is `speakeasy` |
| `resources[0].uri`    | the same URI                                                                                                                                                                                 |
| `resources[0].digest` | `sha256:<hex>` over the served bytes: the version's `skill_versions.raw_sha256` on a hosted endpoint, a hash of the embedded file on the Platform MCP                                        |
| `resources[0].size`   | the length of those bytes                                                                                                                                                                    |
| `frontmatter`         | the version's YAML frontmatter rendered verbatim as JSON: `name`, `description`, and every other field the author wrote                                                                      |

The frontmatter is never curated. `allowed-tools`, `license`, `metadata` and any future field appear only when the author wrote them. The extension requires the frontmatter `name` to equal the URI's `<name>` segment, the one immediately before `/SKILL.md`. Validation trims the name before checking it, so a version whose author quoted a padded name (`name: "refunds "`) is stored valid under the trimmed name while its frontmatter keeps the padding; such a version is never served (the resolver skips it with an error log, and the platform catalog refuses to build), so every served entry's frontmatter `name`, canonical name and URI name segment agree.

## Who receives which skills

A hosted endpoint serves a skill to a caller when all of the following hold:

1. A live skill link names the endpoint: on a hosted MCP server, a link to that server; on a project gateway, a link to that gateway; on the organization gateway, a link to the organization gateway itself or to a member server the caller can reach through it. Links are made from the server's Skills tab, the skill page, or the `skills.link` API.
2. The skill is not archived, its project is not deleted, and the link resolves to a servable version: its pinned version, or the latest valid version when it tracks latest.
3. The caller is an authenticated member of the owning organization acting as themselves.

Membership is checked against the database on every resolution, not taken from the credential: the recipient lookup reads the user only while they are not deleted and hold an active membership in the enabled owning organization. A session or OAuth token that outlives its holder's membership therefore resolves to no skills. Anonymous callers of a public server, API-key callers of any kind, and internal workflow callers are served an empty list and get `-32602` on `skills/get` and `resources/read`, while the capability is still advertised to them. An API key is excluded even though a legacy key's auth context carries its creator's user id: the key is the actor, not that person, so serving the creator's personal assignments to every agent holding the key (and recording each read as the creator's activation) would be wrong, and the RBAC engine already refuses to treat an API key as a user principal. Reaching the server is the authorization: there is no per-skill RBAC check, because a linked skill is text meant for whoever can use that server.

When several links carry the same skill it appears once in the listing. A pinned link wins over one tracking "latest". When two links pin different versions, the newer version wins (by promotion time, then creation time, then id) and the conflict is logged at warn level with `gram.mcp.skill.pin_conflict_count`, so a contradictory pin set is visible rather than silent.

## The feature flag

`skills-over-mcp` is a PostHog flag targeted by organization, like budgets. In Go it is `feature.FlagSkillsOverMCP`; the dashboard reads the same key as `FEATURE_FLAGS.skillsOverMcp`.

It is evaluated for the organization that owns the endpoint (hosted), the caller's active organization (platform toolset), or the organization the OAuth caller signed in to (external Platform MCP), at most once per request, and never per entry. On a hosted endpoint that happens on `initialize`, `server/discover`, the extension methods and `skill://` reads; on the platform toolset also on `resources/*`; on the external Platform MCP on `initialize`, `server/discover`, the extension methods and `skill://` reads. `tools/*` never evaluates it. Evaluation is an organization metadata lookup (one primary-key read, for the slug the flag's group targets) plus a decision from the flag provider's locally cached definitions (`IsFlagEnabledLocal`): the PostHog SDK polls definitions in the background and the decision never falls back to a remote request, so no handshake waits on PostHog. When local definitions are unavailable or inconclusive, or the server runs without a PostHog personal API key, the decision fails closed and the extension stays off.

Off, the surface is exactly what it was before: the `extensions` capability is absent, `skills/list` and `skills/get` answer method-not-found (`-32601`), a `resources/read` of a `skill://` URI on a hosted endpoint is an ordinary resource lookup that is not found, on the platform toolset `resources/*` stays method-not-found, and on the external Platform MCP a `skill://` read is a resource that is not found, as before. The dashboard hides every surface described below.

## The wire surface

Both surfaces advertise the extension in `initialize` and `server/discover` when the flag is on. A hosted endpoint keeps its existing capabilities and adds `extensions`:

```json
{
  "capabilities": {
    "tools": {},
    "prompts": {},
    "resources": {},
    "extensions": {
      "io.modelcontextprotocol/skills": {}
    }
  }
}
```

The platform toolset otherwise declares only `tools`. With the flag on it declares `tools`, `resources` and `extensions`, and nothing else: `resources` is added because the extension depends on the Resources primitive, and `prompts` is never declared on that surface. The external Platform MCP always declares `tools`, `resources` and `logging`, for its setup guides; with the flag on it adds `extensions`. That server's capabilities are shared by every caller, so the extension is added to each caller's `initialize` and `server/discover` result rather than to the server's options.

`directoryRead` is not declared: skills are single files and `resources/directory/read` is not implemented.

The extension's methods are available on any protocol revision the server serves once the capability is declared. Extensions are negotiated separately from the revision.

### `skills/list`

Params are `{}` or `{"cursor": ...}`. Every listing is answered in one page sorted by skill name, and no `nextCursor` is ever issued, so the only accepted cursor is an absent or empty one; any other cursor names a page that does not exist and is `-32602`. The result is private to the caller:

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "resultType": "complete",
    "skills": [
      {
        "uri": "skill://my-project/refund-policy/SKILL.md",
        "frontmatter": {
          "name": "refund-policy",
          "description": "Handle refund requests under the current policy"
        },
        "resources": [
          {
            "uri": "skill://my-project/refund-policy/SKILL.md",
            "digest": "sha256:b95a384300adeea2d902f7d19cd7c04b378ef58e09759107b9c7db4dcacbaa25",
            "size": 190
          }
        ]
      }
    ],
    "ttlMs": 0,
    "cacheScope": "private"
  }
}
```

A caller served nothing gets `"skills": []`, never `null`.

### `skills/get`

Params are `{"uri": "skill://<project-slug>/<name>/SKILL.md"}`. The result is `{"skill": <entry>}` with the same cache hints. A URI that does not name a skill served to this caller, whether because it is malformed, unknown, or not linked to this endpoint, answers `-32602` with the message `no skill is served at <uri>`.

### `resources/read`

Params are `{"uri": "skill://<project-slug>/<name>/SKILL.md"}`. The result carries the served version's content, exactly the bytes the digest covers:

```json
{
  "contents": [
    {
      "uri": "skill://my-project/refund-policy/SKILL.md",
      "mimeType": "text/markdown",
      "text": "---\nname: refund-policy\n...\n"
    }
  ]
}
```

A skill read is answered from the distribution, not from a toolset resource. It invokes nothing, so it is not metered, not billed, writes no tool-call telemetry row, and runs no risk scan on the way out; the content was validated when the version was recorded. An unknown or unassigned URI answers `-32602`. On a hosted endpoint, any `resources/read` whose URI does not have the `skill://<project-slug>/<name>/SKILL.md` shape takes the existing toolset resource path untouched. The platform toolset has no other resources to read: with the flag on, a `resources/read` of any other URI there answers `-32602`, and with it off the method stays method-not-found (`-32601`). On the external Platform MCP, any `skill://` URI that names no served skill answers `-32602`, and other URIs take the setup guide resource path.

Skills never appear in `resources/list`. The platform toolset answers `resources/list` with an empty list while the flag is on, so its declared `resources` capability is honest. The external Platform MCP keeps listing and reading its setup guides as before; only `skill://` URIs are answered from the catalog.

### Cache hints

`skills/list`, `skills/get` and skill reads carry `ttlMs: 0` and `cacheScope: "private"`, the repository's current stance for caller-varying results. A client that wants a fresher entry re-calls `skills/get`.

### Errors

| Case                                        | Answer                                  |
| ------------------------------------------- | --------------------------------------- |
| Flag off, any extension method              | `-32601` method not found               |
| Non-empty cursor on `skills/list`           | `-32602`                                |
| Missing `uri` on `skills/get`               | `-32602`                                |
| Unknown, malformed, or unassigned skill URI | `-32602`, `no skill is served at <uri>` |
| Malformed params                            | `-32600`                                |

### Demand census

The `mcp.request` counter keeps `skills/list`, `skills/get` and `resources/directory/read` as distinct `gram.mcp.method` values on every surface, including organizations that do not have the flag and answer them with method-not-found. Client demand for the extension is therefore measurable before it is switched on anywhere.

## Activation and reporting

A successful hosted read attempts to record one `skill_observations` row with provider, source and source level `mcp`, source path set to the skill URI, the caller's user id and the MCP session id, and the skill and version already linked (`reconciled_at` set at insert time, since the served version is known). The idempotency key is `mcp:<session id>:<raw sha256>`, so a repeat read of the same content within one session inserts no second row and does not inflate counts; a new session or a new version records a new activation. The insert also bumps the skill's first-seen, last-seen and seen-count fields, the part of hook reconciliation that would otherwise run later. Reads of the embedded catalog, on either Platform MCP endpoint, record nothing: there is no stored version to activate.

MCP activations stay out of the two downstream pipelines that consume reconciled, session-bearing observations. An MCP session id is neither a dev nor an assistant session, so the row would be labelled a dev-surface sighting that never joins a hook session, and it names no chat transcript to score. The row is therefore born complete for both: the insert stamps `metrics_synced_at` and `efficacy_enqueued_at` alongside `reconciled_at`, so the project finders that fan out the ClickHouse `skill_session_versions` sync and the efficacy enqueue never select a project for an MCP read, and no sweep is launched for work that would not drain. The batch queries of both pipelines additionally exclude `provider = 'mcp'` as belt and braces. An MCP read counts on the skill (seen count, last seen, `last_loaded_over_mcp_at`) and on the `mcp.skill.read` metric, and nowhere else.

A skills listing serves at most 1000 skills. The query keeps one version per skill before the cap applies, so a skill linked many times takes one slot rather than one per link; an endpoint with more skills than the cap has its later skills by name cut off and the resolver logs a warning. The warning fires only when a skill past the cap exists, not when the listing fills it exactly.

The management API exposes the most recent such read as `last_loaded_over_mcp_at` on `SkillDistribution` (returned by `skills.distribute`) and `PluginSkillDistribution` (returned by `skills.listDistributions`). It is a per-skill value, so every distribution of one skill carries the same time, and it reflects only reads over a Speakeasy-hosted MCP server: a load of the same skill reported by a coding-agent hook does not move it.

Reads are counted on the `mcp.skill.read` counter with `gram.mcp.surface` (`hosting` or `platform`), `gram.mcp.skill.origin` (`distribution` for a managed skill, `embedded` for the platform catalog) and the network surface. Reads on the external Platform MCP are not counted: `gram.mcp.surface` has no value for that endpoint. Only successful reads count, and nothing is recorded per server or per skill, so the counter stays bounded.

## Client support

The extension is new, and a client must implement it to benefit: it issues `skills/list` and `skills/get` only after seeing the capability, and since skills are absent from `resources/list` a client without the extension never encounters them unless given a URI.

- **fast-agent** implements the client side of the extension, so an agent built on it discovers and loads skills from a Speakeasy-hosted server without further configuration.
- **Hugging Face** has a server-side implementation. It is not a client, but it is a useful interoperability reference for the entry shape and error codes.

Verification, approval and the acting window are the host's job under the extension. Speakeasy supplies consistent entries and bytes; it cannot prevent a client that skips verification from loading stale content, and a digest match is not a trust anchor, since the same server supplies both the entry and the content.

## Package delivery is separate

Plugin distribution and MCP serving are separate. Distributing a skill to a plugin still puts it in the published package under `skills/`, as described in [Package format](../plugins/package-format.md). Serving it over a hosted MCP server takes a skill link to that server, made on its own; distributing or undistributing a plugin adds or removes no link, and each route is pinned independently. A newly recorded version of a skill tracking "latest" is visible over MCP on the next `skills/list` through a link that tracks latest, and reaches the package on its next publication.

Remote MCP servers proxy every request upstream, so Speakeasy does not serve linked skills through them; the upstream server's own skills pass through unchanged.

## In the dashboard

These surfaces are shown only when the flag is on for the organization.

- **Skill detail, "Served over MCP".** Lists the MCP servers and gateways that serve the skill, with each link's version (pinned or latest), the served digest of the current version (`sha256:...`, copyable), and when the skill was last loaded over MCP. Writers link the skill to another server or gateway, or remove a link, from here; organization gateway links are managed here.
- **MCP server and gateway detail, Skills tab.** Lists the skills linked to this endpoint with the resolved version and last load over MCP. Writers link or unlink skills from the tab; it manages the endpoint's own links only.
- **Version history.** Shows each version's served digest beside its canonical hash.

## Platform MCP

Assessment for the Platform MCP, recorded per the maintaining-platform-mcp skill.

- **Outcome.** An administrator can serve a skill from an MCP server or gateway, stop serving it, and see which surfaces serve which skills and whether agents load them.
- **Actor and target.** Organization administrators (writes) and members (reads) acting on a skill in an explicit project and one MCP surface.
- **Existing tools.** `distribute_skill` and `undistribute_skill` stay about plugin packages and are unchanged. `get_skill`, `list_skill_versions`, `create_skill` and `add_skill_version` return `served_digest` on valid version summaries, so an agent can match served content to skill-load logs; `get_skill` and `list_skill_distributions` also report `last_loaded_over_mcp_at`, and `list_skill_distributions` also returns `served_by`, the surfaces serving the skill from its live links.
- **Decision: add `link_skill_to_mcp` and `unlink_skill_from_mcp`.** Linking is a new write with no existing tool, so it gets its own pair: admin-only, each call confirmed, and acting on one exactly named server or gateway. Linking a skill that is already linked to that surface replaces the link's pin (or clears it to track latest), which is why the tool is annotated destructive. Where serving skills over MCP is not available both answer with a readable refusal instead of disappearing from the catalogue. The platform toolset's own catalog is delivered to the agent by the protocol itself, which no tool can improve on.
- **Evidence.** The Platform MCP skill link tests cover link, unlink, exact target resolution, confirmation and the unavailable refusal; the skill tool tests cover `last_loaded_over_mcp_at` end to end (a recorded MCP read shows up on the next listing).

The Platform MCP's `search_docs` and `read_doc` tools index the pinned `mcp-setup-docs` module, not this repository's `docs/` tree, so this page is contributor documentation and is not agent-visible through those tools.

## Out of scope

Deliberately not built in this iteration: multi-file skills and supporting files, `resources/directory/read`, change notifications when a listing changes, serving linked skills through remote MCP servers, and a non-zero cache TTL. A skill manifest is limited to 64 KiB, far under the extension's 16 MiB per-skill limit, so no entry needs the limit checks the extension describes.
