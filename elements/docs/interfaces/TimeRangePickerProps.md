[**@gram-ai/elements v1.26.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / TimeRangePickerProps

# Interface: TimeRangePickerProps

## Properties

### preset?

> `optional` **preset**: [`DateRangePreset`](../type-aliases/DateRangePreset.md) \| `null`

Current preset value

***

### customRange?

> `optional` **customRange**: [`TimeRange`](TimeRange.md) \| `null`

Current custom range

***

### onPresetChange()?

> `optional` **onPresetChange**: (`preset`) => `void`

Called when a preset is selected

#### Parameters

##### preset

[`DateRangePreset`](../type-aliases/DateRangePreset.md)

#### Returns

`void`

***

### onCustomRangeChange()?

> `optional` **onCustomRangeChange**: (`from`, `to`, `label?`) => `void`

Called when a custom range is selected

#### Parameters

##### from

`Date`

##### to

`Date`

##### label?

`string`

#### Returns

`void`

***

### onClearCustomRange()?

> `optional` **onClearCustomRange**: () => `void`

Called to clear custom range

#### Returns

`void`

***

### customRangeLabel?

> `optional` **customRangeLabel**: `string` \| `null`

Initial label for custom range (from URL params)

***

### showLive?

> `optional` **showLive**: `boolean`

Show LIVE mode option

***

### isLive?

> `optional` **isLive**: `boolean`

Is LIVE mode active

***

### onLiveChange()?

> `optional` **onLiveChange**: (`isLive`) => `void`

Called when LIVE mode changes

#### Parameters

##### isLive

`boolean`

#### Returns

`void`

***

### disabled?

> `optional` **disabled**: `boolean`

Disabled state

***

### timezone?

> `optional` **timezone**: `string`

Timezone display (e.g., "UTC-08:00")

***

### apiUrl?

> `optional` **apiUrl**: `string`

API URL for AI parsing (defaults to window.location.origin)

***

### projectSlug?

> `optional` **projectSlug**: `string`

Project slug for API authentication
