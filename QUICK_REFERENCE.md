# Staging & Publishing Toolsets - Quick Reference

## What We're Building

Add the ability to test toolset changes in a "staging" environment before releasing them to end users, while maintaining the current simple "iteration mode" for quick prototyping.

## Key Concepts

### Two Editing Modes

1. **Iteration Mode (default)**
   - Changes go live immediately
   - No releases created
   - Current behavior - perfect for prototyping
   - Most toolsets will stay in this mode

2. **Staging Mode (opt-in)**
   - Changes go to a staging copy
   - Must explicitly publish to make live
   - Creates versioned releases
   - For production-ready servers

### Staging Implementation

- Staging = actual duplicate toolset with `parent_toolset_id`
- UI shows as single entity with staging/live toggle
- Full feature parity (playground, install page, etc.)

### Release Model

A release captures complete state via:
- **Source State**: Which deployment + which prompt templates
- **Toolset Version**: Which tools are included
- **Variations**: Point-in-time snapshot of all customizations
- **Toolset Metadata**: Name, description, config (versioned)

## Database Schema Summary

### New Tables
- `toolset_releases` - Release records
- `source_states` - Deployment + system source combinations
- `system_source_states` - Prompt template snapshots
- `tool_variations_group_versions` - Variation group snapshots

### Modified Tables
- `toolsets` - Add: `parent_toolset_id`, `editing_mode`, `current_release_id`, `predecessor_id`, `version`, `history_id`
- `tool_variations` - Add: `predecessor_id`, `version`

## API Endpoints (New)

```
POST   /rpc/toolsets.createStagingVersion
POST   /rpc/toolsets.publishStagingVersion
DELETE /rpc/toolsets.discardStagingVersion
POST   /rpc/toolsets.switchEditingMode
GET    /rpc/toolsets.listReleases
GET    /rpc/toolsets.getReleaseDetails
POST   /rpc/toolsets.rollbackToRelease
```

## Implementation Order

### Week 1-2: Database Foundation
- Add staging support columns
- Create releases tables
- Add versioning to variations
- Add versioning to toolset metadata

### Week 3-4: Backend Services
- Releases service (create, get, list, rollback)
- Staging management (create, publish, discard)
- State capture utilities
- Variation versioning logic

### Week 5: API Layer
- Goa design updates
- Endpoint implementations
- Client generation

### Week 6-7: Frontend
- Staging/live toggle
- Editing mode switcher
- Publish dialog
- Release history view
- Rollback UI

### Week 8: MCP Server
- Load toolset from release
- Staging install URLs

### Week 9: Testing
- Integration tests
- Migration testing
- Documentation

### Week 10: Rollout
- Feature flag
- Beta testing
- GA launch

## Key Files to Modify

### Backend
- `server/database/schema.sql` - Schema changes
- `server/internal/toolsets/impl.go` - Core toolset service
- `server/internal/releases/` (new) - Releases service
- `server/design/toolsets/design.go` - API definitions
- `server/design/shared/toolset.go` - Type definitions

### Frontend
- `client/src/pages/Toolset/ToolsetHeader.tsx` - Add toggle
- `client/src/components/StagingControls/` (new) - Staging UI
- `client/src/components/Releases/` (new) - Release UI

## First Steps

1. **Schema Changes**: Start with `toolsets` table modifications
2. **Simple Test**: Create staging toolset programmatically
3. **Releases Table**: Add releases infrastructure
4. **Service Method**: Implement `CreateStagingVersion`
5. **API Endpoint**: Wire up to Goa
6. **Basic UI**: Add toggle to switch between staging/live

## Testing Strategy

### Unit Tests
- State capture logic
- Version creation
- Release creation

### Integration Tests
- Full staging flow: create → modify → publish
- Rollback flow: create releases → rollback → verify
- Iteration mode still works

### Manual Testing
- Create toolset in iteration mode → verify immediate updates
- Switch to staging mode → create staging → modify → publish → verify
- Test MCP server serves correct version
- Test playground with staging vs live

## Common Pitfalls to Avoid

1. **Don't break iteration mode** - Most users won't use staging
2. **Cache invalidation** - Clear caches when publishing/rolling back
3. **Foreign key cascades** - Use RESTRICT for release references
4. **Migration safety** - Test on prod snapshot before deploying
5. **Functions complexity** - Defer function versioning to v2

## Open Questions & Decisions Needed

- [ ] Rollback scope: toolset-only or project-level?
- [ ] Function staging: defer or implement?
- [ ] Release naming: auto-increment or semver?
- [ ] UI placement: tabs vs dropdown vs dedicated page?

## Success Criteria

- ✅ Can create staging version of toolset
- ✅ Can modify staging without affecting live
- ✅ Can publish staging as new release
- ✅ Can rollback to previous release
- ✅ Iteration mode still works as before
- ✅ MCP server serves correct version
- ✅ No performance regression

## Resources

- [Full Implementation Plan](./IMPLEMENTATION_PLAN.md)
- [RFC Document](https://www.notion.so/speakeasyapi/RFC-Staging-and-Releasing-Toolset-Versions-28e726c497cc800dbbabc7a7caf58a4c)
- [Linear Project](https://linear.app/speakeasy/project/stage-and-promote-toolset-changes-scoped-cce6ebb767dd/overview)
