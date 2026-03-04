# RFC: Deployment Tags & Staged Releases

**Status:** Draft
**Author:** Adam Bull
**Created:** 2026-03-02
**Last Updated:** 2026-03-02

## Abstract

This RFC proposes adding OCI-style deployment tags to Gram, enabling staged releases where changes don't go live immediately. Users will work in a "Draft" environment and explicitly promote to "Live" when ready.

## Motivation

Currently, all changes to Gram MCP servers go live immediately:
- Uploading an OpenAPI spec instantly updates the MCP
- Adding/removing tools takes effect immediately
- No way to preview changes before they affect clients
- No staging environment for CI/CD workflows

This creates problems for teams who need:
- Review changes before they go live
- CI pipelines with approval gates
- Rollback capability with clear version history
- Multiple environments (staging, production)

## Proposal

### Core Model: OCI-Style Tags

Adopt the OCI (Open Container Initiative) model used by Docker registries:

```
project/
├── deployments/           # Immutable snapshots
│   ├── deploy-001
│   ├── deploy-002  ← main
│   └── deploy-003  ← latest
└── tags/                  # Mutable pointers
    ├── latest → deploy-003
    ├── main → deploy-002
    └── staging → deploy-003
```

**Key properties:**
- **Deployments are immutable** - once created, never modified
- **Tags are mutable pointers** - can be moved to point to any deployment
- **Last write wins** - concurrent edits create separate deployments; tag points to most recent
- **Content addressable** - old deployments always accessible by ID

### Default Tags

Two tags are created automatically for every project:

| Tag | Purpose | UI Name |
|-----|---------|---------|
| `latest` | Most recent deployment | Draft |
| `main` | What MCP clients see | Live |

### New Behavior

**Before (current):**
```
Upload source → Deployment created → MCP clients see changes immediately
```

**After (proposed):**
```
Upload source → Deployment created → Tagged as 'latest'
                                   → MCP clients still see 'main'
                                   → User promotes when ready
```

### User Interface

Global environment tabs appear on all project pages:

```
┌───────────────────────────────────────────────────┐
│  Gram   Sources   MCPs   Deployments              │
├───────────────────────────────────────────────────┤
│  [ Draft ]  [ Live ]            [Promote to Live] │
├───────────────────────────────────────────────────┤
│  3 pending changes:                               │
│  • +2 tools • -1 tool • 1 source updated          │
└───────────────────────────────────────────────────┘
```

- **Draft tab**: Shows `latest` tag state. Editable.
- **Live tab**: Shows `main` tag state. Read-only.
- **Promote button**: Updates `main` to point to current `latest` deployment.

### CLI

```bash
# Deploy (creates deployment, tags as 'latest')
gram push
gram push --tag staging      # Also tag as 'staging'

# Promote
gram promote                 # latest → main
gram promote <id> --to main  # Specific deployment
gram promote --dry-run       # Preview without executing

# Inspect
gram tags                    # List all tags
gram diff                    # Diff between latest and main
gram history                 # Deployment history with diffs
```

### API

**New endpoints:**

```
POST   /projects/{id}/tags              # Create tag
GET    /projects/{id}/tags              # List tags
GET    /projects/{id}/tags/{name}       # Get tag details
PUT    /projects/{id}/tags/{name}       # Update tag (promote)
DELETE /projects/{id}/tags/{name}       # Delete tag
GET    /projects/{id}/tags/{name}/history
```

**Modified behavior:**
- `POST /deployments` and `evolve`: Auto-tag as `latest`, do NOT update `main`
- MCP endpoint: Serve from `main` by default, `?tag=X` for others

### MCP Client Routing

Clients specify which tag to connect to via query parameter:

```
# Default (main/Live)
https://mcp.getgram.ai/project-slug

# Specific tag
https://mcp.getgram.ai/project-slug?tag=staging
https://mcp.getgram.ai/project-slug?tag=latest
```

### Version History & Diffs

Diffs are precomputed when deployments complete:

```json
{
  "compared_to": "deploy-002",
  "tools": {
    "added": [{ "urn": "...", "name": "create_user" }],
    "removed": [{ "urn": "...", "name": "legacy_auth" }],
    "modified": [{ "urn": "...", "name": "get_user", "changes": ["schema"] }]
  },
  "sources": {
    "added": [],
    "removed": [],
    "modified": [{ "slug": "api", "type": "openapiv3" }]
  }
}
```

Stored as `diff_from_previous` JSONB column on deployments table.

### Rollback

Rollback is simply promoting an older deployment:

```bash
gram promote deploy-001 --to main
```

No special rollback mechanism needed - tags are just pointers.

## Database Schema

