# TypeScript Framework

This directory contains the TypeScript packages for Gram.

## Packages

- `create-function` - CLI tool for scaffolding new Gram functions (`pnpm create @gram-ai/function`)
- `functions` - Core framework for building Gram functions

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
