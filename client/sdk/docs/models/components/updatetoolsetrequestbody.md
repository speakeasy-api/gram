# UpdateToolsetRequestBody

## Example Usage

```typescript
import { UpdateToolsetRequestBody } from "@gram/sdk/models/components";

let value: UpdateToolsetRequestBody = {
  description: "Incidunt sunt corrupti est.",
  httpToolIdsToAdd: [
    "Praesentium exercitationem eius perferendis qui minus.",
    "Rerum deserunt nihil laborum.",
  ],
  httpToolIdsToRemove: [
    "Iure consequuntur consequatur praesentium.",
    "Laborum non veniam ut.",
  ],
  name: "Aut esse est modi ipsam.",
};
```

## Fields

| Field                                                                                                                                             | Type                                                                                                                                              | Required                                                                                                                                          | Description                                                                                                                                       | Example                                                                                                                                           |
| ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `description`                                                                                                                                     | *string*                                                                                                                                          | :heavy_minus_sign:                                                                                                                                | The new description of the toolset                                                                                                                | Vel praesentium illo.                                                                                                                             |
| `httpToolIdsToAdd`                                                                                                                                | *string*[]                                                                                                                                        | :heavy_minus_sign:                                                                                                                                | HTTP tool IDs to add to the toolset                                                                                                               | [<br/>"Alias iste.",<br/>"Odio quia odit necessitatibus quibusdam.",<br/>"Aut autem dolorum alias dolorem et.",<br/>"Excepturi ut aut expedita consequatur ea."<br/>] |
| `httpToolIdsToRemove`                                                                                                                             | *string*[]                                                                                                                                        | :heavy_minus_sign:                                                                                                                                | HTTP tool IDs to remove from the toolset                                                                                                          | [<br/>"Aperiam aliquam alias nostrum.",<br/>"Id repudiandae nemo.",<br/>"Quia et.",<br/>"Dolor et aliquid inventore sunt."<br/>]                  |
| `name`                                                                                                                                            | *string*                                                                                                                                          | :heavy_minus_sign:                                                                                                                                | The new name of the toolset                                                                                                                       | Distinctio aliquam laudantium in id.                                                                                                              |