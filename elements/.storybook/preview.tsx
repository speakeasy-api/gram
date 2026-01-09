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
  decorators: [
    (Story, context) => {
      // Stories can override config via parameters.elements
      const elementsParams = context.parameters.elements ?? {}
      const elementsConfig: Partial<ElementsConfig> =
        elementsParams.config ?? {}

      const projectParam = elementsConfig.projectSlug
      const mcpParam = elementsConfig.mcp
      const projectArg = context.args.projectSlug
      const mcpArg = context.args.mcpUrl
      if (!projectParam && projectArg) {
        elementsConfig.projectSlug = projectArg
      }
      if (!mcpParam && mcpArg) {
        elementsConfig.mcp = mcpArg
      }

      return (
        <ElementsDecorator config={elementsConfig}>
          <Story />
        </ElementsDecorator>
      )
    },
  ],
}

export default preview
