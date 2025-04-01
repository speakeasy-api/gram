# UpdateToolsetRequestBody

## Example Usage

```typescript
import { UpdateToolsetRequestBody } from "@gram/sdk/models/components";

let value: UpdateToolsetRequestBody = {
  description: "Doloribus mollitia saepe iure aut quis tempora.",
  httpToolIdsToAdd: [
    "Amet et molestiae unde ut sit temporibus.",
    "Expedita necessitatibus.",
  ],
  httpToolIdsToRemove: [
    "Natus saepe sint magni id ab assumenda.",
    "Ipsa voluptatibus autem.",
    "Id quos.",
    "Commodi qui ut dolor ut.",
  ],
  name: "Architecto ipsam.",
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  | Example                                                                      |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `description`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | The new description of the toolset                                           | Saepe non sed.                                                               |
| `httpToolIdsToAdd`                                                           | *string*[]                                                                   | :heavy_minus_sign:                                                           | HTTP tool IDs to add to the toolset                                          | [<br/>"Nisi tempora quasi.",<br/>"Consectetur laudantium eos deleniti et iste ex."<br/>] |
| `httpToolIdsToRemove`                                                        | *string*[]                                                                   | :heavy_minus_sign:                                                           | HTTP tool IDs to remove from the toolset                                     | [<br/>"Aut nemo perspiciatis pariatur.",<br/>"Eos consequatur et natus quia."<br/>] |
| `name`                                                                       | *string*                                                                     | :heavy_minus_sign:                                                           | The new name of the toolset                                                  | Modi officiis accusantium eius in.                                           |