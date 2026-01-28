# GetInstanceResult

## Example Usage

```typescript
import { GetInstanceResult } from "@gram/client/models/components";

let value: GetInstanceResult = {
  mcpServers: [
    {
      url: "https://different-spring.biz",
    },
  ],
  name: "<value>",
  tools: [],
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `description`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | The description of the toolset                                                                     |
| `externalMcpHeaderDefinitions`                                                                     | [components.ExternalMCPHeaderDefinition](../../models/components/externalmcpheaderdefinition.md)[] | :heavy_minus_sign:                                                                                 | The external MCP header definitions that are relevant to the toolset                               |
| `functionEnvironmentVariables`                                                                     | [components.FunctionEnvironmentVariable](../../models/components/functionenvironmentvariable.md)[] | :heavy_minus_sign:                                                                                 | The function environment variables that are relevant to the toolset                                |
| `mcpServers`                                                                                       | [components.InstanceMcpServer](../../models/components/instancemcpserver.md)[]                     | :heavy_check_mark:                                                                                 | The MCP servers that are relevant to the toolset                                                   |
| `name`                                                                                             | *string*                                                                                           | :heavy_check_mark:                                                                                 | The name of the toolset                                                                            |
| `promptTemplates`                                                                                  | [components.PromptTemplate](../../models/components/prompttemplate.md)[]                           | :heavy_minus_sign:                                                                                 | The list of prompt templates                                                                       |
| `securityVariables`                                                                                | [components.SecurityVariable](../../models/components/securityvariable.md)[]                       | :heavy_minus_sign:                                                                                 | The security variables that are relevant to the toolset                                            |
| `serverVariables`                                                                                  | [components.ServerVariable](../../models/components/servervariable.md)[]                           | :heavy_minus_sign:                                                                                 | The server variables that are relevant to the toolset                                              |
| `tools`                                                                                            | [components.Tool](../../models/components/tool.md)[]                                               | :heavy_check_mark:                                                                                 | The list of tools                                                                                  |