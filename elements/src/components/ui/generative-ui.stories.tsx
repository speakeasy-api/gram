import type { Meta, StoryObj } from '@storybook/react-vite'
import { GenerativeUI } from './generative-ui'

const meta: Meta<typeof GenerativeUI> = {
  title: 'Components/Generative UI',
  component: GenerativeUI,
  parameters: {
    layout: 'fullscreen',
  },
  decorators: [
    (Story) => (
      <div className="bg-background text-foreground min-h-screen p-6">
        <div className="max-w-2xl">
          <Story />
        </div>
      </div>
    ),
  ],
}

export default meta
type Story = StoryObj<typeof GenerativeUI>

/**
 * Basic layout with Stack and Text components.
 */
export const BasicLayout: Story = {
  args: {
    content: {
      type: 'Stack',
      props: { direction: 'vertical', gap: 'md' },
      children: [
        { type: 'Text', props: { content: 'Hello World', variant: 'heading' } },
        {
          type: 'Text',
          props: { content: 'This is a body text example.', variant: 'body' },
        },
        {
          type: 'Text',
          props: { content: 'Caption text for extra info', variant: 'caption' },
        },
      ],
    },
  },
}

/**
 * Card with metrics displaying key statistics.
 */
export const MetricsCard: Story = {
  args: {
    content: {
      type: 'Card',
      props: { title: 'Monthly Overview' },
      children: [
        {
          type: 'Grid',
          props: { columns: 3, gap: 'md' },
          children: [
            {
              type: 'Metric',
              props: { label: 'Revenue', value: 12500, format: 'currency' },
            },
            {
              type: 'Metric',
              props: { label: 'Orders', value: 156, format: 'number' },
            },
            {
              type: 'Metric',
              props: { label: 'Conversion', value: 0.032, format: 'percent' },
            },
          ],
        },
      ],
    },
  },
}

/**
 * Data table with headers and rows.
 */
export const DataTable: Story = {
  args: {
    content: {
      type: 'Card',
      props: { title: 'Recent Orders' },
      children: [
        {
          type: 'Table',
          props: {
            headers: ['Order ID', 'Customer', 'Amount', 'Status'],
            rows: [
              ['#1001', 'Alice Johnson', '$125.00', 'Shipped'],
              ['#1002', 'Bob Smith', '$89.50', 'Processing'],
              ['#1003', 'Carol White', '$234.00', 'Delivered'],
              ['#1004', 'David Brown', '$56.75', 'Pending'],
            ],
          },
        },
      ],
    },
  },
}

/**
 * Badges showing different status variants.
 */
export const BadgeVariants: Story = {
  args: {
    content: {
      type: 'Stack',
      props: { direction: 'horizontal', gap: 'sm' },
      children: [
        { type: 'Badge', props: { content: 'Default' } },
        {
          type: 'Badge',
          props: { content: 'Secondary', variant: 'secondary' },
        },
        { type: 'Badge', props: { content: 'Success', variant: 'success' } },
        { type: 'Badge', props: { content: 'Warning', variant: 'warning' } },
        {
          type: 'Badge',
          props: { content: 'Destructive', variant: 'destructive' },
        },
        { type: 'Badge', props: { content: 'Outline', variant: 'outline' } },
      ],
    },
  },
}

/**
 * Alert messages for different contexts.
 */
export const Alerts: Story = {
  args: {
    content: {
      type: 'Stack',
      props: { direction: 'vertical', gap: 'md' },
      children: [
        {
          type: 'Alert',
          props: {
            title: 'Information',
            description: 'This is a default informational alert.',
          },
        },
        {
          type: 'Alert',
          props: {
            title: 'Error',
            description: 'Something went wrong. Please try again.',
            variant: 'destructive',
          },
        },
      ],
    },
  },
}

/**
 * Progress bars showing completion.
 */
