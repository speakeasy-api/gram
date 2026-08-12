import "../src/App.css";

import { withThemeByClassName } from "@storybook/addon-themes";
import type { Decorator, Preview } from "@storybook/react-vite";

import { DesignSystemProviders } from "./providers";

const withDesignSystem: Decorator = (Story, context) => {
  const theme = context.globals["theme"] === "dark" ? "dark" : "light";

  // Keyed so switching the toolbar theme remounts the provider; without it the
  // context keeps its initial value and anything reading `useConfig().theme`
  // (CodeSnippet, ThemeSwitcher) goes stale.
  return (
    <DesignSystemProviders key={theme} initialTheme={theme}>
      <Story />
    </DesignSystemProviders>
  );
};

const preview: Preview = {
  parameters: {
    controls: { matchers: { color: /(background|color)$/i, date: /Date$/i } },
    options: {
      storySort: {
        // Without an explicit method, anything not named in `order` keeps its
        // import order, which is effectively random.
        method: "alphabetical",
        // `*` is the slot the unnamed entries sort into, so the gallery stays
        // pinned above the individual components.
        order: ["Design System", ["All Components", "Page Templates", "*"]],
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
