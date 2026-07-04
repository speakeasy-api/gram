# HookMessageData

Assistant/user message payload.

## Fields

| Field        | Type       | Required           | Description                                                        |
| ------------ | ---------- | ------------------ | ------------------------------------------------------------------ |
| `DurationMs` | `*float64` | :heavy_minus_sign: | Message or thinking-block duration in milliseconds, when reported. |
| `Role`       | `*string`  | :heavy_minus_sign: | Message role, e.g. assistant or user.                              |
| `Text`       | `*string`  | :heavy_minus_sign: | Message text.                                                      |