export const ProgressBars: Story = {
  args: {
    content: {
      type: 'Stack',
      props: { direction: 'vertical', gap: 'md' },
      children: [
        {
          type: 'Stack',
          props: { direction: 'vertical', gap: 'sm' },
          children: [
            {
              type: 'Text',
              props: { content: 'Upload Progress', variant: 'body' },
            },
            { type: 'Progress', props: { value: 75 } },
          ],
        },
        {
          type: 'Stack',
          props: { direction: 'vertical', gap: 'sm' },
          children: [
            {
              type: 'Text',
              props: { content: 'Task Completion', variant: 'body' },
            },
            { type: 'Progress', props: { value: 45 } },
          ],
        },
      ],
    },
  },
}

/**
 * Lists - ordered and unordered.
 */
export const Lists: Story = {
  args: {
    content: {
      type: 'Grid',
      props: { columns: 2, gap: 'lg' },
      children: [
        {
          type: 'Card',
          props: { title: 'Unordered List' },
          children: [
            {
              type: 'List',
              props: {
                items: [
                  'First item',
                  'Second item',
                  'Third item',
                  'Fourth item',
                ],
              },
            },
          ],
        },
        {
          type: 'Card',
          props: { title: 'Ordered List' },
          children: [
            {
              type: 'List',
              props: {
                items: ['Step one', 'Step two', 'Step three', 'Step four'],
                ordered: true,
              },
            },
          ],
        },
      ],
    },
  },
}

/**
 * Accordion with collapsible sections.
 */
export const AccordionExample: Story = {
  args: {
    content: {
      type: 'Card',
      props: { title: 'FAQ' },
      children: [
        {
          type: 'Accordion',
          props: { type: 'single' },
          children: [
            {
              type: 'AccordionItem',
              props: { value: 'q1', title: 'What is your return policy?' },
              children: [
                {
                  type: 'Text',
                  props: {
                    content:
                      'We offer a 30-day return policy for all unused items in original packaging.',
                    variant: 'body',
                  },
                },
              ],
            },
            {
              type: 'AccordionItem',
              props: { value: 'q2', title: 'How long does shipping take?' },
              children: [
                {
                  type: 'Text',
                  props: {
                    content:
                      'Standard shipping takes 5-7 business days. Express shipping is available for 2-3 day delivery.',
                    variant: 'body',
                  },
                },
              ],
            },
            {
              type: 'AccordionItem',
              props: { value: 'q3', title: 'Do you ship internationally?' },
              children: [
                {
                  type: 'Text',
                  props: {
                    content:
                      'Yes! We ship to over 50 countries worldwide. Shipping costs vary by destination.',
                    variant: 'body',
                  },
                },
              ],
            },
          ],
        },
      ],
    },
  },
}

/**
 * Tabs with multiple content panels.
 */
export const TabsExample: Story = {
  args: {
    content: {
      type: 'Card',
      props: { title: 'Product Details' },
      children: [
        {
          type: 'Tabs',
          props: {
            defaultValue: 'overview',
            tabs: [
              { value: 'overview', label: 'Overview' },
              { value: 'specs', label: 'Specifications' },
              { value: 'reviews', label: 'Reviews' },
            ],
          },
          children: [
            {
              type: 'TabContent',
              props: { value: 'overview' },
              children: [
                {
                  type: 'Text',
                  props: {
                    content:
                      'This premium product features cutting-edge technology and elegant design.',
                    variant: 'body',
                  },
                },
              ],
            },
            {
              type: 'TabContent',
              props: { value: 'specs' },
              children: [
                {
                  type: 'List',
                  props: {
                    items: [
                      'Weight: 1.2 kg',
                      'Dimensions: 30x20x10 cm',
                      'Material: Aluminum',
                      'Battery: 10 hours',
                    ],
                  },
                },
              ],
            },
            {
              type: 'TabContent',
              props: { value: 'reviews' },
              children: [
                {
                  type: 'Text',
                  props: {
                    content: '4.8 out of 5 stars based on 256 reviews',
                    variant: 'body',
                  },
                },
              ],
            },
          ],
        },
      ],
    },
  },
}

/**
 * Form elements - inputs, checkboxes, and selects.
 */
