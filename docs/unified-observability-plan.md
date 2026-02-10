# Unified Observability UI Plan

## Goal

Combine the original raw telemetry logs concept with the new chat-centric resolution UI. Users should be able to:

1. View chat sessions with resolution status at a glance (current UI)
2. Drill down into underlying raw logs and telemetry data
3. Use the Copilot to search both chat resolutions AND raw telemetry data

---

## Current State

### Pages
- **Insights page**: High-level metrics (chat resolutions, tool performance, system health)
- **Logs page**: Chat sessions table with resolution scores and status filters

### Copilot Integration
- Insights Copilot: Uses metrics tools (`*metrics*` filter)
- Logs Copilot: Uses chat tools (`*chat*` filter)

### Available Tools
- `searchChats` - Search chat session summaries
- `listChatsWithResolutions` - Chat sessions with resolution data
- `searchLogs` - Search raw telemetry logs (trace ID, severity, HTTP info, etc.)
- `searchToolCalls` - Search tool call summaries
- `getObservabilityOverview` - Dashboard metrics

---

## Proposed Changes

### 1. Enhanced Chat Detail Panel

When a user clicks on a chat session in the Logs table, the detail panel should show:

**Tab 1: Overview (Current)**
- Resolution status and score
- User goal and resolution notes
- Message timeline

**Tab 2: Telemetry Logs (New)**
- Raw telemetry logs filtered by `gram_chat_id`
- Searchable/filterable log entries
- Log severity indicators (info, warn, error)
- Expandable log details with full attributes

**Tab 3: Tool Calls (New)**
- Tool calls made during this chat session
- Success/failure status for each call
- Latency and duration metrics
- Expandable view showing request/response details

### 2. Logs Page Enhancement

Add a view toggle or tabs at the page level:

**View A: Chat Sessions (Current default)**
- Current table showing chat sessions with resolution scores
- Filter by resolution status, search, date range

**View B: Raw Telemetry Logs**
- Table of raw log entries (similar to old Logs page)
- Columns: timestamp, severity, message, trace ID, service, etc.
- Clicking a log entry could link to the parent chat session

**View C: Tool Calls**
- Table of individual tool calls
- Columns: timestamp, tool name, status, duration, trace ID
- Link to parent chat session

### 3. Copilot Integration

Update the Logs page copilot to include BOTH chat and logs tools:

```typescript
// Updated filter to include both chat and telemetry tools
const combinedToolFilter = useCallback(
  ({ toolName }: { toolName: string }) => {
    const name = toolName.toLowerCase();
    return (
      name.includes("chat") ||
      name.includes("logs") ||
      name.includes("toolcall")
    );
  },
  [],
);
```

Update system prompt to explain the dual capability:

```
You are a helpful assistant for analyzing chat sessions and telemetry data.

You can:
1. Search and analyze chat sessions, including resolution status and user goals
2. Search raw telemetry logs for debugging and detailed analysis
3. Find tool call patterns and failures

When debugging issues, you can correlate chat sessions with their underlying telemetry logs and tool calls.
```

### 4. Navigation & Drill-Down

Add intuitive drill-down paths:

- **From Insights → Logs**: "View chats" links already exist
- **From Chat → Logs**: Add "View raw logs" button in chat detail panel
- **From Log entry → Chat**: Add link back to parent chat session
- **From Tool call → Logs**: Link to related log entries via trace ID

### 5. Search & Filter Integration

Unified search that works across:
- Chat content (messages, user goals)
- Telemetry logs (log body, attributes)
- Tool calls (tool names, status)

Could use a global search bar with type facets:
```
[ Search across chats, logs, and tools... ] [Type: All ▼] [Date: 30d ▼]
```

---

## Implementation Phases

### Phase 1: Chat Detail Panel Tabs ✅ PRIORITY
- Add Telemetry Logs tab to ChatDetailPanel
- Add Tool Calls tab to ChatDetailPanel
- Query logs/tool calls filtered by gram_chat_id

### Phase 2: Page View Toggle ❌ DROPPED
Raw telemetry logs are supplementary to chat resolutions, not a separate view.

### Phase 3: Copilot Enhancement ✅ PRIORITY
- Update tool filter to include all relevant tools (chat + logs)
- Update system prompt for dual capability
- Add example prompts for cross-domain queries

### Phase 4: Navigation & Linking ❌ DROPPED
Not needed since Phase 2 was dropped.

---

## Open Questions

1. **Performance**: How to efficiently load raw logs for large chat sessions?
   - Consider pagination within the detail panel
   - Lazy load tabs

2. **Storage**: Raw logs can be verbose. Consider:
   - Sampling for display
   - Aggregation views
   - Full log access via export/download

3. **Time alignment**: Ensure all views respect the same date range filter
   - Already implemented via URL params

4. **Permissions**: Any logs/data that should be restricted?
   - Consider sensitive data masking
