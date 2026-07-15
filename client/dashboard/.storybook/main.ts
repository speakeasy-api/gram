import type { StorybookConfig } from "@storybook/react-vite";

const config: StorybookConfig = {
  stories: ["../src/**/*.mdx", "../src/**/*.stories.@(ts|tsx)"],
  addons: [
    "@storybook/addon-links",
    "@storybook/addon-themes",
    "@storybook/addon-docs",
    "@storybook/addon-mcp",
  ],
  framework: {
    name: "@storybook/react-vite",
    options: {
      builder: {
        // The app's vite.config.ts is dev-server oriented (requires GRAM_*
        // env, HTTPS certs, API proxying, manual chunking). Storybook uses
        // its own minimal vite config instead of inheriting all of that.
        viteConfigPath: ".storybook/vite.config.mts",
      },
    },
  },
  features: {
    sidebarOnboardingChecklist: false,
  },
  staticDirs: ["../public"],
};

export default config;
