import './vite-env.d.ts'
import type { Preview } from '@storybook/react-vite'
import { ElementsDecorator } from './GlobalDecorator'
import React from 'react'
import '../src/global.css'
import type { ElementsConfig } from '../src/types'

const preview: Preview = {
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
  },
  // Global args for all stories - used by the decorator to configure ElementsProvider
  args: {
    projectSlug:
      import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG ?? '',
    mcpUrl: import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL ?? '',
  },
  argTypes: {
    projectSlug: { control: 'text' },
    mcpUrl: { control: 'text' },
  },
  decorators: [
    (Story, context) => {
      // Stories can override config via parameters.elements
      const elementsParams = context.parameters.elements ?? {}
      const elementsConfig: Partial<ElementsConfig> =
        elementsParams.config ?? {}

      // Storbook users control these args using the controls panel
      const projectSlugArg = context.args.projectSlug
      const mcpUrlArg = context.args.mcpUrl

      elementsConfig.projectSlug ||= projectSlugArg
      elementsConfig.mcp ||= mcpUrlArg

      return (
        <ElementsDecorator config={elementsConfig}>
          <Story />
        </ElementsDecorator>
      )
    },
  ],
}

export default preview
