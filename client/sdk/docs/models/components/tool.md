# Tool

A polymorphic tool - can be an HTTP tool, function tool, prompt template, or external MCP proxy

## Example Usage

```typescript
import { Tool } from "@gram/client/models/components/tool.js";

let value: Tool = {};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `externalMcpToolDefinition`                                                                  | [components.ExternalMCPToolDefinition](../../models/components/externalmcptooldefinition.md) | :heavy_minus_sign:                                                                           | A proxy tool that references an external MCP server                                          |
| `functionToolDefinition`                                                                     | [components.FunctionToolDefinition](../../models/components/functiontooldefinition.md)       | :heavy_minus_sign:                                                                           | A function tool                                                                              |
| `httpToolDefinition`                                                                         | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)               | :heavy_minus_sign:                                                                           | An HTTP tool                                                                                 |
| `platformToolDefinition`                                                                     | [components.PlatformToolDefinition](../../models/components/platformtooldefinition.md)       | :heavy_minus_sign:                                                                           | A platform-owned tool served directly by the platform                                        |
| `promptTemplate`                                                                             | [components.PromptTemplate](../../models/components/prompttemplate.md)                       | :heavy_minus_sign:                                                                           | A prompt template                                                                            |