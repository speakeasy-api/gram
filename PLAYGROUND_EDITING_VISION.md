# Playground Editing Experience - Deep Analysis & Vision

## Current State Analysis

### What We Have

#### 1. **Existing Tool Management Functionality**
- **ToolList Component** (`src/components/tool-list/ToolList.tsx`):
  - Selection mode for adding tools to toolsets
  - Remove mode for removing tools from toolsets
  - Inline editing of tool names and descriptions via EditableText
  - Grouping by source (package, function, custom)
  - Round-robin method sorting for visual variety
  - Command palette integration for keyboard navigation

- **AddToolsDialog** (`src/pages/toolsets/AddToolsDialog.tsx`):
  - Search and filter tools by source
  - Select multiple tools to add to a toolset
  - Shows which tools are already in the toolset

- **ToolCard** (`src/pages/toolsets/ToolCard.tsx`):
  - Editable tool name and description
  - Auto-summarize toggle
  - Update tool variations via API

#### 2. **Current Playground Structure**
```
┌─────────────────────────────────────────────────────────────┐
│  [Toolset Selector]                                          │
├─────────────────────────────────────────────────────────────┤
│  ▼ Toolset Info                                              │
│     Name, Slug, Description, Tool Count                      │
├─────────────────────────────────────────────────────────────┤
│  [Environment Selector]                                      │
├─────────────────────────────────────────────────────────────┤
│  ▼ Tools (N)                                                 │
│     [Package Name]                                           │
│       ☑ Tool 1 - description        [GET]                    │
│       ☑ Tool 2 - description        [POST]                   │
│     [Functions]                                              │
│       ☑ Function Tool - description                          │
├─────────────────────────────────────────────────────────────┤
│  ▼ Model Settings                                            │
│     Temperature slider                                       │
└─────────────────────────────────────────────────────────────┘
```

#### 3. **Available API Operations**
- `toolsets.update({ slug, toolUrns[], name, description, ... })` - Update entire toolset including tool list
- `variations.upsertGlobal()` - Update individual tool variations (name, description, etc.)
- `templates.update()` - Update prompt templates

### The Gap: What's Missing for OpenAI-style Editing

Based on OpenAI's Assistants Playground pattern (inferred from your description), the key missing pieces are:

## Vision: Playground as a Temporary Editing Sandbox

### Core Philosophy
**The playground should be a lightweight, ephemeral workspace where you can:**
1. Test toolset configurations without modifying the source toolset
2. Add/remove tools temporarily to see how they work together
3. Edit tool metadata on the fly
4. See live logs of tool execution
5. **Eventually** choose to persist changes back to the toolset

### Proposed Architecture

#### **Phase 1: Local State Management (Immediate)**
Add a "working copy" layer in the playground that shadows the real toolset:

```typescript
interface PlaygroundState {
  // Original toolset data from server
  originalToolset: Toolset;

  // Working copy with local modifications
  workingToolset: {
    name: string;
    description: string;
    toolUrns: string[]; // Can add/remove without API calls
  };

  // Tool metadata overrides (local only, not persisted)
  toolOverrides: Map<string, {
    name?: string;
    description?: string;
    enabled?: boolean; // Hide from chat without removing
  }>;

  // Track what's changed
  hasChanges: boolean;
  changes: {
    addedTools: string[];
    removedTools: string[];
    modifiedTools: string[];
    metadata: { name?: boolean; description?: boolean };
  };
}
```

**Key Benefits:**
- Fast, instant updates (no API calls)
- Can experiment freely
- Easy to discard changes
- Can implement undo/redo later

#### **Phase 2: Change Persistence (Future)**
Add UI to save changes:

```tsx
{hasChanges && (
  <div className="sticky bottom-0 border-t bg-background p-4 flex gap-2">
    <Button onClick={discardChanges} variant="ghost">
      Discard Changes
    </Button>
    <Button onClick={saveChanges} variant="default">
      Save to Toolset
    </Button>
  </div>
)}
```

---

## Detailed Implementation Plan

### 1. **Tool Management in Left Panel**

#### A. **Add "+" Button to Tools Section Header**
```tsx
<CollapsibleTrigger className="flex w-full items-center justify-between px-4 py-2.5">
  <div className="flex items-center gap-1.5">
    <span>Tools ({workingToolset.toolUrns.length})</span>
    <ChevronDownIcon />
  </div>

  {/* NEW: Action buttons on the right */}
  <div className="flex items-center gap-1">
    <Button
      size="icon-sm"
      variant="ghost"
      onClick={(e) => {
        e.stopPropagation(); // Don't collapse
        setShowAddToolsDialog(true);
      }}
    >
      <PlusIcon className="size-3.5" />
    </Button>
  </div>
</CollapsibleTrigger>
```

