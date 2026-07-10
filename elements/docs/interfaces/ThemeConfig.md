[**@gram-ai/elements v1.42.0**](../README.md)

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

> `optional` **colorScheme?**: `"system"` \| `"light"` \| `"dark"`

The color scheme to use for the UI.

#### Default

```ts
'light'
```

***

### density?

> `optional` **density?**: `"compact"` \| `"normal"` \| `"spacious"`

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

> `optional` **radius?**: `"round"` \| `"soft"` \| `"sharp"`

Determines the overall roundness of the UI.
- `round`: Large border radius
- `soft`: Moderate border radius (default)
- `sharp`: Minimal border radius

#### Default

```ts
'soft'
```

***

### customCss?

> `optional` **customCss?**: `string`

Extra CSS injected into the Elements shadow root after the built-in
stylesheet. Elements renders inside a shadow DOM, so host-page styles
cannot reach its internals — this is the supported escape hatch for
embedders that need to restyle specific components (targeting the
stable `aui-*` class hooks).
