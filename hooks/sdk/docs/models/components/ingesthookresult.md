# IngestHookResult

Provider-neutral decision returned by the unified hook endpoint.

## Fields

| Field      | Type                                                       | Required           | Description                                             |
| ---------- | ---------------------------------------------------------- | ------------------ | ------------------------------------------------------- |
| `Decision` | [components.Decision](../../models/components/decision.md) | :heavy_check_mark: | Whether the local hook should allow or deny the action. |
| `Effects`  | map[string]`any`                                           | :heavy_minus_sign: | Optional side-effect hints for hook SDKs.               |
| `Message`  | `*string`                                                  | :heavy_minus_sign: | User-facing decision message.                           |
| `Reason`   | `*string`                                                  | :heavy_minus_sign: | Machine-readable decision reason.                       |
