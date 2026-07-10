# LastPollStatus

Derived status for the latest usage poll state. Omitted when no config is set for the provider.

## Example Usage

```typescript
import { LastPollStatus } from "@gram/client/models/components/aiintegrationconfig.js";

let value: LastPollStatus = "failed";
```

## Values

```typescript
"pending" | "success" | "failed"
```