#### B. **Make Each Tool Row Editable & Removable**
```tsx
<div className="group flex items-center justify-between px-4 py-2.5 hover:bg-muted/50">
  <div className="flex gap-2.5 items-center min-w-0 flex-1">
    <Checkbox
      checked={!toolOverrides.get(tool.id)?.enabled === false}
      onCheckedChange={(checked) => {
        updateToolOverride(tool.id, { enabled: !!checked });
      }}
    />

    {/* Editable name */}
    <EditableText
      label="Tool Name"
      value={toolOverrides.get(tool.id)?.name ?? tool.name}
      onSubmit={(newName) => {
        updateToolOverride(tool.id, { name: newName });
      }}
    >
      <span className="text-xs font-medium truncate hover:underline cursor-pointer">
        {toolOverrides.get(tool.id)?.name ?? tool.name}
      </span>
    </EditableText>
  </div>

  {/* Show actions on hover */}
  <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
    {tool.httpMethod && <MethodBadge method={tool.httpMethod} />}

    <Button
      size="icon-sm"
      variant="ghost"
      onClick={() => removeTool(tool.id)}
    >
      <XIcon className="size-3.5" />
    </Button>
  </div>
</div>
```

#### C. **Add Tools Dialog (Reuse Existing)**
Invoke the existing `AddToolsDialog` but in a playground-specific mode:
- Pass current `workingToolset.toolUrns` as `selectedUrns`
- On submit, update local state (not API)
- Show "Added to playground" instead of "Added to toolset"

### 2. **Toolset Info Editing**

Make the Toolset Info section fully editable:

```tsx
<CollapsibleContent className="px-4 py-3 space-y-2.5">
  <div className="space-y-1.5">
    <Label>Name</Label>
    <EditableText
      label="Toolset Name"
      value={workingToolset.name}
      onSubmit={(name) => updateWorkingToolset({ name })}
    >
      <Type variant="small" className="font-medium hover:underline cursor-pointer">
        {workingToolset.name}
      </Type>
    </EditableText>
  </div>

  <div className="space-y-1.5">
    <Label>Description</Label>
    <EditableText
      label="Toolset Description"
      value={workingToolset.description}
      onSubmit={(description) => updateWorkingToolset({ description })}
      lines={3}
    >
      <Type variant="small" className="text-muted-foreground hover:underline cursor-pointer">
        {workingToolset.description || "Click to add description"}
      </Type>
    </EditableText>
  </div>

  {/* Show change indicator */}
  {hasMetadataChanges && (
    <Badge variant="secondary" className="text-xs">
      Unsaved changes
    </Badge>
  )}
</CollapsibleContent>
```

### 3. **Visual Change Indicators**

Add subtle indicators for what's changed:

```tsx
// Modified tool name
<span className="flex items-center gap-1">
  {toolOverrides.get(tool.id)?.name ?? tool.name}
  {toolOverrides.has(tool.id) && (
    <DotIcon className="size-2 text-amber-500" />
  )}
</span>

// Newly added tool (not in original toolset)
{!originalToolset.toolUrns.includes(tool.id) && (
  <Badge variant="outline" className="text-xs">New</Badge>
)}

// Tool will be removed (unchecked)
{!toolEnabled && (
  <Badge variant="secondary" className="text-xs">Hidden</Badge>
)}
```

### 4. **Change Summary Panel (Optional but Powerful)**

Add a collapsible "Changes" section at the bottom:

```tsx
{hasChanges && (
  <div className="border-t">
    <Collapsible>
      <CollapsibleTrigger>
        <div className="flex items-center gap-1.5 px-4 py-2.5">
          <span className="text-[11px] font-semibold uppercase">
            Changes ({totalChanges})
          </span>
          <ChevronDownIcon />
        </div>
      </CollapsibleTrigger>

      <CollapsibleContent className="px-4 py-2 text-xs space-y-2">
        {changes.addedTools.length > 0 && (
          <div>
            <span className="font-medium text-green-600">
              +{changes.addedTools.length} tools added
            </span>
            <ul className="ml-4 text-muted-foreground">
              {changes.addedTools.map(urn => (
                <li key={urn}>• {getToolName(urn)}</li>
              ))}
            </ul>
          </div>
        )}

        {changes.removedTools.length > 0 && (
          <div>
            <span className="font-medium text-red-600">
              -{changes.removedTools.length} tools removed
            </span>
          </div>
        )}

        {changes.metadata.name && (
          <div className="text-amber-600">
            • Toolset name changed
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  </div>
)}
```

