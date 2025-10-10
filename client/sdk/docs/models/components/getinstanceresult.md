# GetInstanceResult

## Example Usage

```typescript
import { GetInstanceResult } from "@gram/client/models/components";

let value: GetInstanceResult = {
  environment: {
    createdAt: new Date("2025-09-30T14:15:42.248Z"),
    entries: [
      {
        createdAt: new Date("2025-04-04T09:14:28.012Z"),
        name: "<value>",
        updatedAt: new Date("2024-02-25T05:54:42.792Z"),
        value: "<value>",
      },
    ],
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    slug: "<value>",
    updatedAt: new Date("2023-07-28T21:14:20.018Z"),
  },
  name: "<value>",
  tools: [
    {},
  ],
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `description`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | The description of the toolset                                                                     |
| `environment`                                                                                      | [components.Environment](../../models/components/environment.md)                                   | :heavy_check_mark:                                                                                 | Model representing an environment                                                                  |
| `functionEnvironmentVariables`                                                                     | [components.FunctionEnvironmentVariable](../../models/components/functionenvironmentvariable.md)[] | :heavy_minus_sign:                                                                                 | The function environment variables that are relevant to the toolset                                |
| `name`                                                                                             | *string*                                                                                           | :heavy_check_mark:                                                                                 | The name of the toolset                                                                            |
| `promptTemplates`                                                                                  | [components.PromptTemplate](../../models/components/prompttemplate.md)[]                           | :heavy_minus_sign:                                                                                 | The list of prompt templates                                                                       |
| `securityVariables`                                                                                | [components.SecurityVariable](../../models/components/securityvariable.md)[]                       | :heavy_minus_sign:                                                                                 | The security variables that are relevant to the toolset                                            |
| `serverVariables`                                                                                  | [components.ServerVariable](../../models/components/servervariable.md)[]                           | :heavy_minus_sign:                                                                                 | The server variables that are relevant to the toolset                                              |
| `tools`                                                                                            | [components.Tool](../../models/components/tool.md)[]                                               | :heavy_check_mark:                                                                                 | The list of tools                                                                                  |