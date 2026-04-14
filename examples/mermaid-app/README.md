# Mermaid app (MCP Apps example)

A React + Vite app rendered as an **MCP App** (SEP-1865) through a Gram
Function. Demonstrates:

- Building a real framework-based frontend (React + TypeScript)
- Bundling it into a single self-contained HTML file with
  [`vite-plugin-singlefile`](https://github.com/richardtallent/vite-plugin-singlefile)
- Exposing that HTML as a UI resource via
  `gram.experimental_uiResource({ content })`
- Linking a tool to the widget via `meta: { "ui/resourceUri": "ui://mermaid" }`
- Passing structured data to the widget with `ctx.json(...)` → `Gram.onData(cb)`

The tool `render_diagram(diagram, title?)` accepts raw Mermaid source and the
widget renders it with the [`mermaid`](https://mermaid.js.org) library.

## Project layout

```
.
├── ui/                 # React app (Vite root)
│   ├── index.html
│   ├── main.tsx
│   ├── App.tsx
│   ├── Mermaid.tsx
│   └── styles.css
├── scripts/
│   └── inline-html.mjs # Reads dist-ui/index.html → src/inlined.ts
├── src/
│   ├── gram.ts         # Gram Function entrypoint
│   └── inlined.ts      # Generated — do not edit
├── vite.config.ts
├── gram.config.ts
├── tsconfig.json       # For src/ (gram function)
└── tsconfig.ui.json    # For ui/ (React app)
```

## Develop the UI in isolation

```bash
pnpm install
pnpm dev
```

Opens the React app standalone. When `window.Gram` is absent, `App.tsx` falls
back to a demo diagram so you can iterate on the widget without deploying.

## Build

```bash
pnpm build
```

Runs three steps:

1. `vite build` → `dist-ui/index.html` (one file, JS/CSS/assets inlined)
2. `scripts/inline-html.mjs` → writes `src/inlined.ts` exporting the HTML as a
   string constant
3. `gf build` → bundles the Gram Function

## Deploy

```bash
gram auth
pnpm push
```

## Notes

- `mermaid` is a heavy dependency (~500 KB minified). The single-file bundle
  will reflect that. If you hit payload limits, consider lazy-loading the
  library or pre-rendering SVGs server-side and shipping those instead.
- The host → widget bridge (`window.Gram.onData`) is inlined in
  `ui/index.html` so the built bundle is self-sufficient. The gram framework
  also provides this shim automatically when you use the `body` option of
  `experimental_uiResource`; we use the `content` option here because Vite
  emits a complete HTML document.