---

## Implementation Phases

### **Phase 1: Foundation (Week 1)**
- [ ] Create `usePlaygroundState` hook to manage working copy
- [ ] Add local tool overrides (enable/disable, rename)
- [ ] Make tool names editable inline
- [ ] Add remove button to each tool (local only)
- [ ] Show visual indicators for changed tools

### **Phase 2: Tool Management (Week 1-2)**
- [ ] Add "+" button to Tools section header
- [ ] Wire up AddToolsDialog in playground mode
- [ ] Implement tool filtering (show only enabled tools in chat)
- [ ] Add tool description editing
- [ ] Group management (expand/collapse all, sort options)

### **Phase 3: Toolset Metadata (Week 2)**
- [ ] Make toolset name editable
- [ ] Make toolset description editable
- [ ] Add change indicators to metadata fields
- [ ] Implement basic validation (name regex, required fields)

### **Phase 4: Change Tracking UI (Week 2-3)**
- [ ] Build change summary panel
- [ ] Add "Discard Changes" button
- [ ] Add "Save to Toolset" button (calls `toolsets.update()`)
- [ ] Handle optimistic updates and error states
- [ ] Add confirmation dialogs for destructive actions

### **Phase 5: Polish & UX (Week 3)**
- [ ] Add keyboard shortcuts (⌘+S to save, ESC to discard)
- [ ] Implement undo/redo stack
- [ ] Add loading states during save
- [ ] Toast notifications for success/errors
- [ ] Autosave draft state to localStorage

### **Phase 6: Advanced Features (Future)**
- [ ] Staging area (save drafts without publishing)
- [ ] Version history / snapshots
- [ ] Share playground configuration via URL
- [ ] Compare current vs original (diff view)
- [ ] Bulk operations (remove all tools from source X)
- [ ] Tool templates (common configurations)

---

## Key Design Decisions

### 1. **Why Local State First?**
- **Speed**: No API latency for every change
- **Experimentation**: Users can try things without fear
- **Undo/Redo**: Easy to implement with immutable state
- **Offline**: Works even if server is slow/down

### 2. **Checkbox vs Remove Button**
Both patterns exist in the wild:
- **OpenAI**: Uses checkboxes (our current approach)
- **Anthropic Console**: Uses remove buttons

**Recommendation**: Keep checkboxes but change semantics:
- Unchecked = "Hidden from chat" (still in toolset)
- Remove button = "Remove from playground session"

This gives users fine-grained control:
- Temporarily disable tools without removing
- Truly remove tools they never want

### 3. **When to Persist Changes?**
**Option A: Manual Save** (Recommended)
- Explicit "Save" button
- Clear control over when changes apply
- Can discard mistakes easily

**Option B: Autosave**
- Saves every N seconds if changes exist
- Less cognitive load
- Risk of unwanted changes

**Option C: Save on Navigate Away**
- Prompt "You have unsaved changes" dialog
- Like Google Docs
- Can be annoying

**Recommendation**: Start with Manual Save (Option A), add autosave to localStorage as draft recovery.

### 4. **Tool Editing Scope**
**What should be editable in playground?**

✅ **Should be editable (local only):**
- Tool name (display name in chat)
- Tool description (shown to LLM)
- Enable/disable (show in chat)

❌ **Should NOT be editable here:**
- Tool implementation (OpenAPI spec, function code)
- Authentication/headers (security risk)
- HTTP method/path (would break tool)

**Why?** Playground is for **configuration**, not **implementation**. Editing actual tool behavior should be done in the toolset detail page or deployment flow.

### 5. **Environment Switching**
**What happens to working copy when environment changes?**

**Option A: Reset to New Environment**
- Discard all changes
- Load fresh instance for new environment
- Simple, but loses work

**Option B: Ask User**
- "You have unsaved changes. Discard or save first?"
- More control, more clicks

**Option C: Keep Local Changes Across Environments**
- Working copy independent of environment
- Complex to reason about

**Recommendation**: Option B (ask user). Changes are tied to a specific toolset+environment combination.

