---
description: Create or update a Storybook story
---

# Create Story

Create a comprehensive Storybook story for a component.

## Process

1. **Identify the component**: Read the component file to understand:
   - Props interface
   - Variants (CVA)
   - Expected behavior
   - Edge cases

2. **Create story structure**:

   ```tsx
   import type { Meta, StoryObj } from '@storybook/react'
   import { ComponentName } from './component-name'

   const meta: Meta<typeof ComponentName> = {
     title: 'Category/ComponentName',  // UI/, AssistantUI/, Chat/
     component: ComponentName,
     tags: ['autodocs'],
     parameters: {
       layout: 'centered',  // or 'fullscreen', 'padded'
     },
     argTypes: {
       // Control types for props
       variant: {
         control: 'select',
         options: ['default', 'secondary'],
         description: 'Visual variant',
       },
       size: {
         control: 'radio',
         options: ['sm', 'md', 'lg'],
       },
       disabled: {
         control: 'boolean',
       },
       onClick: { action: 'clicked' },
     },
   }

   export default meta
   type Story = StoryObj<typeof ComponentName>
   ```

3. **Add story variants**:

   ```tsx
   // Default state
   export const Default: Story = {
     args: {
       children: 'Default content',
     },
   }

   // All variants
   export const Variants: Story = {
     render: () => (
       <div className="flex gap-4">
         <ComponentName variant="default">Default</ComponentName>
         <ComponentName variant="secondary">Secondary</ComponentName>
       </div>
     ),
   }

   // All sizes
   export const Sizes: Story = {
     render: () => (
       <div className="flex items-center gap-4">
         <ComponentName size="sm">Small</ComponentName>
         <ComponentName size="md">Medium</ComponentName>
         <ComponentName size="lg">Large</ComponentName>
       </div>
     ),
   }

   // Interactive state
   export const Interactive: Story = {
     args: {
       children: 'Hover or click me',
     },
     play: async ({ canvasElement }) => {
       // Interaction tests
     },
   }

   // Edge cases
   export const LongContent: Story = {
     args: {
       children: 'Very long content that might overflow...',
     },
   }

   export const Empty: Story = {
     args: {
       children: '',
     },
   }

   // With decorators (if needs context)
   export const WithProvider: Story = {
     decorators: [
       (Story) => (
         <SomeProvider>
           <Story />
         </SomeProvider>
       ),
     ],
   }
   ```

4. **Verify in Storybook**:
   ```bash
   pnpm dev
   # Open http://localhost:6006
   ```

## Story Checklist

- [ ] Has `autodocs` tag for auto-documentation
- [ ] Default story shows typical usage
- [ ] All variants have stories
- [ ] All sizes have stories
- [ ] Interactive states covered (hover, focus, active, disabled)
- [ ] Edge cases (empty, overflow, error states)
- [ ] Uses argTypes for prop controls
- [ ] Proper category in title path

## Arguments
- `$ARGUMENTS`: Component name or path to create story for
