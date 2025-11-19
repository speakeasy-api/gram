import type { Preview } from "@storybook/react-vite";
import { GlobalDecorator } from "./GlobalDecorator";
import React from "react";
import "../src/global.css";

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
    (Story) => (
      <GlobalDecorator>
        <Story />
      </GlobalDecorator>
    ),
  ],
};

export default preview;
