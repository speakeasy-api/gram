# Hooks Service

This service handles Claude Code hook events for tool usage observability, providing real-time tracking of tool calls with user attribution.

## Architecture Overview

The hooks service uses a **Redis-buffered validation pattern** to handle the timing mismatch between unauthenticated hook events and authenticated session metadata.

### The Challenge

Claude Code sends two types of requests to our service:

1. **Hook Events** (unauthenticated) - `PreToolUse`, `PostToolUse`, `PostToolUseFailure`
   - (Usually) arrive first (immediately when tools are called)
   - Contain tool information (including `session_id`) but no user identity
   - Cannot validate authenticity

2. **Logs Events** (authenticated via API key)
   - (Usually) arrive later (after session established)
   - Contain `session_id`, `user_email`, `organization.id`
   - Validated through API key authentication

**Problem**: Hook events often arrive _before_ we know the user's identity via the Logs endpoint.

### The Solution: Redis Buffering

```
┌─────────────────────────────────────────────────────────────────┐
│                      Hook Event Flow                            │
└─────────────────────────────────────────────────────────────────┘

Hook Event Arrives
       │
       ├──> Check Redis: "session:metadata:{session_id}"
       │
       ├──> FOUND: Session validated! ✓
       │    └──> Write directly to ClickHouse with:
       │         • user_email
       │         • gram_org_id
       │         • project_id
       │         • tool call data
       │
       └──> NOT FOUND: Session not yet validated
            └──> Buffer in Redis: "hook:pending:{session_id}:{tool_use_id}"
                 • TTL: 10 minutes
                 • Contains full hook payload
                 • Will be processed when session validates


┌─────────────────────────────────────────────────────────────────┐
│                      Logs Event Flow                            │
└─────────────────────────────────────────────────────────────────┘

Logs Event Arrives (Authenticated)
       │
       ├──> Validate API Key ✓
       │
       ├──> Extract:
       │    • session_id
       │    • user_email
       │    • Claude organization.id
       │
       ├──> Get gram_org_id & project_id from auth context
       │
       └──> Store in Redis: "session:metadata:{session_id}"
            • TTL: 24 hours
            • Future hooks will find this and write directly to ClickHouse
            • Buffered hooks will be processed on next call (within 10min window)
```
