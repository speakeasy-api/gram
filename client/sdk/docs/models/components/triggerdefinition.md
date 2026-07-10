# TriggerDefinition

## Example Usage

```typescript
import { TriggerDefinition } from "@gram/client/models/components/triggerdefinition.js";

let value: TriggerDefinition = {
  configSchema: "{key: 6599610655286421, key1: null, key2: \"<value>\"}",
  description:
    "jealously versus midst defensive aw vastly joshingly mostly yum",
  envRequirements: [
    {
      name: "<value>",
      required: true,
    },
  ],
  kind: "schedule",
  slug: "<value>",
  title: "<value>",
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `configSchema`                                                                         | *string*                                                                               | :heavy_check_mark:                                                                     | JSON schema describing the trigger config.                                             |
| `description`                                                                          | *string*                                                                               | :heavy_check_mark:                                                                     | Description of the trigger definition.                                                 |
| `envRequirements`                                                                      | [components.TriggerEnvRequirement](../../models/components/triggerenvrequirement.md)[] | :heavy_check_mark:                                                                     | Environment variables required by this trigger definition.                             |
| `kind`                                                                                 | [components.TriggerDefinitionKind](../../models/components/triggerdefinitionkind.md)   | :heavy_check_mark:                                                                     | The ingress kind for the trigger definition.                                           |
| `slug`                                                                                 | *string*                                                                               | :heavy_check_mark:                                                                     | The trigger definition slug.                                                           |
| `title`                                                                                | *string*                                                                               | :heavy_check_mark:                                                                     | The trigger definition title.                                                          |