# HookIngestSource

Metadata about the local hook adapter that translated a provider event into the Gram hook contract.

## Example Usage

```typescript
import { HookIngestSource } from "@gram/client/models/components/hookingestsource.js";

let value: HookIngestSource = {
  adapter: "<value>",
};
```

## Fields

| Field                                                                     | Type                                                                      | Required                                                                  | Description                                                               |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `adapter`                                                                 | *string*                                                                  | :heavy_check_mark:                                                        | Stable adapter slug, e.g. claude, cursor, codex, or a customer hook name. |
| `adapterVersion`                                                          | *string*                                                                  | :heavy_minus_sign:                                                        | Adapter implementation version.                                           |
| `hostname`                                                                | *string*                                                                  | :heavy_minus_sign:                                                        | Hostname of the machine that emitted the hook event.                      |
| `rawEventName`                                                            | *string*                                                                  | :heavy_minus_sign:                                                        | Provider-native event name, if one exists.                                |