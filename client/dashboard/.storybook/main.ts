import type { StorybookConfig } from "@storybook/react-vite";

const config: StorybookConfig = {
  stories: [
    "../src/components/ui/**/*.mdx",
    "../src/components/ui/**/*.stories.@(ts|tsx)",
    "../src/components/page-templates/**/*.mdx",
    "../src/components/page-templates/**/*.stories.@(ts|tsx)",
  ],
  addons: ["@storybook/addon-docs", "@storybook/addon-themes"],
  framework: {
    name: "@storybook/react-vite",
    options: {
      builder: { viteConfigPath: ".storybook/vite.config.ts" },
    },
  },
  typescript: {
    // The typescript-powered docgen plugin does not work against the
    // TypeScript version this repo pins; the babel-based one does.
    reactDocgen: "react-docgen",
  },
};

export default config;