```sql
CREATE TABLE deployment_tags (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id),
  name TEXT NOT NULL,
  deployment_id UUID REFERENCES deployments(id),
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(project_id, name)
);

CREATE TABLE deployment_tag_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tag_id UUID NOT NULL REFERENCES deployment_tags(id),
  previous_deployment_id UUID REFERENCES deployments(id),
  new_deployment_id UUID REFERENCES deployments(id),
  changed_by UUID REFERENCES users(id),
  changed_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_deployment_tags_project_name
  ON deployment_tags(project_id, name);

ALTER TABLE deployments
  ADD COLUMN diff_from_previous JSONB;
```

## Migration Strategy

1. Create `deployment_tags` and `deployment_tag_history` tables
2. For each project with deployments:
   - Create `latest` tag pointing to current active deployment
   - Create `main` tag pointing to current active deployment
3. Update MCP serving logic to read from `main` tag
4. Update `createDeployment`/`evolve` to auto-tag as `latest` only

### Open Question: Existing Projects

**Option A: Force staged workflow (breaking change)**
- All projects switch to requiring explicit promotion
- Requires user communication (changelog, email, in-app banner)

**Option B: Preserve instant behavior, opt-in to staging**
- Add `auto_promote` project setting
- Existing projects default to `true` (current behavior)
- New projects default to `false`

Recommendation: TBD based on user feedback.

## Concurrent Edits

Following the OCI model, concurrent edits are handled simply:

1. User A uploads source → creates `deploy-004`, tagged as `latest`
2. User B uploads source → creates `deploy-005`, tagged as `latest`
3. `latest` now points to `deploy-005`
4. `deploy-004` still exists, accessible by ID

No merge conflicts. No revision accumulation. Last write wins for tags.

Users needing User A's changes can:
- View deployment history
- Promote `deploy-004` directly
- Create a custom tag pointing to `deploy-004`

## Alternatives Considered

### Revisions Layer (Git-style staging area)

Changes accumulate as "revisions" until explicitly bundled into a deployment.

**Rejected because:**
- Adds complexity (revisions + deployments + tags = 3 concepts)
- Merge conflicts for concurrent edits
- Stale revisions problem
- No efficiency gain - same work just delayed

### Linear Versions (v1, v2, v3)

Simple incrementing version numbers instead of named tags.

**Rejected because:**
- Less flexible than tags
- Can't have multiple named environments
- Doesn't match industry standard (OCI, Docker)

### Status-based (draft/live flags)

Deployments have status field instead of separate tags.

**Rejected because:**
- Can't have multiple environments
- Less intuitive than pointer model
- Harder to implement custom workflows

## Future Work

**Out of scope for initial release:**

1. **GitHub Action** - `getgram/action` for CI/CD with environment-based approvals
2. **Content-addressable storage** - Dedupe identical asset uploads
3. **Tool extraction caching** - Cache by content hash
4. **Auto-pruning** - Archive old untagged deployments

## Implementation Plan

### Phase 1: Backend - Tags Infrastructure
- Database migration
- Goa API design for tag endpoints
- Tag CRUD service implementation
- Update MCP serving to read from tags
- Migration script to create initial tags

### Phase 2: Backend - Diff Computation
- Implement diff computation in ProcessDeployment activity
- Store diff JSON on deployment record

### Phase 3: CLI Updates
- `gram push` auto-tag behavior
- `gram promote` command
- `gram tags` command
- `gram diff` command

### Phase 4: Frontend - Environment Tabs
- Global Draft/Live tab component
- Tab state management
- Pending changes banner
- Promote button with confirmation
- Read-only mode for Live tab

### Phase 5: Frontend - Diff View
- Diff visualization component
- Tools/sources changes display

## Security Considerations

- Tag operations require project write permissions
- `main` tag cannot be deleted (protected)
- Tag history provides audit trail for compliance
- No new attack vectors introduced

## Testing Plan

**Core flow:**
- Push creates deployment tagged as `latest`
- Promote updates `main`, clients see new tools
- Draft editable, Live read-only
- Diff computation accurate
- Rollback via promote works

**Tag management:**
- Create/list/delete custom tags
- Tag history audit trail

**MCP routing:**
- No `?tag` param → serves `main`
- `?tag=staging` → serves that tag
- Invalid tag → appropriate error

**Edge cases:**
- First deployment (no previous diff)
- Promote when `latest` == `main` (no-op)
- Concurrent deployments
- Invalid tag names (validation)

## References

- [OCI Distribution Spec](https://github.com/opencontainers/distribution-spec)
- [Docker Registry HTTP API](https://docs.docker.com/registry/spec/api/)
