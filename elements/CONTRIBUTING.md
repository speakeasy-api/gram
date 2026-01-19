# Contributing to `@gram-ai/elements`

## Setup

Ensure that you have your Gram API key setup in your `.env.local` file (rename the template).

Then simply run the Storybook which has a preconfigured dev middleware for chat completions so that you can test the components against a real LLM:

```bash
pnpm storybook
```

## Third party dependencies

If adding a heavy dependency, please make sure you mark it as a peer in `peerDependencies`, mark it as optional in `peerDependenciesMeta` (so the server package doesn't require it), and mark it as an `external` in the vite configuration.

## Documentation

We use TypeDoc to automatically generate markdown documentation via a github action - the documentation is generated from the TypeScript types. However you can generate documentation locally by following the guide below:

```bash
# Generate documentation
pnpm run docs

# Watch mode (regenerate on changes)
pnpm run docs:watch
```

## Chromatic

Chromatic is used to automatically test the library for visual regressions. The tests are run automatically on every pull request to the `main` branch. Before merging a pull request, you must manually approve the changes in the Chromatic dashboard by clicking on the PR check. Anyone with contributor level access to the Gram repo can approve the changes.
