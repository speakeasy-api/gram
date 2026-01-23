# SourceEnvironmentLink

A link between a source and an environment

## Example Usage

```typescript
import { SourceEnvironmentLink } from "@gram/client/models/components";

let value: SourceEnvironmentLink = {
  environmentId: "358c9a57-55f6-40e6-adec-27cd199e2771",
  id: "06225d7f-155f-4c53-a53b-de840ae1759d",
  sourceKind: "http",
  sourceSlug: "<value>",
};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `environmentId`                                                                                          | *string*                                                                                                 | :heavy_check_mark:                                                                                       | The ID of the environment                                                                                |
| `id`                                                                                                     | *string*                                                                                                 | :heavy_check_mark:                                                                                       | The ID of the source environment link                                                                    |
| `sourceKind`                                                                                             | [components.SourceEnvironmentLinkSourceKind](../../models/components/sourceenvironmentlinksourcekind.md) | :heavy_check_mark:                                                                                       | The kind of source that can be linked to an environment                                                  |
| `sourceSlug`                                                                                             | *string*                                                                                                 | :heavy_check_mark:                                                                                       | The slug of the source                                                                                   |