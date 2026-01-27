import './vite-env.d.ts'
import type { Preview } from '@storybook/react-vite'
import { ElementsDecorator } from './GlobalDecorator'
import React from 'react'
import '../src/global.css'
import type { ElementsConfig } from '../src/types'
import { allModes } from './modes.ts'
import { withThemeByClassName } from '@storybook/addon-themes'
import { initialize, mswLoader } from 'msw-storybook-addon'
import { handlers } from './mocks/handlers'

// Production defaults for Chromatic visual testing (set via STORYBOOK_CHROMATIC=true in CI)
const IS_CHROMATIC = import.meta.env.STORYBOOK_CHROMATIC === 'true'
const CHROMATIC_PROJECT_SLUG = 'adamtest'
const CHROMATIC_MCP_URL = 'https://chat.speakeasy.com/mcp/speakeasy-team-my_api'

// Only initialize MSW for Chromatic - local dev uses real backend
if (IS_CHROMATIC) {
  initialize()
}

const preview: Preview = {
  globalTypes: {
    theme: {
      description: 'Color theme for components',
      toolbar: {
        title: 'Theme',
        icon: 'paintbrush',
        items: [
          { value: 'light', title: 'Light', icon: 'sun' },
          { value: 'dark', title: 'Dark', icon: 'moon' },
        ],
        dynamicTitle: true,
      },
    },
  },
  initialGlobals: {
    theme: 'dark',
  },
  parameters: {
    layout: 'fullscreen',
    chromatic: {
      delay: 500,
      modes: {
        'light desktop': allModes['light desktop'],
        'dark desktop': allModes['dark desktop'],
      },
    },
    // Only enable MSW handlers for Chromatic
    ...(IS_CHROMATIC && { msw: { handlers } }),
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
  },
  // Only use MSW loader for Chromatic
  ...(IS_CHROMATIC && { loaders: [mswLoader] }),
  // Global args for all stories - used by the decorator to configure ElementsProvider
  args: {
    projectSlug: IS_CHROMATIC
      ? CHROMATIC_PROJECT_SLUG
      : (import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG ?? ''),
    mcpUrl: IS_CHROMATIC
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
      const baseConfig: Partial<ElementsConfig> = elementsParams.config ?? {}

      // Storybook users control these args using the controls panel
      const projectSlugArg = context.args.projectSlug
      const mcpUrlArg = context.args.mcpUrl

      // Storybook theme from toolbar
      const storybookTheme = context.globals.theme as 'light' | 'dark' | undefined

      // Create new config object (immutable) to ensure React detects changes
      const elementsConfig: Partial<ElementsConfig> = {
        ...baseConfig,
        projectSlug: baseConfig.projectSlug || projectSlugArg,
        mcp: baseConfig.mcp || mcpUrlArg,
        theme: {
          ...baseConfig.theme,
          ...(storybookTheme && { colorScheme: storybookTheme }),
        },
      }

      return (
        <ElementsDecorator config={elementsConfig} key={storybookTheme}>
          <Story />
        </ElementsDecorator>
      )
    },
  ],
}

export default preview
