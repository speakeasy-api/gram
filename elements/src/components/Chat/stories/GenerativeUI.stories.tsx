import type { Meta, StoryFn } from '@storybook/react-vite'
import { Chat } from '..'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Plugins/Generative UI',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

/**
 * E-commerce store assistant with natural task-focused prompts.
 */
export const StoreAssistant: Story = () => <Chat />
StoreAssistant.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Store Assistant',
        subtitle: 'How can I help you today?',
        suggestions: [
          {
            title: 'My Orders',
            label: 'View recent orders',
            prompt: 'Show me my current orders',
          },
          {
            title: 'Browse Products',
            label: "See what's available",
            prompt: 'What products do you have?',
          },
          {
            title: 'Find Deals',
            label: 'Best prices',
            prompt: 'What are the best deals right now?',
          },
        ],
      },
    },
  },
}

/**
 * Shopping assistant for product discovery.
 */
export const ShoppingAssistant: Story = () => <Chat />
ShoppingAssistant.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Shopping Assistant',
        subtitle: 'Find the perfect product',
        suggestions: [
          {
            title: 'New Arrivals',
            label: 'Latest products',
            prompt: 'Show me the newest products',
          },
          {
            title: 'Gift Ideas',
            label: 'Under $50',
            prompt: 'I need a gift under $50, what do you recommend?',
          },
          {
            title: 'Compare Options',
            label: 'Help me decide',
            prompt: 'Can you compare your top 3 products for me?',
          },
        ],
      },
    },
  },
}

/**
 * Store management assistant.
 */
export const StoreManager: Story = () => <Chat />
StoreManager.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Store Manager',
        subtitle: 'Manage your store',
        suggestions: [
          {
            title: 'Stock Check',
            label: 'Inventory status',
            prompt: 'Which products are running low on stock?',
          },
          {
            title: 'Sales Summary',
            label: 'How are we doing?',
            prompt: 'How are sales looking this month?',
          },
          {
            title: 'Top Sellers',
            label: 'Best performers',
            prompt: 'What are our best selling products?',
          },
        ],
      },
    },
  },
}
