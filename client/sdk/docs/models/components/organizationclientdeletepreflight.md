# OrganizationClientDeletePreflight

Authoritative impact summary for deleting a remote_session_client: how many sessions it holds and the names of the MCP servers it is attached to.

## Example Usage

```typescript
import { OrganizationClientDeletePreflight } from "@gram/client/models/components/organizationclientdeletepreflight.js";

let value: OrganizationClientDeletePreflight = {
  mcpServerNames: ["<value 1>", "<value 2>", "<value 3>"],
  sessionCount: 125813,
};
```

## Fields

| Field            | Type       | Required           | Description                                                       |
| ---------------- | ---------- | ------------------ | ----------------------------------------------------------------- |
| `mcpServerNames` | _string_[] | :heavy_check_mark: | Display names of MCP servers this client is attached to.          |
| `sessionCount`   | _number_   | :heavy_check_mark: | Number of non-deleted remote_sessions minted against this client. |
