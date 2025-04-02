# Toolset

## Example Usage

```typescript
import { Toolset } from "@gram/sdk/models/components";

let value: Toolset = {
  createdAt: new Date("1993-04-10T16:29:23Z"),
  description: "Sunt quia.",
  httpToolIds: [
    "Eos minima ut assumenda.",
    "Magnam sunt voluptatem veniam blanditiis.",
    "Dolor non consequatur.",
  ],
  id: "Molestias eos laudantium.",
  name: "Quo dolor vero dolorum.",
  organizationId: "Iste dicta exercitationem earum.",
  projectId: "Ut porro ut repudiandae iure qui.",
  slug: "Eveniet voluptatem quae totam quisquam laborum qui.",
  updatedAt: new Date("1988-09-01T01:28:14Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was created.                                                                 | 2003-06-23T19:35:01Z                                                                          |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Description of the toolset                                                                    | Dicta libero at corrupti recusandae.                                                          |
| `httpToolIds`                                                                                 | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | List of HTTP tool IDs included in this toolset                                                | [<br/>"Quasi et omnis nihil repudiandae aut.",<br/>"Aliquam sint velit qui quasi."<br/>]      |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the toolset                                                                         | Voluptatibus exercitationem nihil voluptatum eligendi omnis.                                  |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the toolset                                                                       | Eum rerum voluptates et.                                                                      |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this toolset belongs to                                                   | Voluptate dolor dolorum distinctio aut.                                                       |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this toolset belongs to                                                        | Ut velit accusamus.                                                                           |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The slug of the toolset                                                                       | Reiciendis nulla exercitationem aut aut quos.                                                 |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was last updated.                                                            | 1985-11-07T21:13:42Z                                                                          |