import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { ZapIcon } from 'lucide-react'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Modal',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

export const CustomIcon: Story = () => <Chat />
CustomIcon.parameters = {
  elements: {
    config: {
      modal: {
        defaultOpen: false,
        icon: (state: 'open' | 'closed' | undefined) => (
          <ZapIcon
            data-state={state}
            className="aui-modal-button-closed-icon gramel:absolute gramel:transition-all data-[state=closed]:scale-100 data-[state=closed]:rotate-0 data-[state=open]:scale-0 data-[state=open]:rotate-90"
          />
        ),
      },
    },
  },
}

export const Expandable: Story = () => <Chat />
Expandable.parameters = {
  elements: {
    config: {
      modal: {
        expandable: true,
        dimensions: {
          default: { width: '500px', height: '600px', maxHeight: '100vh' },
          expanded: { width: '80vw', height: '90vh' },
        },
      },
    },
  },
}

export const PositionTopRight: Story = () => <Chat />
PositionTopRight.parameters = {
  elements: {
    config: {
      modal: { position: 'top-right' },
    },
  },
}

export const PositionBottomRight: Story = () => <Chat />
PositionBottomRight.parameters = {
  elements: {
    config: {
      modal: { position: 'bottom-right' },
    },
  },
}

export const PositionBottomLeft: Story = () => <Chat />
PositionBottomLeft.parameters = {
  elements: {
    config: {
      modal: { position: 'bottom-left' },
    },
  },
}

export const PositionTopLeft: Story = () => <Chat />
PositionTopLeft.parameters = {
  elements: {
    config: {
      modal: { position: 'top-left' },
    },
  },
}
