# CreateToolsetForm

## Example Usage

```typescript
import { CreateToolsetForm } from "@gram/sdk/models/components";

let value: CreateToolsetForm = {
  description: "Hic tempora.",
  httpToolIds: [
    "Architecto dolorem accusamus non et provident.",
    "Sit aut sapiente ipsam.",
    "Corrupti voluptatem consectetur et autem non.",
  ],
  name: "Praesentium nemo in quos quasi.",
  projectId: "Asperiores eaque aut minus.",
};
```

## Fields

| Field                                                                                                                                                    | Type                                                                                                                                                     | Required                                                                                                                                                 | Description                                                                                                                                              | Example                                                                                                                                                  |
| -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `description`                                                                                                                                            | *string*                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                       | Description of the toolset                                                                                                                               | Sint dolores sit laboriosam et dolores rerum.                                                                                                            |
| `httpToolIds`                                                                                                                                            | *string*[]                                                                                                                                               | :heavy_minus_sign:                                                                                                                                       | List of HTTP tool IDs to include                                                                                                                         | [<br/>"Rerum nostrum non voluptate.",<br/>"Quo vitae.",<br/>"Voluptatum illum aut delectus dolorem officiis.",<br/>"Voluptatem recusandae ut similique dolor dolorum."<br/>] |
| `name`                                                                                                                                                   | *string*                                                                                                                                                 | :heavy_check_mark:                                                                                                                                       | The name of the toolset                                                                                                                                  | Placeat aut nam nostrum alias.                                                                                                                           |
| `projectId`                                                                                                                                              | *string*                                                                                                                                                 | :heavy_check_mark:                                                                                                                                       | The project ID this toolset belongs to                                                                                                                   | Reprehenderit fugiat temporibus reprehenderit.                                                                                                           |