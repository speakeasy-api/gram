# AgentPlugin

## Example Usage

```typescript
import { AgentPlugin } from "@gram/client/models/components/agentplugin.js";

let value: AgentPlugin = {
  marketplaceName: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                                                                                                                   | Type                                                                                                                    | Required                                                                                                                | Description                                                                                                             |
| ----------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `marketplaceName`                                                                                                       | *string*                                                                                                                | :heavy_check_mark:                                                                                                      | Name of the marketplace this plugin lives in. Always equals the `name` of one of the marketplaces in the same response. |
| `slug`                                                                                                                  | *string*                                                                                                                | :heavy_check_mark:                                                                                                      | Plugin slug. Combined with marketplace_name, this identifies the plugin the agent enables in the managed tool.          |