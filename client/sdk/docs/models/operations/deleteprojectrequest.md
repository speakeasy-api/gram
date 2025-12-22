# DeleteProjectRequest

## Example Usage

```typescript
import { DeleteProjectRequest } from "@gram/client/models/operations";

let value: DeleteProjectRequest = {
  id: "ddafe79f-01ae-4d3d-9851-3fe5582161d5",
};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `id`                            | *string*                        | :heavy_check_mark:              | The id of the project to delete |
| `gramKey`                       | *string*                        | :heavy_minus_sign:              | API Key header                  |
| `gramSession`                   | *string*                        | :heavy_minus_sign:              | Session header                  |