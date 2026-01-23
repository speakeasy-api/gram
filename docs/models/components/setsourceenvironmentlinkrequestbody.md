# SetSourceEnvironmentLinkRequestBody

## Example Usage

```typescript
import { SetSourceEnvironmentLinkRequestBody } from "@gram/client/models/components";

let value: SetSourceEnvironmentLinkRequestBody = {
  environmentId: "4efec091-ab20-43f0-91f9-1e9883176914",
  sourceKind: "function",
  sourceSlug: "<value>",
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `environmentId`                                                | *string*                                                       | :heavy_check_mark:                                             | The ID of the environment to link                              |
| `sourceKind`                                                   | [components.SourceKind](../../models/components/sourcekind.md) | :heavy_check_mark:                                             | The kind of source (http or function)                          |
| `sourceSlug`                                                   | *string*                                                       | :heavy_check_mark:                                             | The slug of the source                                         |