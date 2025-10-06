# Tool

A polymorphic tool - can be an HTTP tool or a prompt template

## Example Usage

```typescript
import { Tool } from "@gram/client/models/components";

let value: Tool = {};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `httpToolDefinition`                                                           | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md) | :heavy_minus_sign:                                                             | An HTTP tool                                                                   |
| `promptTemplate`                                                               | [components.PromptTemplate](../../models/components/prompttemplate.md)         | :heavy_minus_sign:                                                             | A prompt template                                                              |