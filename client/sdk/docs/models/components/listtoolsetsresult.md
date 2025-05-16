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
      slug: "<value>",
      updatedAt: new Date("2025-10-26T13:44:01.494Z"),
    },
  ],
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `toolsets`                                                 | [components.Toolset](../../models/components/toolset.md)[] | :heavy_check_mark:                                         | The list of toolsets                                       |