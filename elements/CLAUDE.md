# Elements Component Library Guide

This is `@gram-ai/elements` - a React component library for building AI chat experiences with MCP servers.

## Quick Reference

```bash
# Development
pnpm dev              # Start Storybook on localhost:6006
pnpm build            # Build library
pnpm test             # Run Vitest tests
pnpm lint             # ESLint + Prettier check
pnpm format           # Auto-format with Prettier

# Type checking
pnpm tsc --noEmit     # Check types without building
```

## Tech Stack

| Category | Technology |
|----------|------------|
| Framework | React 19.2 |
| Language | TypeScript (strict mode) |
| Styling | TailwindCSS 4.1 + CVA |
| Primitives | Radix UI |
| Chat UI | @assistant-ui/react |
| Build | Vite 7.1 |
| Docs | Storybook 10 |
| Testing | Vitest 4.0 |

## Directory Structure

```
src/
├── components/
│   ├── ui/              # Base primitives (button, dialog, tooltip)
│   ├── assistant-ui/    # Chat-specific components (thread, modal)
│   ├── Chat/            # Main Chat component + stories
│   └── ...
├── hooks/               # Custom React hooks
├── contexts/            # React Context providers
├── lib/                 # Utilities (cn, api, tools)
├── types/               # TypeScript definitions
└── plugins/             # Extensible plugin system
```

## Component Patterns

### Creating a New Component

```tsx
// src/components/ui/my-component.tsx
import * as React from 'react'
import { cn } from '@/lib/utils'
import { cva, type VariantProps } from 'class-variance-authority'

const myComponentVariants = cva(
  'base-classes-here', // Base styles
  {
    variants: {
      variant: {
        default: 'default-variant-classes',
        secondary: 'secondary-variant-classes',
      },
      size: {
        sm: 'h-8 px-3 text-sm',
        md: 'h-10 px-4',
        lg: 'h-12 px-6 text-lg',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'md',
    },
  }
)

export interface MyComponentProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof myComponentVariants> {
  // Additional props
}

const MyComponent = React.forwardRef<HTMLDivElement, MyComponentProps>(
  ({ className, variant, size, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={cn(myComponentVariants({ variant, size, className }))}
        {...props}
      />
    )
  }
)
MyComponent.displayName = 'MyComponent'

export { MyComponent, myComponentVariants }
```

### Creating a Story

```tsx
// src/components/ui/my-component.stories.tsx
import type { Meta, StoryObj } from '@storybook/react'
import { MyComponent } from './my-component'

const meta: Meta<typeof MyComponent> = {
  title: 'UI/MyComponent',
  component: MyComponent,
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'select',
      options: ['default', 'secondary'],
    },
    size: {
      control: 'select',
      options: ['sm', 'md', 'lg'],
    },
  },
}

export default meta
type Story = StoryObj<typeof MyComponent>

export const Default: Story = {
  args: {
    children: 'My Component',
  },
}

export const Secondary: Story = {
  args: {
    variant: 'secondary',
    children: 'Secondary Variant',
  },
}
```

## Styling Rules

### DO
- Use `cn()` helper for className merging
- Use CVA for component variants
- Use Tailwind utility classes
- Use CSS custom properties for theming
- Use `@/` alias for imports

### DON'T
- Hardcode colors (use theme variables)
- Use inline styles
- Create one-off CSS files
- Mix relative imports with alias imports

### Theme Variables

```tsx
// Access theme via hooks
import { useDensity, useRadius, useThemeProps } from '@/hooks'

const density = useDensity()    // 'compact' | 'default' | 'comfortable'
const radius = useRadius()      // 'none' | 'sm' | 'md' | 'lg' | 'full'
const theme = useThemeProps()   // Full theme object
```

## Import Conventions

Always use the `@/` alias:

```tsx
// Good
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useElements } from '@/hooks/useElements'

// Bad - avoid relative imports
import { Button } from '../../../components/ui/button'
import { cn } from '../../lib/utils'
```

## Hook Patterns

### Using Context

```tsx
import { useElements } from '@/hooks/useElements'

function MyComponent() {
  const {
    config,
    runtime,
    isConnected
  } = useElements()

  // ...
}
```

### Custom Hook Structure

```tsx
// src/hooks/useMyHook.ts
import { useState, useCallback, useMemo } from 'react'

export interface UseMyHookOptions {
  initialValue?: string
}

export interface UseMyHookReturn {
  value: string
  setValue: (v: string) => void
  reset: () => void
}

export function useMyHook(options: UseMyHookOptions = {}): UseMyHookReturn {
  const { initialValue = '' } = options
  const [value, setValue] = useState(initialValue)

  const reset = useCallback(() => {
    setValue(initialValue)
  }, [initialValue])

  return useMemo(() => ({
    value,
    setValue,
    reset,
  }), [value, reset])
}
```

## Testing

### Component Tests

```tsx
// src/components/ui/button.test.tsx
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import { Button } from './button'

describe('Button', () => {
  it('renders children', () => {
    render(<Button>Click me</Button>)
    expect(screen.getByRole('button')).toHaveTextContent('Click me')
  })

  it('handles click events', async () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>Click</Button>)

    await userEvent.click(screen.getByRole('button'))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it('applies variant classes', () => {
    render(<Button variant="secondary">Secondary</Button>)
    expect(screen.getByRole('button')).toHaveClass('secondary-class')
  })
})
```

### Hook Tests

```tsx
// src/hooks/useMyHook.test.ts
import { renderHook, act } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { useMyHook } from './useMyHook'

describe('useMyHook', () => {
  it('initializes with default value', () => {
    const { result } = renderHook(() => useMyHook())
    expect(result.current.value).toBe('')
  })

  it('updates value', () => {
    const { result } = renderHook(() => useMyHook())

    act(() => {
      result.current.setValue('new value')
    })

    expect(result.current.value).toBe('new value')
  })
})
```

## Error Handling

Use the error tracking system instead of console:

```tsx
// Bad
console.error('Something went wrong', error)

// Good
import { trackError } from '@/lib/errorTracking'
trackError(error, { context: 'MyComponent' })
```

## Accessibility

All components must be accessible:

- Use semantic HTML elements
- Include ARIA attributes where needed
- Support keyboard navigation
- Test with screen readers
- Use Radix UI primitives (they handle a11y)

```tsx
// Good - uses Radix Dialog (handles focus trap, escape key, etc.)
import * as Dialog from '@radix-ui/react-dialog'

// Good - proper button semantics
<button type="button" aria-label="Close dialog">
  <XIcon aria-hidden="true" />
</button>
```

## Common Pitfalls

1. **Forgetting forwardRef** - UI components need ref forwarding
2. **Missing displayName** - Set it for DevTools debugging
3. **Inline objects in props** - Creates new references, causes re-renders
4. **Not using cn()** - Class conflicts without proper merging
5. **Relative imports** - Use `@/` alias consistently
6. **Console statements** - Use error tracking instead
