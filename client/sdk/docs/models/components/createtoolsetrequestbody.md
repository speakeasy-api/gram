# CreateToolsetRequestBody

## Example Usage

```typescript
import { CreateToolsetRequestBody } from "@gram/sdk/models/components";

let value: CreateToolsetRequestBody = {
  description: "Mollitia quisquam amet.",
  httpToolIds: [
    "Nostrum dolor eum dolores.",
    "Dolores ducimus cumque.",
    "A id in placeat quasi ut.",
  ],
  name: "Incidunt sed dolor ut.",
};
```

## Fields

| Field                                                                                                        | Type                                                                                                         | Required                                                                                                     | Description                                                                                                  | Example                                                                                                      |
| ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| `description`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | Description of the toolset                                                                                   | Natus accusantium explicabo.                                                                                 |
| `httpToolIds`                                                                                                | *string*[]                                                                                                   | :heavy_minus_sign:                                                                                           | List of HTTP tool IDs to include                                                                             | [<br/>"Quae animi saepe ex possimus ut vero.",<br/>"Dolorem quibusdam corrupti.",<br/>"Et et hic molestias excepturi."<br/>] |
| `name`                                                                                                       | *string*                                                                                                     | :heavy_check_mark:                                                                                           | The name of the toolset                                                                                      | Numquam est.                                                                                                 |