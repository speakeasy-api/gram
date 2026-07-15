import type { Decorator, Preview } from "@storybook/react-vite";
import "../src/App.css";
import React from "react";
import { MemoryRouter } from "react-router";
import { withThemeByClassName } from "@storybook/addon-themes";
import { ThemedDocsContainer } from "./themedDocsContainer";

import { ThemeProvider } from "../src/contexts/Theme";
import { TooltipProvider } from "../src/components/ui/tooltip";

// Mirrors the provider stack in App.tsx so components render like they do in
// the app: theme context, the tooltip provider, and a router for anything
// that renders a Link.
const appProvidersDecorator: Decorator = (story, context) => {
  return (
    <ThemeProvider theme={context.globals["theme"]} setTheme={() => {}}>
      <TooltipProvider>
        <MemoryRouter>{story()}</MemoryRouter>
      </TooltipProvider>
    </ThemeProvider>
  );
};

export const decorators: Decorator[] = [
  withThemeByClassName({
    themes: { light: "light", dark: "dark" },
    defaultTheme: "light",
  }),
  appProvidersDecorator,
];

const preview: Preview = {
  parameters: {
    viewport: {
      options: {
        small: { name: "Small", styles: { width: "640px", height: "800px" } },
        large: {
          name: "Large",
          styles: { width: "1024px", height: "1000px" },
        },
      },
    },
    backgrounds: {
      options: {
        light: { name: "light", value: "#fff" },
        dark: { name: "dark", value: "hsl(0, 0%, 7%)" },
      },
    },
    docs: {
      container: ThemedDocsContainer,
    },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
  },
};

export default preview;
