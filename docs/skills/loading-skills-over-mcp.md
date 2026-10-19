# Loading skills over MCP

Speakeasy serves the skills distributed to a plugin over that plugin's hosted MCP servers, using the MCP Skills extension (`io.modelcontextprotocol/skills`, SEP-2640). An agent connected to one of those servers lists the skills it is assigned, verifies each one by digest, and reads its `SKILL.md` when it needs it, without installing the plugin package. Package delivery is unchanged: a published plugin still carries the same skills under `skills/`.

The feature is behind the `skills-over-mcp` flag while client support matures. With the flag off, nothing in this page is reachable.

## What is served

Two surfaces serve skills.

**Hosted MCP servers.** The endpoint of a toolset with MCP enabled, including any MCP server record fronting that toolset, serves the managed skills distributed to the plugins that include the server. Each entry is the skill's current served version: the version the distribution is pinned to, or else the latest version that passes Agent Skills validation. Versions that fail validation are never served, even when they are newer.

**The Platform MCP.** Both Platform MCP endpoints serve the reviewed Platform MCP skills that ship inside the Platform plugin, straight from the binary: the external Platform MCP at `/platform-mcp`, which administrators reach through the Platform plugin with OAuth, and the platform toolset at `/platform/mcp/{toolsetSlug}`, which managed assistants call with their runtime token. Both build their catalog from `mcpskills.NewEmbeddedCatalog`, and its entries are the same `SKILL.md` files kept under `server/internal/plugins/platform_mcp_skills/<name>/SKILL.md`, so what the plugin package carries and what the endpoints serve are byte-identical. Every caller whose organization has the flag receives the whole catalog; there is no per-caller filtering.

A skill is served as a single file. Its entry has exactly one resource:

| Field                 | Value                                                                                                                                                 |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `uri`                 | `skill://<name>/SKILL.md`, where `<name>` is the skill's canonical name                                                                               |
| `resources[0].uri`    | the same URI                                                                                                                                          |
| `resources[0].digest` | `sha256:<hex>` over the served bytes: the version's `skill_versions.raw_sha256` on a hosted endpoint, a hash of the embedded file on the Platform MCP |
| `resources[0].size`   | the length of those bytes                                                                                                                             |
| `frontmatter`         | the version's YAML frontmatter rendered verbatim as JSON: `name`, `description`, and every other field the author wrote                               |

The frontmatter is never curated. `allowed-tools`, `license`, `metadata` and any future field appear only when the author wrote them. The extension requires the frontmatter `name` to equal the URI's final segment. Validation trims the name before checking it, so a version whose author quoted a padded name (`name: "refunds "`) is stored valid under the trimmed name while its frontmatter keeps the padding; such a version is never served (the resolver skips it with an error log, and the platform catalog refuses to build), so every served entry's frontmatter `name`, canonical name and URI segment agree.

## Who receives which skills

A hosted endpoint serves a skill to a caller when all of the following hold:

1. A plugin in the endpoint's project, not deleted, includes the endpoint's server: one of its plugin servers is the endpoint's toolset, or is the MCP server record fronting that toolset.
2. That plugin is assigned to the caller. The caller's delivery principals are their email principal, the wildcard `*`, and their directory and role audiences, the same principals that decide who sees the plugin in a marketplace. Any assignment matching one of them qualifies.
3. The skill has an active distribution to that plugin (plugin channel, not revoked), is not archived, and has a servable version.

