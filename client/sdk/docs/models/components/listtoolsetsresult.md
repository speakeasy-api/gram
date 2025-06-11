# ListToolsetsResult

## Example Usage

```typescript
import { ListToolsetsResult } from "@gram/client/models/components";

let value: ListToolsetsResult = {
  toolsets: [
    {
      createdAt: new Date("2025-09-20T12:21:57.178Z"),
      httpTools: [
        {
          canonicalName: "<value>",
          confirm: "<value>",
          createdAt: new Date("2025-10-23T12:10:12.732Z"),
          deploymentId: "<id>",
          description: "revoke aw blah upside-down gah greatly",
          httpMethod: "<value>",
          id: "<id>",
          name: "<value>",
          path: "/selinux",
          projectId: "<id>",
          schema: "<value>",
          summary: "<value>",
          tags: [
            "<value>",
          ],
          updatedAt: new Date("2024-11-14T21:37:54.702Z"),
        },
      ],
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      promptTemplates: [
        {
          createdAt: new Date("2025-10-26T13:44:01.494Z"),
          engine: "mustache",
          historyId: "<id>",
          id: "<id>",
          kind: "prompt",
          name: "<value>",
          prompt: "<value>",
          toolsHint: [
            "<value>",
          ],
          updatedAt: new Date("2023-11-25T20:01:26.084Z"),
        },
      ],
      slug: "<value>",
      updatedAt: new Date("2023-02-09T02:06:35.798Z"),
    },
  ],
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `toolsets`                                                 | [components.Toolset](../../models/components/toolset.md)[] | :heavy_check_mark:                                         | The list of toolsets                                       |