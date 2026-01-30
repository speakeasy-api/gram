# Integration Test Apps

These apps verify that `@gram-ai/elements` works with older React versions.

## Architecture

```
test-apps/
├── shared/              # Shared code across all test apps
│   ├── App.tsx          # Common test UI component
│   ├── server.base.ts   # Session server factory
│   ├── vite.config.base.ts  # Vite config factory
│   ├── tsconfig.base.json   # Shared TypeScript config
│   └── index.html       # Common HTML template
├── react-16/            # React 16.14 test app
├── react-17/            # React 17 test app
└── README.md
```

To add a new test app (e.g. React 18):

1. Copy any existing app directory
2. Update `package.json` with the React version
3. Update `server.ts` with a unique port
4. Update `vite.config.ts` with the matching port

## Running a test app

Each app has its own `node_modules` to avoid version conflicts with the main project.

```bash
# From the test app directory (e.g. react-17/)
pnpm install

# Terminal 1: start the session server
GRAM_API_KEY=your-key pnpm server

# Terminal 2: start the vite dev server
pnpm dev
```

## What's tested

- The `compat.ts` polyfills install correctly (`useSyncExternalStore`, `useId`, `useInsertionEffect`)
- `ElementsProvider` renders without errors
- `Chat` component renders and accepts messages
- Session endpoint proxying works via the dev server

## Notes

- React 16.14+ is the minimum because it includes the `react/jsx-runtime` module
  needed by the new JSX transform. React 16.8–16.13 would require additional
  bundler configuration to alias `react/jsx-runtime`.
- These apps are **not** part of the main pnpm workspace — they have their own
  `node_modules` to avoid version conflicts with the main project's React 19.
