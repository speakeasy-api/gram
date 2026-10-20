# TypeScript Framework

This directory contains the TypeScript packages for Speakeasy.

## Packages

- `create-function` - Deprecated scaffolder for new Gram functions (`pnpm create @gram-ai/function`). Use `speakeasy functions init` instead.
- `functions` - Core framework for building Speakeasy functions, published as `@speakeasy-api/functions`
- `functions-compat` - Deprecated `@gram-ai/functions` package. It depends on `@speakeasy-api/functions` at the same version and re-exports it, including every subpath and the `gf` command, so existing projects keep working

The project templates live in `cli/internal/functions/templates`, where the
`speakeasy` CLI embeds them. `create-function` copies them into its package
when it builds.

## Local Development

### Testing `create-function` locally

The `create-function` package is designed to be run via `pnpm create @gram-ai/function`. To test it locally:

1. **Build the package:**

   ```bash
   cd create-function
   pnpm build
   ```

2. **Link it globally:**

   ```bash
   pnpm link --global
   ```

3. **Test it:**

   ```bash
   create-function
   ```

4. **After making changes:**

   ```bash
   pnpm build  # Rebuild
   create-function  # Test again
   ```

5. **To unlink when done:**
   ```bash
   pnpm unlink --global
   ```
