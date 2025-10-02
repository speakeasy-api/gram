# Tool

A polymorphic tool - can be an HTTP tool or a prompt template


## Supported Types

### `components.HTTPToolDefinition`

```typescript
const value: components.HTTPToolDefinition = {
  canonicalName: "<value>",
  confirm: "<value>",
  createdAt: new Date("2024-07-12T21:04:02.837Z"),
  deploymentId: "<id>",
  description: "since character yogurt freely yet substitution essential",
  httpMethod: "<value>",
  id: "<id>",
  name: "<value>",
  path: "/boot",
  projectId: "<id>",
  schema: "<value>",
  summary: "<value>",
  tags: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  toolUrn: "<value>",
  type: "http",
  updatedAt: new Date("2025-06-27T15:50:36.598Z"),
};
```

### `components.PromptTemplate`

```typescript
const value: components.PromptTemplate = {
  canonicalName: "<value>",
  confirm: "<value>",
  createdAt: new Date("2025-09-26T02:41:42.436Z"),
  deploymentId: "<id>",
  description: "scratch certainly while ajar",
  engine: "mustache",
  historyId: "<id>",
  id: "<id>",
  kind: "higher_order_tool",
  name: "<value>",
  projectId: "<id>",
  prompt: "<value>",
  toolUrn: "<value>",
  toolsHint: [
    "<value 1>",
    "<value 2>",
  ],
  type: "prompt",
  updatedAt: new Date("2024-04-02T03:48:17.332Z"),
};
```

