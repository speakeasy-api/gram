[**@gram-ai/elements v1.18.6**](../README.md)

***

[@gram-ai/elements](../globals.md) / ThemeConfig

# Interface: ThemeConfig

ThemeConfig provides visual appearance customization options.
Inspired by OpenAI ChatKit's ThemeOption.

## Example

```ts
const config: ElementsConfig = {
  theme: {
    colorScheme: 'dark',
    density: 'compact',
    radius: 'round',
  },
}
```

## Properties

### colorScheme?

> `optional` **colorScheme**: `"light"` \| `"dark"` \| `"system"`

The color scheme to use for the UI.

#### Default

```ts
'light'
```

***

### density?

> `optional` **density**: `"compact"` \| `"normal"` \| `"spacious"`

Determines the overall spacing of the UI.
- `compact`: Reduced padding and margins for dense layouts
- `normal`: Standard spacing (default)
- `spacious`: Increased padding and margins for airy layouts

#### Default

```ts
'normal'
```

***

### radius?

> `optional` **radius**: `"round"` \| `"soft"` \| `"sharp"`

Determines the overall roundness of the UI.
- `round`: Large border radius
- `soft`: Moderate border radius (default)
- `sharp`: Minimal border radius

#### Default

```ts
'soft'
```
