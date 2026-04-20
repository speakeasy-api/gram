# Shared skills producer

This directory hosts the shared Node runtime for Claude/Cursor skill discovery, metadata enrichment, deterministic packaging, and upload request shaping.

Current status: additive only. Hook installer commands are not switched yet.

## Module layout

- `send-hook.mts`
  - production hook entrypoint for Claude/Cursor hook config
  - reads hook payload JSON from stdin
  - enriches payload via shared producer core
  - posts enriched payload to hooks endpoint
  - optionally spawns detached upload worker (best-effort)
- `producer-core.mts`
  - orchestration/facade module
  - stable public API surface for callers/tests
- `constants.mts`
  - shared constants (status taxonomy, limits, discovery roots)
- `discovery.mts`
  - skill name extraction from hook payload
  - deterministic root precedence and skill root resolution
- `frontmatter.mts`
  - `x-gram-ignore` detection
  - registry-managed frontmatter stripping for hash normalization
- `packaging.mts`
  - canonical file collection + ignore handling
  - deterministic content hash
  - deterministic ZIP generation
  - `skills.capture` request shaping helper
- `upload.mts`
  - upload request validation/execution helpers
  - detached worker spawning and request-file serialization
  - temp request files are written with restricted permissions
  - worker request files exclude `Gram-Key` / `Gram-Project` and worker injects from runtime env
- `producer-upload-worker.mts`
  - detached background worker entrypoint
  - executes one upload request from temp request file
- `cache.mts`
  - recent-seen upload suppression cache (`~/.gram/skills-upload-cache.json`)
  - TTL-based best-effort dedupe gate before worker spawn
- `producer-core.test.mts`
  - focused unit/integration-style tests for discovery/enrichment/packaging
- `upload.test.mts`
  - focused tests for upload execution + worker file flow
- `cache.test.mts`
  - focused tests for cache keying, TTL, and suppression behavior

## Current behavior

For Skill tool invocations, payload enrichment adds `additional_data.skills[0]` with fields such as:

- `name`
- `scope`
- `discovery_root`
- `source_type`
- `resolution_status`
- `content_sha256` (canonical normalized content hash, when resolved/hashable)
- `asset_format` (`zip`, when resolved/hashable)

### Resolution flow (high level)

1. Extract skill name from tool input.
2. Resolve root by deterministic precedence per agent.
3. Validate `SKILL.md` exists.
4. Respect `metadata.x-gram-ignore: true` (`skipped_by_author`).
5. Apply canonical walk + limits + hash.
6. Build deterministic ZIP.
7. Shape `skills.capture` upload request.
8. Optionally spawn detached upload worker (best-effort, non-blocking).

## Return shapes

`buildSkillMetadata(payload, options)` returns:

```js
{
  metadata: { skills: [/* one skill item */] },
  uploadRequest: null | {
    method: "POST",
    url: "https://.../rpc/skills.capture",
    headers: { ... },
    body: Buffer
  }
}
```

`buildEnrichedHookPayload(payload, options)` always returns:

```js
{
  payload: { ...originalHookPayload, additional_data: { ... } },
  uploadRequest: null | { ... }
}
```

Hook runtime behavior:

- hook entrypoint enriches payload and posts to hooks endpoint.
- upload execution is optional and controlled by env flag (`GRAM_SKILLS_UPLOAD_ENABLED=true`).
- upload failures never block hook flow (fail-open).
- detached worker failures are fail-open and do not break hook flow.
- recent identical uploads are suppressed by a user-local cache.

## Hook command usage

```bash
# Claude example
cat hook-payload.json | node hooks/shared-producer/send-hook.mts --agent=claude

# Cursor example
cat hook-payload.json | node hooks/shared-producer/send-hook.mts --agent=cursor
```

## Environment fallbacks

- `GRAM_HOOK_AGENT` when `--agent` is not provided

- `GRAM_SKILLS_RESOLUTION_STATUS` to override `resolution_status` only when compatible with discovery (it will not force unresolved skills to `resolved`)
- `GRAM_HOOKS_SERVER_URL`, `GRAM_API_KEY`, `GRAM_PROJECT_SLUG` for upload request shaping
- `GRAM_SKILLS_UPLOAD_ENABLED=true` to enable detached upload worker execution from hook entrypoint
- `GRAM_SKILLS_UPLOAD_ENABLED` defaults to disabled unless explicitly set to `true`
- `GRAM_SKILLS_UPLOAD_CACHE_TTL_MS` optional TTL override for recent-seen suppression (default 900000 ms / 15 min)
- `GRAM_HOOKS_DEBUG=true` to enable JSONL debug logs written to `~/.gram/hooks-debug.log`
- `GRAM_HOOKS_DEBUG_LOG_PATH` optional explicit path override for the debug log file

## Notes

- `X-Gram-Skill-Content-Sha256` in shaped upload requests is SHA-256 of the ZIP body bytes (matches current server validation contract).
- `content_sha256` in metadata is canonical normalized skill-content hash used for version identity semantics.
- cache key for suppression uses `(project_slug, skill_name, content_sha256)`.
