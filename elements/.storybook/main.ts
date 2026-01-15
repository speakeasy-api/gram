import type { StorybookConfig } from '@storybook/react-vite'

const config: StorybookConfig = {
  stories: ['../src/**/*.mdx', '../src/**/*.stories.@(js|jsx|mjs|ts|tsx)'],
  addons: ['@storybook/addon-docs', '@storybook/addon-themes'],
  viteFinal: (config) => {
    config.server = {
      ...config.server,
      allowedHosts: ['localhost', '127.0.0.1', 'devbox'],
    }
    return config
  },
  framework: {
    name: '@storybook/react-vite',
    options: {
      builder: {
        viteConfigPath: './.storybook/vite.config.mts',
      },
    },
  },
}
export default config
