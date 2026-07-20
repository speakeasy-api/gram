# PublishStatusResult

## Example Usage

```typescript
import { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";

let value: PublishStatusResult = {
  configured: true,
  connected: false,
};
```

## Fields

| Field             | Type                                                                                          | Required           | Description                                                                                                                                                                                                                                      |
| ----------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `configured`      | _boolean_                                                                                     | :heavy_check_mark: | Whether GitHub publishing is configured on the server.                                                                                                                                                                                           |
| `connected`       | _boolean_                                                                                     | :heavy_check_mark: | Whether this project has a GitHub connection.                                                                                                                                                                                                    |
| `lastPublishedAt` | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | When the project was last published to GitHub. Absent when the project is not connected.                                                                                                                                                         |
| `marketplaceUrl`  | _string_                                                                                      | :heavy_minus_sign: | Git-based Claude Code marketplace URL — the value to pass to `/plugin marketplace add` or set as the source URL in `extraKnownMarketplaces`. Present once a marketplace token has been minted, which happens automatically on the first publish. |
| `repoName`        | _string_                                                                                      | :heavy_minus_sign: | GitHub repo name, if connected.                                                                                                                                                                                                                  |
| `repoOwner`       | _string_                                                                                      | :heavy_minus_sign: | GitHub repo owner, if connected.                                                                                                                                                                                                                 |
| `repoUrl`         | _string_                                                                                      | :heavy_minus_sign: | Full GitHub repository URL, if connected.                                                                                                                                                                                                        |
| `upToDate`        | _boolean_                                                                                     | :heavy_minus_sign: | Whether the project's current plugin state matches what was last published to GitHub. Absent when the project is not connected, or when the connection predates content fingerprinting (freshness can't be determined).                          |