---

## Technical Implementation Details

### State Management Structure

```typescript
// New hook: usePlaygroundState
export function usePlaygroundState(
  toolsetSlug: string,
  environmentSlug: string
) {
  const { data: instance } = useInstance({ toolsetSlug, environmentSlug });
  const { data: toolset } = useToolset(toolsetSlug);

  // Local state for working copy
  const [workingState, setWorkingState] = useState<WorkingState>({
    metadata: {
      name: toolset?.name ?? "",
      description: toolset?.description ?? "",
    },
    toolUrns: instance?.tools?.map(t => t.toolUrn) ?? [],
    toolOverrides: new Map(),
  });

  // Compute changes
  const changes = useMemo(() => {
    const original = new Set(instance?.tools?.map(t => t.toolUrn) ?? []);
    const current = new Set(workingState.toolUrns);

    return {
      addedTools: [...current].filter(urn => !original.has(urn)),
      removedTools: [...original].filter(urn => !current.has(urn)),
      modifiedTools: [...workingState.toolOverrides.keys()],
      metadata: {
        name: workingState.metadata.name !== toolset?.name,
        description: workingState.metadata.description !== toolset?.description,
      },
    };
  }, [workingState, instance, toolset]);

  const hasChanges =
    changes.addedTools.length > 0 ||
    changes.removedTools.length > 0 ||
    changes.modifiedTools.length > 0 ||
    changes.metadata.name ||
    changes.metadata.description;

  // Actions
  const addTool = (toolUrn: string) => {
    setWorkingState(prev => ({
      ...prev,
      toolUrns: [...prev.toolUrns, toolUrn],
    }));
  };

  const removeTool = (toolUrn: string) => {
    setWorkingState(prev => ({
      ...prev,
      toolUrns: prev.toolUrns.filter(urn => urn !== toolUrn),
    }));
  };

  const updateToolOverride = (toolUrn: string, override: ToolOverride) => {
    setWorkingState(prev => {
      const overrides = new Map(prev.toolOverrides);
      overrides.set(toolUrn, { ...overrides.get(toolUrn), ...override });
      return { ...prev, toolOverrides: overrides };
    });
  };

  const saveChanges = async () => {
    await client.toolsets.update({
      slug: toolsetSlug,
      updateToolsetRequestBody: {
        name: workingState.metadata.name,
        description: workingState.metadata.description,
        toolUrns: workingState.toolUrns,
      },
    });

    // TODO: Also save tool variations if overrides exist
    for (const [toolUrn, override] of workingState.toolOverrides) {
      if (override.name || override.description) {
        await client.variations.upsertGlobal({
          srcToolUrn: toolUrn,
          name: override.name,
          description: override.description,
        });
      }
    }

    // Clear local state
    reset();
  };

  const discardChanges = () => {
    setWorkingState({
      metadata: {
        name: toolset?.name ?? "",
        description: toolset?.description ?? "",
      },
      toolUrns: instance?.tools?.map(t => t.toolUrn) ?? [],
      toolOverrides: new Map(),
    });
  };

  return {
    workingState,
    hasChanges,
    changes,
    actions: {
      addTool,
      removeTool,
      updateToolOverride,
      updateMetadata: (metadata: Partial<WorkingState['metadata']>) => {
        setWorkingState(prev => ({
          ...prev,
          metadata: { ...prev.metadata, ...metadata },
        }));
      },
      saveChanges,
      discardChanges,
    },
  };
}
```

### Tools Filtering for Chat

When rendering the chat, filter tools based on working state:

```typescript
// In ChatWindow or wherever tools are passed to AI
const chatTools = useMemo(() => {
  return workingState.toolUrns
    .map(urn => allTools.find(t => t.toolUrn === urn))
    .filter(Boolean)
    .filter(tool => {
      // Respect enable/disable override
      const override = workingState.toolOverrides.get(tool.toolUrn);
      return override?.enabled !== false;
    })
    .map(tool => {
      // Apply name/description overrides
      const override = workingState.toolOverrides.get(tool.toolUrn);
      return {
        ...tool,
        name: override?.name ?? tool.name,
        description: override?.description ?? tool.description,
      };
    });
}, [workingState, allTools]);
```

---

## Comparison with OpenAI Assistants Playground

Based on typical OpenAI Assistants patterns:

| Feature | OpenAI | Our Vision |
|---------|--------|------------|
| **Tool Selection** | Checkbox list | ✅ Checkbox list (done) |
| **Add Tools** | "+" button in header | ✅ Planned Phase 2 |
| **Remove Tools** | "X" button per tool | ✅ Planned Phase 2 |
| **Edit Tool Name** | Inline edit | ✅ Planned Phase 2 |
| **Edit Instructions** | Textarea | ✅ Could add as system prompt |
| **Model Selection** | Dropdown | ⚠️ Not in scope (toolset-level) |
| **Temperature** | Slider | ✅ Already have |
| **Save Changes** | Auto-save | ✅ Manual save (Phase 4) |
| **Test in Chat** | Right panel | ✅ Already have |
| **View Logs** | Right panel | ✅ Already have |

**Key Differences:**
1. **OpenAI**: Assistants are mutable, first-class entities
   **Gram**: Toolsets are blueprints; instances are ephemeral

2. **OpenAI**: Changes save immediately to assistant
   **Gram**: Changes are local until explicitly saved

3. **OpenAI**: Code Interpreter, File Search are special tools
   **Gram**: All tools are uniform (HTTP, function, prompt)

---

## Risk & Mitigation

### Risk 1: Confusion Between Playground and Toolset Pages
**Problem**: Users might not understand that playground changes are temporary.

**Mitigation**:
- Clear labeling: "Playground Configuration" vs "Toolset Configuration"
- Visual indicator when changes exist: "Unsaved Changes" badge
- Confirmation dialog before navigating away with unsaved changes
- Help tooltip explaining the difference

### Risk 2: Accidental Overwrites
**Problem**: Saving playground changes might overwrite production toolset.

**Mitigation**:
- Confirmation dialog on save: "Save changes to [Toolset Name]?"
- Show diff before saving (added/removed tools)
- Option to "Save as New Toolset" instead
- Environment selector warns if on production env

### Risk 3: Performance with Many Tools
**Problem**: Large toolsets (100+ tools) might be slow to render/edit.

**Mitigation**:
- Virtual scrolling for tool list (react-window)
- Debounce search/filter inputs
- Pagination or "Load More" for huge lists
- Lazy load tool details on expand

### Risk 4: State Synchronization
**Problem**: If toolset is edited elsewhere while playground is open, changes conflict.

**Mitigation**:
- Poll for updates every N seconds (like Google Docs)
- Show banner: "Toolset was updated. Reload?"
- Optimistic UI with rollback on conflict
- Version checking before save (compare timestamps)

---

## Success Metrics

### User Behavior
- % of playground sessions that modify tools
- % of playground sessions that save changes
- Average time to add/remove tools
- Tool churn rate (add then remove)

### Performance
- Time to load playground (<500ms)
- Time to add tool to working copy (<50ms)
- Time to save changes (<2s)
- Tool list render time (<100ms)

### Quality
- Bug reports related to tool management
- Support tickets about "lost changes"
- User confusion (via surveys/feedback)

---

## Open Questions

1. **Should playground changes affect other users' playground sessions?**
   - If yes, need real-time sync (WebSockets)
   - If no, simpler but isolated

2. **Should we version playground configurations?**
   - Could save snapshots: "Playground Session 2024-01-15 3:42pm"
   - Users could restore or share configurations

3. **Should tools have per-environment overrides?**
   - Tool might work differently in dev vs prod
   - Could store overrides per environment

4. **What about resource variables (API keys, etc)?**
   - Not covered in this doc
   - Needs separate design (security-sensitive)

5. **Should we support "branching" from a toolset?**
   - Create derivative toolset from playground session
   - Like "Save As" but with provenance tracking

---

## Conclusion

The vision is to make the Playground a **true editing environment** where users can:
1. ✨ **Experiment freely** without fear of breaking production
2. 🛠️ **Configure toolsets** directly in the context where they're tested
3. 📊 **See immediate feedback** via chat and logs
4. 💾 **Save when ready** with clear control over persistence

This aligns with OpenAI's pattern while respecting Gram's architecture (toolsets as blueprints, instances as runtime environments).

**The key insight**: Playground should feel like a **draft mode** for toolsets, not a separate experience.

### Next Steps
1. Review this doc with team
2. Validate assumptions about user workflow
3. Prioritize phases based on user feedback
4. Start with Phase 1 (foundation) for quick wins
5. Iterate based on real usage patterns

---

**Last Updated**: 2025-01-06
**Author**: Claude (Anthropic)
**Status**: Draft for Review
