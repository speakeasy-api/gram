# HTTPToolDefinition

## Example Usage

```typescript
import { HTTPToolDefinition } from "@gram/sdk/models/components";

let value: HTTPToolDefinition = {
  apikeyEnvVar: "Dolorem fugiat cupiditate corporis laborum cum.",
  bearerEnvVar: "Et dignissimos.",
  bodySchema: "Molestiae architecto qui.",
  description: "Eos placeat quia illo.",
  headersSchema: "Autem id minima sit numquam.",
  httpMethod: "Repudiandae ipsa velit atque et architecto nisi.",
  id: "Est minima voluptatibus.",
  name: "Nobis impedit eaque similique aliquid.",
  passwordEnvVar: "Aliquam harum voluptatum autem.",
  path: "Quis magni vel autem voluptas officiis.",
  pathparamsSchema: "Eligendi quibusdam qui animi illum nisi.",
  queriesSchema: "Fugiat rem et aliquid aut.",
  securityType: "Rerum ullam velit molestiae odio.",
  serverEnvVar: "Dignissimos dolor tempore.",
  usernameEnvVar: "Accusamus et possimus id doloremque.",
};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        | Example                                            |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `apikeyEnvVar`                                     | *string*                                           | :heavy_minus_sign:                                 | Environment variable for API key                   | Earum culpa rem et nulla.                          |
| `bearerEnvVar`                                     | *string*                                           | :heavy_minus_sign:                                 | Environment variable for bearer token              | Omnis laudantium distinctio qui.                   |
| `bodySchema`                                       | *string*                                           | :heavy_minus_sign:                                 | JSON schema for request body                       | Quisquam atque qui dicta et a.                     |
| `description`                                      | *string*                                           | :heavy_check_mark:                                 | Description of the tool                            | Qui est repudiandae sint placeat sed explicabo.    |
| `headersSchema`                                    | *string*                                           | :heavy_minus_sign:                                 | JSON schema for headers                            | Quia nihil eaque quam praesentium eligendi.        |
| `httpMethod`                                       | *string*                                           | :heavy_check_mark:                                 | HTTP method for the request                        | Facere minus ratione qui.                          |
| `id`                                               | *string*                                           | :heavy_check_mark:                                 | The ID of the HTTP tool                            | Non architecto.                                    |
| `name`                                             | *string*                                           | :heavy_check_mark:                                 | The name of the tool                               | Velit iure et corrupti quia quis.                  |
| `passwordEnvVar`                                   | *string*                                           | :heavy_minus_sign:                                 | Environment variable for password                  | Inventore accusamus accusamus aut repudiandae.     |
| `path`                                             | *string*                                           | :heavy_check_mark:                                 | Path for the request                               | Laudantium molestiae ipsum.                        |
| `pathparamsSchema`                                 | *string*                                           | :heavy_minus_sign:                                 | JSON schema for path parameters                    | Hic sed fugit cum voluptatem laborum eum.          |
| `queriesSchema`                                    | *string*                                           | :heavy_minus_sign:                                 | JSON schema for query parameters                   | Aliquam sunt non voluptas maxime similique.        |
| `securityType`                                     | *string*                                           | :heavy_check_mark:                                 | Type of security (http:bearer, http:basic, apikey) | Totam voluptatem tempora consequatur.              |
| `serverEnvVar`                                     | *string*                                           | :heavy_check_mark:                                 | Environment variable for the server URL            | Esse perspiciatis totam iure.                      |
| `usernameEnvVar`                                   | *string*                                           | :heavy_minus_sign:                                 | Environment variable for username                  | Autem ut in repellendus cupiditate.                |