---
description: Generate tests for a specific file or function
---

# Generate Tests

Generate comprehensive tests for the specified file or function following Gram's testing conventions.

## Process

1. **Identify the target**: Read the file or function to be tested
   - If a file path is provided: generate tests for that file
   - If a function name is provided: find and test that specific function

2. **Analyze existing tests**: Look for existing test files to match patterns
   - Go: Look for `*_test.go` files in the same directory
   - TypeScript: Look for `*.test.ts` or `*.spec.ts` files

3. **Generate tests** following Gram conventions:

   ### Go Tests
   ```go
   func TestFunctionName(t *testing.T) {
       // Use table-driven tests
       tests := []struct {
           name    string
           input   InputType
           want    OutputType
           wantErr bool
       }{
           {
               name:  "it does something when condition",
               input: InputType{...},
               want:  OutputType{...},
           },
       }

       for _, tt := range tests {
           t.Run(tt.name, func(t *testing.T) {
               got, err := FunctionUnderTest(tt.input)
               if tt.wantErr {
                   require.Error(t, err)
                   return
               }
               require.NoError(t, err)
               require.Equal(t, tt.want, got)
           })
       }
   }
   ```

   **Go test requirements:**
   - Use `github.com/stretchr/testify/require` for assertions (NOT assert)
   - Use table-driven tests with descriptive "it" prefix names
   - For integration tests, use `testenv.Launch()` in `TestMain`
   - For ClickHouse tests, add `time.Sleep(100ms)` after inserts

   ### TypeScript Tests
   ```typescript
   import { describe, it, expect } from 'vitest';

   describe('ComponentOrFunction', () => {
     it('should do something when condition', () => {
       // Arrange
       const input = ...;

       // Act
       const result = functionUnderTest(input);

       // Assert
       expect(result).toEqual(expected);
     });
   });
   ```

   **TypeScript test requirements:**
   - Use Vitest or Jest (check package.json)
   - Mock external dependencies with `vi.mock()` or `jest.mock()`
   - Use React Testing Library for component tests

4. **Test coverage** should include:
   - Happy path (expected behavior)
   - Edge cases (empty inputs, boundary values)
   - Error cases (invalid inputs, failure scenarios)
   - For Go: nil checks, context cancellation
   - For TypeScript: async/await handling, error boundaries

5. **Run the tests** to verify they pass:
   - Go: `mise test:server -- -run TestName ./path/to/package`
   - TypeScript: `pnpm test -- --filter=<test-file>`

6. **Output**: Write tests to appropriate file location

## Arguments
- `$ARGUMENTS`: File path or function name to test
