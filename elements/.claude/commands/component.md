---
description: Create a new React component with proper patterns
---

# Create Component

Create a new React component following elements library patterns.

## Process

1. **Gather requirements**:
   - Component name (PascalCase)
   - Location: `ui/` (primitive) or `assistant-ui/` (chat-specific)
   - Does it need variants? (size, color, state)
   - Does it wrap a Radix primitive?

2. **Create the component file**:

   ```tsx
   // src/components/{location}/{component-name}.tsx
   import * as React from 'react'
   import { cn } from '@/lib/utils'
   import { cva, type VariantProps } from 'class-variance-authority'

   const componentVariants = cva(
     'base-styles',
     {
       variants: {
         variant: { default: '...', secondary: '...' },
         size: { sm: '...', md: '...', lg: '...' },
       },
       defaultVariants: {
         variant: 'default',
         size: 'md',
       },
     }
   )

   export interface ComponentProps
     extends React.HTMLAttributes<HTMLElement>,
       VariantProps<typeof componentVariants> {}

   const Component = React.forwardRef<HTMLElement, ComponentProps>(
     ({ className, variant, size, ...props }, ref) => (
       <element
         ref={ref}
         className={cn(componentVariants({ variant, size, className }))}
         {...props}
       />
     )
   )
   Component.displayName = 'Component'

   export { Component, componentVariants }
   ```

3. **Create the story file**:

   ```tsx
   // src/components/{location}/{component-name}.stories.tsx
   import type { Meta, StoryObj } from '@storybook/react'
   import { Component } from './{component-name}'

   const meta: Meta<typeof Component> = {
     title: '{Location}/Component',
     component: Component,
     tags: ['autodocs'],
   }
   export default meta
   type Story = StoryObj<typeof Component>

   export const Default: Story = { args: {} }
   ```

4. **Export from index** (if in ui/):
   - Add export to `src/components/ui/index.ts`

5. **Run checks**:
   ```bash
   pnpm lint
   pnpm tsc --noEmit
   pnpm dev  # Verify in Storybook
   ```

## Checklist

- [ ] Uses `forwardRef` for ref forwarding
- [ ] Has `displayName` set
- [ ] Uses `cn()` for className merging
- [ ] Uses CVA for variants (if applicable)
- [ ] Uses `@/` alias imports
- [ ] Has TypeScript interface exported
- [ ] Has Storybook story
- [ ] No hardcoded colors
- [ ] Accessible (keyboard nav, ARIA)

## Arguments
- `$ARGUMENTS`: Component name and type (e.g., "Card ui" or "MessageBubble assistant-ui")
