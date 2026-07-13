# GetPluginsResult

## Example Usage

```typescript
import { GetPluginsResult } from "@gram/client/models/components/getpluginsresult.js";

let value: GetPluginsResult = {
  etag: "<value>",
  marketplaces: [
    {
      name: "<value>",
      url: "https://unaware-swanling.info/",
    },
  ],
  plugins: [],
};
```

## Fields

| Field          | Type                                                                         | Required           | Description                                                                                                              |
| -------------- | ---------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `etag`         | _string_                                                                     | :heavy_check_mark: | Opaque revision identifier covering the marketplace + plugin set. The agent stores this to detect changes between polls. |
| `marketplaces` | [components.AgentMarketplace](../../models/components/agentmarketplace.md)[] | :heavy_check_mark: | Plugin marketplaces the agent should register with the tools it manages. Sorted by name.                                 |
| `plugins`      | [components.AgentPlugin](../../models/components/agentplugin.md)[]           | :heavy_check_mark: | Plugins the agent should enable. Each entry references one of the marketplaces above by name.                            |