The caller must be an authenticated member of the owning organization with a user identity. Membership is checked against the database on every resolution, not taken from the credential: the recipient lookup reads the user only while they are not deleted and hold an active membership in the enabled owning organization. A session or OAuth token that outlives its holder's membership therefore resolves to no principals, so a removed or deleted user is served nothing, not even the skills of plugins assigned to everyone through the wildcard principal. Anonymous callers of a public server, API-key callers of any kind, and internal workflow callers are served an empty list and get `-32602` on `skills/get` and `resources/read`, while the capability is still advertised to them. An API key is excluded even though a legacy key's auth context carries its creator's user id: the key is the actor, not that person, so serving the creator's personal assignments to every agent holding the key (and recording each read as the creator's activation) would be wrong, and the RBAC engine already refuses to treat an API key as a user principal. Plugin assignment is the authorization: there is no per-skill RBAC check, because a skill is text an assigned recipient could already read from the package.

When several assigned plugins carry the same skill it appears once in the listing. A pinned version wins over "latest". When two plugins pin different versions, the newer version wins (by promotion time, then creation time, then id) and the conflict is logged at warn level with `gram.mcp.skill.pin_conflict_count`, so a contradictory pin set is visible rather than silent.

A server included in several plugins serves the union of the caller's assigned plugins' skills. There is no per-plugin endpoint.

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
        "uri": "skill://refund-policy/SKILL.md",
        "frontmatter": {
          "name": "refund-policy",
          "description": "Handle refund requests under the current policy"
        },
        "resources": [
          {
            "uri": "skill://refund-policy/SKILL.md",
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

Params are `{"uri": "skill://<name>/SKILL.md"}`. The result is `{"skill": <entry>}` with the same cache hints. A URI that does not name a skill served to this caller, whether because it is malformed, unknown, or carried only by a plugin the caller is not assigned, answers `-32602` with the message `no skill is served at <uri>`.

### `resources/read`

Params are `{"uri": "skill://<name>/SKILL.md"}`. The result carries the served version's content, exactly the bytes the digest covers:

```json
{
  "contents": [
    {
      "uri": "skill://refund-policy/SKILL.md",
      "mimeType": "text/markdown",
      "text": "---\nname: refund-policy\n...\n"
    }
  ]
}
```

A skill read is answered from the distribution, not from a toolset resource. It invokes nothing, so it is not metered, not billed, writes no tool-call telemetry row, and runs no risk scan on the way out; the content was validated when the version was recorded. An unknown or unassigned URI answers `-32602`. On a hosted endpoint, any `resources/read` whose URI does not have the `skill://<name>/SKILL.md` shape takes the existing toolset resource path untouched. The platform toolset has no other resources to read: a `resources/read` of any other URI there stays method-not-found (`-32601`), flag on or off.

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

A successful hosted read attempts to record one `skill_observations` row with provider and source `mcp`, source level `plugin`, source path set to the skill URI, the caller's user id and the MCP session id, and the skill and version already linked (`reconciled_at` set at insert time, since the served version is known). The idempotency key is `mcp:<session id>:<raw sha256>`, so a repeat read of the same content within one session inserts no second row and does not inflate counts; a new session or a new version records a new activation. The insert also bumps the skill's first-seen, last-seen and seen-count fields, the part of hook reconciliation that would otherwise run later. Reads of the embedded catalog, on either Platform MCP endpoint, record nothing: there is no stored version to activate.

MCP activations stay out of the two downstream pipelines that consume reconciled, session-bearing observations. An MCP session id is neither a dev nor an assistant session, so the row would be labelled a dev-surface sighting that never joins a hook session, and it names no chat transcript to score. The row is therefore born complete for both: the insert stamps `metrics_synced_at` and `efficacy_enqueued_at` alongside `reconciled_at`, so the project finders that fan out the ClickHouse `skill_session_versions` sync and the efficacy enqueue never select a project for an MCP read, and no sweep is launched for work that would not drain. The batch queries of both pipelines additionally exclude `provider = 'mcp'` as belt and braces. An MCP read counts on the skill (seen count, last seen, `last_loaded_over_mcp_at`) and on the `mcp.skill.read` metric, and nowhere else.

A skills listing serves at most 1000 skills. The query keeps one version per skill before the cap applies, so a skill carried by many assigned plugins takes one slot rather than one per plugin; an endpoint with more skills than the cap has its later skills by name cut off and the resolver logs a warning. The warning fires only when a skill past the cap exists, not when the listing fills it exactly.

The management API exposes the most recent such read as `last_loaded_over_mcp_at` on `SkillDistribution` (returned by `skills.distribute`) and `PluginSkillDistribution` (returned by `skills.listDistributions`). It is a per-skill value, so every distribution of one skill carries the same time, and it reflects only reads over a Speakeasy-hosted MCP server: a load of the same skill reported by a coding-agent hook does not move it.

Reads are counted on the `mcp.skill.read` counter with `gram.mcp.surface` (`hosting` or `platform`), `gram.mcp.skill.origin` (`distribution` for a managed skill, `embedded` for the platform catalog) and the network surface. Reads on the external Platform MCP are not counted: `gram.mcp.surface` has no value for that endpoint. Only successful reads count, and nothing is recorded per server or per skill, so the counter stays bounded.

## Client support

The extension is new, and a client must implement it to benefit: it issues `skills/list` and `skills/get` only after seeing the capability, and since skills are absent from `resources/list` a client without the extension never encounters them unless given a URI.

- **fast-agent** implements the client side of the extension, so an agent built on it discovers and loads skills from a Speakeasy-hosted server without further configuration.
- **Hugging Face** has a server-side implementation. It is not a client, but it is a useful interoperability reference for the entry shape and error codes.

Verification, approval and the acting window are the host's job under the extension. Speakeasy supplies consistent entries and bytes; it cannot prevent a client that skips verification from loading stale content, and a digest match is not a trust anchor, since the same server supplies both the entry and the content.

## Package delivery is unchanged

Distributing a skill to a plugin is one action with two delivery routes. The published package still carries the skill under `skills/` as described in [Package format](../plugins/package-format.md), and the plugin's hosted MCP servers serve it to the same recipients. Pinning applies to both routes. Undistributing removes it from both. A newly recorded version of a skill tracking "latest" is visible over MCP on the next `skills/list` and reaches the package on its next publication.

A plugin without a hosted MCP server, or one whose servers are all remote MCP servers, still delivers its skills through the package only; Speakeasy does not proxy skills through remote servers.

## In the dashboard

All three surfaces are shown only when the flag is on for the organization.

- **Skill detail, Usage page.** The plugin distributions section says that the plugins' hosted MCP servers serve the skill, shows the served digest of the current version (`sha256:...`, copyable), and lists per plugin which hosted servers serve it, or that the plugin includes no hosted server. A plugin server keyed by an MCP server record is resolved to its toolset through the project's MCP server list, so a plugin that includes a non-canonical wrapper of a toolset is shown serving it, as the backend does. The version history shows each version's served digest beside its canonical hash.
- **Plugin detail, Skills section.** The description notes that agents connected to the plugin's hosted servers load the skills over MCP, and each skill shows when it was last loaded over MCP, or that it never was.
- **MCP server detail, Skills tab** (`/mcp/<slug>/skills`). Lists the skills served over this endpoint grouped by the plugin that carries them, with the resolved version, last load over MCP, and a warning when the plugin is assigned to nobody. Writers add a skill by picking one and distributing it to a plugin that includes this server, and remove one by ending that distribution; both are the ordinary distribute and undistribute operations, so there is no new write path. When no plugin includes the server, the tab points to Plugins. A reader with skill read but neither organization read nor plugin write cannot list plugins, and the tab says so instead of loading.

## Platform MCP

Assessment for the Platform MCP, recorded per the maintaining-platform-mcp skill.

- **Outcome.** An administrator can give a skill to a plugin, take it back, and see which plugins carry which skills, knowing that each of those plugins also serves the skill to agents connected to its hosted MCP servers.
- **Actor and target.** Organization administrators (writes) and members (reads) acting on a skill distribution in an explicit project.
- **Existing tools.** `distribute_skill`, `undistribute_skill`, `list_skill_distributions` and `get_skill` already express this outcome. Their descriptions and result messages describe distribution as giving a skill to a plugin "so agents there start loading it", which is delivery-neutral: it was true of the package route and is now true of both routes. The shipped `review-skill-suggestions` skill says a new version reaches every plugin that follows the skill's latest version, which also remains accurate.
- **Decision: update `list_skill_distributions`; no new tool.** No new tool, because every write the dashboard's Skills tab performs is an existing distribute or undistribute, and the platform toolset's own catalog is delivered to the agent by the protocol itself, which no tool can improve on. No description change, because no tool text describes the delivery mechanism incompletely or names the package as the only route; rewriting delivery-neutral text to enumerate routes would tie the tool contract to a flagged rollout. The one result change: each `list_skill_distributions` row carries the optional `last_loaded_over_mcp_at` (RFC 3339), the same value the management API reports on `PluginSkillDistribution`, so an agent asked "is anyone loading this skill over MCP" can answer from the listing the dashboard answers from. Its schema description says it is per skill: every distribution of one skill reports the same time, and it says nothing about which plugin served the read. It is absent when the skill was never loaded over MCP, which is every row for an organization without the flag, so the addition is invisible there.
- **Evidence.** The Platform MCP skill tool tests cover the new field on the unit mapping and end to end (a recorded MCP read shows up on the next listing); audiences, authorization and the tool's inputs are unchanged.

The Platform MCP's `search_docs` and `read_doc` tools index the pinned `mcp-setup-docs` module, not this repository's `docs/` tree, so this page is contributor documentation and is not agent-visible through those tools.

## Out of scope

Deliberately not built in this iteration: multi-file skills and supporting files, `resources/directory/read`, change notifications when a listing changes, proxying skills through remote MCP servers, per-plugin endpoints, and a non-zero cache TTL. A skill manifest is limited to 64 KiB, far under the extension's 16 MiB per-skill limit, so no entry needs the limit checks the extension describes.
