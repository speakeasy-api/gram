import { createCatalog } from '@json-render/core'
import { z } from 'zod'

/**
 * Generative UI Catalog
 *
 * Defines all available components for LLM-generated UI.
 * Components map to shadcn/ui primitives in the ui/ directory.
 */
export const catalog = createCatalog({
  name: 'generative-ui',
  components: {
    // Layout
    Stack: {
      props: z.object({
        direction: z.enum(['horizontal', 'vertical']).optional(),
        gap: z.enum(['sm', 'md', 'lg']).optional(),
        align: z.enum(['start', 'center', 'end', 'stretch']).optional(),
        justify: z
          .enum(['start', 'center', 'end', 'between', 'around'])
          .optional(),
        className: z.string().optional(),
      }),
      hasChildren: true,
      description: 'Flex layout container for arranging child elements',
    },

    Grid: {
      props: z.object({
        columns: z.number().optional(),
        gap: z.enum(['sm', 'md', 'lg']).optional(),
        className: z.string().optional(),
      }),
      hasChildren: true,
      description: 'Grid layout for arranging items in columns',
    },

    Card: {
      props: z.object({
        title: z.string().optional(),
        className: z.string().optional(),
      }),
      hasChildren: true,
      description: 'Container with optional title, border and padding',
    },

    // Typography
    Text: {
      props: z.object({
        content: z.string(),
        variant: z.enum(['default', 'muted', 'small', 'large']).optional(),
        className: z.string().optional(),
      }),
      description: 'Text content with styling variants',
    },

    // Data Display
    Metric: {
      props: z.object({
        label: z.string(),
        value: z.union([z.string(), z.number()]),
        format: z.enum(['number', 'currency', 'percent']).optional(),
        className: z.string().optional(),
      }),
      description:
        'Display a key metric with label and formatted value (e.g., revenue, users)',
    },

    Badge: {
      props: z.object({
        text: z.string().optional(),
        variant: z
          .enum(['default', 'secondary', 'destructive', 'outline'])
          .optional(),
        className: z.string().optional(),
      }),
      hasChildren: true,
      description: 'Status badge or tag for categorization',
    },

    Progress: {
      props: z.object({
        value: z.number(),
        max: z.number().optional(),
        className: z.string().optional(),
      }),
      description: 'Progress bar showing completion percentage',
    },

    Table: {
      props: z.object({
        headers: z.array(z.string()).optional(),
        rows: z.array(z.array(z.union([z.string(), z.number()]))),
        className: z.string().optional(),
      }),
      description: 'Data table with headers and rows',
    },

    List: {
      props: z.object({
        items: z.array(z.string()),
        ordered: z.boolean().optional(),
        className: z.string().optional(),
      }),
      description: 'Ordered or unordered list of items',
    },

    // Feedback
    Alert: {
      props: z.object({
        title: z.string(),
        description: z.string().optional(),
        variant: z.enum(['default', 'destructive']).optional(),
      }),
      description: 'Alert message for important information or errors',
    },

    // Structure
    Separator: {
      props: z.object({
        orientation: z.enum(['horizontal', 'vertical']).optional(),
        className: z.string().optional(),
      }),
      description: 'Visual divider between content sections',
    },

    Divider: {
      props: z.object({
        orientation: z.enum(['horizontal', 'vertical']).optional(),
        className: z.string().optional(),
      }),
      description:
        'Visual divider between content sections (alias for Separator)',
    },

    // Interactive
    Accordion: {
      props: z.object({
        type: z.enum(['single', 'multiple']).optional(),
      }),
      hasChildren: true,
      description: 'Collapsible accordion container',
    },

    AccordionItem: {
      props: z.object({
        value: z.string(),
        title: z.string(),
      }),
      hasChildren: true,
      description: 'Individual accordion item with trigger and content',
    },

    Tabs: {
      props: z.object({
        defaultValue: z.string().optional(),
        tabs: z.array(
          z.object({
            value: z.string(),
            label: z.string(),
          })
        ),
      }),
      hasChildren: true,
      description: 'Tabbed content container',
    },

    TabContent: {
      props: z.object({
        value: z.string(),
      }),
      hasChildren: true,
      description: 'Content panel for a specific tab',
    },

    // Actions
    Button: {
      props: z.object({
        label: z.string(),
        variant: z
          .enum(['default', 'secondary', 'destructive', 'outline', 'ghost'])
          .optional(),
        size: z.enum(['default', 'sm', 'lg', 'icon']).optional(),
        disabled: z.boolean().optional(),
        action: z.string().optional(),
        actionParams: z.record(z.string(), z.unknown()).optional(),
      }),
      description:
        'Clickable button that can trigger actions. Use action/actionParams to call backend functions.',
    },

    ActionButton: {
      props: z.object({
        label: z.string(),
        action: z.string(),
        args: z.record(z.string(), z.unknown()).optional(),
        variant: z
          .enum(['default', 'secondary', 'destructive', 'outline', 'ghost'])
          .optional(),
        size: z.enum(['default', 'sm', 'lg', 'icon']).optional(),
        className: z.string().optional(),
      }),
      description:
        'Button that triggers a frontend tool call directly without LLM roundtrip',
    },

    // Form Elements
    Input: {
      props: z.object({
        label: z.string().optional(),
        placeholder: z.string().optional(),
        type: z.enum(['text', 'email', 'password', 'number', 'tel']).optional(),
        valuePath: z.string(),
      }),
      description: 'Text input field with optional label',
    },

    Checkbox: {
      props: z.object({
        label: z.string().optional(),
        valuePath: z.string(),
        defaultChecked: z.boolean().optional(),
      }),
      description: 'Checkbox input with label',
    },

    Select: {
      props: z.object({
        placeholder: z.string().optional(),
        valuePath: z.string(),
        options: z.array(
          z.object({
            value: z.string(),
            label: z.string(),
          })
        ),
      }),
      description: 'Dropdown select input',
    },

    // Display
    Avatar: {
      props: z.object({
        src: z.string().optional(),
        alt: z.string().optional(),
        fallback: z.string(),
      }),
      description: 'User avatar with image and fallback initials',
    },

    Skeleton: {
      props: z.object({
        width: z.string().optional(),
        height: z.string().optional(),
        className: z.string().optional(),
      }),
      description: 'Loading placeholder skeleton',
    },
  },
})

export type CatalogComponentProps = typeof catalog extends {
  components: infer C
}
  ? {
      [K in keyof C]: C[K] extends { props: infer P }
        ? z.infer<P extends z.ZodType ? P : never>
        : never
    }
  : never