export const FormElements: Story = {
  args: {
    content: {
      type: 'Card',
      props: { title: 'Contact Form' },
      children: [
        {
          type: 'Stack',
          props: { direction: 'vertical', gap: 'md' },
          children: [
            {
              type: 'Input',
              props: {
                label: 'Full Name',
                placeholder: 'Enter your name',
                valuePath: 'name',
              },
            },
            {
              type: 'Input',
              props: {
                label: 'Email',
                placeholder: 'Enter your email',
                type: 'email',
                valuePath: 'email',
              },
            },
            {
              type: 'Select',
              props: {
                placeholder: 'Select a topic',
                valuePath: 'topic',
                options: [
                  { value: 'general', label: 'General Inquiry' },
                  { value: 'support', label: 'Technical Support' },
                  { value: 'sales', label: 'Sales Question' },
                  { value: 'feedback', label: 'Feedback' },
                ],
              },
            },
            {
              type: 'Checkbox',
              props: {
                label: 'Subscribe to newsletter',
                valuePath: 'subscribe',
              },
            },
            {
              type: 'Button',
              props: { label: 'Submit', variant: 'default' },
            },
          ],
        },
      ],
    },
  },
}

/**
 * Avatar and skeleton loading states.
 */
export const AvatarsAndSkeletons: Story = {
  args: {
    content: {
      type: 'Stack',
      props: { direction: 'vertical', gap: 'lg' },
      children: [
        {
          type: 'Stack',
          props: { direction: 'horizontal', gap: 'md', align: 'center' },
          children: [
            { type: 'Avatar', props: { fallback: 'JD' } },
            { type: 'Avatar', props: { fallback: 'AS' } },
            { type: 'Avatar', props: { fallback: 'MK' } },
          ],
        },
        {
          type: 'Card',
          props: { title: 'Loading State' },
          children: [
            {
              type: 'Stack',
              props: { direction: 'vertical', gap: 'sm' },
              children: [
                { type: 'Skeleton', props: { height: '1.5rem', width: '60%' } },
                { type: 'Skeleton', props: { height: '1rem', width: '100%' } },
                { type: 'Skeleton', props: { height: '1rem', width: '80%' } },
              ],
            },
          ],
        },
      ],
    },
  },
}

/**
 * Complete dashboard example combining multiple components.
 */
export const Dashboard: Story = {
  args: {
    content: {
      type: 'Stack',
      props: { direction: 'vertical', gap: 'lg' },
      children: [
        {
          type: 'Text',
          props: { content: 'Store Dashboard', variant: 'heading' },
        },
        {
          type: 'Grid',
          props: { columns: 4, gap: 'md' },
          children: [
            {
              type: 'Card',
              children: [
                {
                  type: 'Metric',
                  props: {
                    label: 'Total Revenue',
                    value: 45231,
                    format: 'currency',
                  },
                },
              ],
            },
            {
              type: 'Card',
              children: [
                {
                  type: 'Metric',
                  props: { label: 'Orders', value: 1234, format: 'number' },
                },
              ],
            },
            {
              type: 'Card',
              children: [
                {
                  type: 'Metric',
                  props: { label: 'Customers', value: 567, format: 'number' },
                },
              ],
            },
            {
              type: 'Card',
              children: [
                {
                  type: 'Metric',
                  props: {
                    label: 'Conversion',
                    value: 0.0312,
                    format: 'percent',
                  },
                },
              ],
            },
          ],
        },
        { type: 'Separator' },
        {
          type: 'Card',
          props: { title: 'Recent Orders' },
          children: [
            {
              type: 'Table',
              props: {
                headers: ['Order', 'Customer', 'Status', 'Amount'],
                rows: [
                  ['#3210', 'Olivia Martin', 'Shipped', '$42.25'],
                  ['#3209', 'Ava Johnson', 'Processing', '$74.99'],
                  ['#3208', 'Michael Chen', 'Delivered', '$124.00'],
                  ['#3207', 'Lisa Wang', 'Pending', '$64.75'],
                ],
              },
            },
          ],
        },
      ],
    },
  },
}
