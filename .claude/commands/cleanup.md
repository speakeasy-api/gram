---
description: Clean up code issues (console, any types, imports)
---

# Code Cleanup

Clean up common code quality issues in the elements codebase.

## Process

1. **Identify cleanup scope**:
   - Single file, directory, or entire codebase
   - Specific issue type or all issues

2. **Find and fix issues**:

### Console Statements
Find and replace with error tracking:

```bash
# Find all console statements
grep -rn "console\." src/
```

```tsx
// Before
console.error('Failed to load', error)

// After
import { trackError } from '@/lib/errorTracking'
trackError(error, { context: 'ComponentName', action: 'load' })
```

### `any` Types
Replace with proper types:

```tsx
// Before
const data: any = response.data
items.map((item: any) => item.name)

// After
interface ResponseData {
  items: Item[]
}
const data: ResponseData = response.data
items.map((item: Item) => item.name)
```

### Relative Imports
Convert to alias imports:

```tsx
// Before
import { Button } from '../../../components/ui/button'
import { cn } from '../../lib/utils'

// After
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
```

### Missing displayName
Add to forwardRef components:

```tsx
// Before
const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(...)

// After
const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(...)
Button.displayName = 'Button'
```

### Inline Object Props
Extract to prevent re-renders:

```tsx
// Before
<Component style={{ marginTop: 10 }} data={{ id: 1 }} />

// After
const style = useMemo(() => ({ marginTop: 10 }), [])
const data = useMemo(() => ({ id: 1 }), [])
<Component style={style} data={data} />
```

### Unused Imports
Remove unused imports:

```bash
# ESLint will catch these
pnpm lint
```

3. **Run verification**:
   ```bash
   pnpm lint          # Check for remaining issues
   pnpm tsc --noEmit  # Verify types
   pnpm test          # Ensure nothing broke
   ```

## Quick Commands

```bash
# Find all issues
grep -rn "console\." src/
grep -rn ": any" src/
grep -rn "from '\.\." src/

# Auto-fix formatting
pnpm format

# Lint with auto-fix
pnpm lint --fix
```

## Cleanup Checklist

- [ ] No `console.log/error/warn` in production code
- [ ] No `any` types (use `unknown` if truly unknown)
- [ ] All imports use `@/` alias
- [ ] All forwardRef components have displayName
- [ ] No inline object/array literals in JSX props
- [ ] No unused imports or variables
- [ ] Consistent formatting (Prettier)

## Arguments
- `$ARGUMENTS`: File/directory to clean, or issue type (e.g., "console", "any", "imports")
