# GetAgentPluginsRequest

## Example Usage

```typescript
import { GetAgentPluginsRequest } from "@gram/client/models/operations/getagentplugins.js";

let value: GetAgentPluginsRequest = {
  email: "dev@acme.corp",
};
```

## Fields

| Field     | Type     | Required           | Description                                                                                    | Example       |
| --------- | -------- | ------------------ | ---------------------------------------------------------------------------------------------- | ------------- |
| `email`   | _string_ | :heavy_check_mark: | Email address of the enrolled user. Used to resolve plugin assignments against principal URNs. | dev@acme.corp |
| `gramKey` | _string_ | :heavy_minus_sign: | API Key header                                                                                 |               |
