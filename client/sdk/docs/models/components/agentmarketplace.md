# AgentMarketplace

## Example Usage

```typescript
import { AgentMarketplace } from "@gram/client/models/components/agentmarketplace.js";

let value: AgentMarketplace = {
  name: "<value>",
  url: "https://decent-chiffonier.net",
};
```

## Fields

| Field  | Type     | Required           | Description                                                                                                                                                                                                                                                                                             |
| ------ | -------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name` | _string_ | :heavy_check_mark: | Stable identifier for the marketplace, used as its key when the agent registers it with a managed tool. Matches the name written into the published marketplace.json, derived from the organization name (for example, `<org-slug>-gram`), so plugin references resolve deterministically across polls. |
| `url`  | _string_ | :heavy_check_mark: | Git URL for the marketplace, served by the marketplace proxy.                                                                                                                                                                                                                                           |
