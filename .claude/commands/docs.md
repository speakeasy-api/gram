---
description: Generate or update documentation
---

# Documentation Generation

Generate or update documentation for the specified code, feature, or component.

## Process

1. **Identify documentation scope**:
   - If a file/package is specified: document that code
   - If "api" is specified: document API endpoints
   - If "runbook" is specified: create operational documentation
   - If empty: suggest documentation improvements

2. **Analyze existing documentation**:
   - Check for existing README.md files
   - Review inline comments and godoc/jsdoc
   - Look at `/docs` directory for patterns

3. **Generate appropriate documentation**:

   ### Code Documentation (Go)
   ```go
   // FunctionName does X by doing Y.
   // It returns Z when the condition is met.
   //
   // Example:
   //
   //     result, err := FunctionName(ctx, input)
   //     if err != nil {
   //         return err
   //     }
   func FunctionName(ctx context.Context, input InputType) (OutputType, error) {
   ```

   - Document exported functions, types, and constants
   - Use godoc conventions (start with function name)
   - Include examples for complex functions

   ### Code Documentation (TypeScript)
   ```typescript
   /**
    * Brief description of what the function does.
    *
    * @param input - Description of the input parameter
    * @returns Description of what is returned
    * @throws {ErrorType} When the error condition occurs
    *
    * @example
    * ```typescript
    * const result = functionName(input);
    * ```
    */
   export function functionName(input: InputType): OutputType {
   ```

   ### README Documentation
   ```markdown
   # Package/Component Name

   Brief description of purpose.

   ## Usage

   How to use this package/component.

   ## API

   Key functions/components and their purposes.

   ## Examples

   Practical examples with code.

   ## Configuration

   Any configuration options.
   ```

   ### API Endpoint Documentation
   - Document in Goa design files (`server/design/`)
   - Use `Meta("openapi:...")` for OpenAPI metadata
   - Include request/response examples

   ### Runbook Documentation
   - Located in `/docs/runbooks/`
   - Include problem description, symptoms, diagnosis steps, resolution
   - Add monitoring/alerting context

4. **Documentation standards**:
   - Write in clear, concise English
   - Use active voice
   - Include practical examples
   - Keep documentation close to code when possible
   - Update existing docs rather than creating duplicates

5. **Verify accuracy**: Ensure documentation matches actual code behavior

## Arguments
- `$ARGUMENTS`: File path, package name, "api", "runbook", or feature name
