import './vite-env.d.ts'
import type { Preview } from '@storybook/react-vite'
import { ElementsDecorator } from './GlobalDecorator'
import React from 'react'
import '../src/global.css'
import type { ElementsConfig } from '../src/types'
import { allModes } from './modes.ts'
import { withThemeByClassName } from '@storybook/addon-themes'
import isChromatic from 'chromatic/isChromatic'

// Production defaults for Chromatic visual testing
const CHROMATIC_PROJECT_SLUG = 'adamtest'
const CHROMATIC_MCP_URL = 'https://chat.speakeasy.com/mcp/speakeasy-team-my_api'

const preview: Preview = {
  parameters: {
    chromatic: {
      delay: 500,
      modes: {
        'light desktop': allModes['light desktop'],
        'dark desktop': allModes['dark desktop'],
      },
    },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
  },
  // Global args for all stories - used by the decorator to configure ElementsProvider
  args: {
    projectSlug: isChromatic()
      ? CHROMATIC_PROJECT_SLUG
      : (import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG ?? ''),
    mcpUrl: isChromatic()
      ? CHROMATIC_MCP_URL
      : (import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL ?? ''),
  },
  argTypes: {
    projectSlug: { control: 'text' },
    mcpUrl: { control: 'text' },
  },
  decorators: [
    withThemeByClassName({
      themes: {
        light: 'light',
        dark: 'dark',
      },
      defaultTheme: 'light',
    }),
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
