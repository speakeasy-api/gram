import "../src/App.css";

import { withThemeByClassName } from "@storybook/addon-themes";
import type { Decorator, Preview } from "@storybook/react-vite";

import { DesignSystemProviders } from "./providers";

const withDesignSystem: Decorator = (Story, context) => (
  <DesignSystemProviders
    initialTheme={context.globals["theme"] === "dark" ? "dark" : "light"}
  >
    <Story />
  </DesignSystemProviders>
);

const preview: Preview = {
  parameters: {
    controls: { matchers: { color: /(background|color)$/i, date: /Date$/i } },
    options: {
      storySort: {
        order: ["Design System", ["All Components"]],
      },
    },
  },
  decorators: [
    withDesignSystem,
    withThemeByClassName({
      themes: { light: "light", dark: "dark" },
      defaultTheme: "light",
    }),
  ],
};

export default preview;
