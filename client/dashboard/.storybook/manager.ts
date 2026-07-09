import { addons } from "storybook/manager-api";
import { create } from "storybook/theming";

export const gramTheme = create({
  base: "light",

  colorPrimary: "hsl(53, 96%, 40%)",
  colorSecondary: "hsl(53, 96%, 40%)",

  appBg: "hsl(0, 0%, 100%)",
  appContentBg: "hsl(0, 0%, 100%)",
  appBorderColor: "hsl(0, 0%, 20%)",
  appBorderRadius: 4,

  textColor: "hsl(0, 0%, 10%)",
  textInverseColor: "hsl(0, 0%, 100%)",

  barTextColor: "hsl(0, 0%, 10%)",
  barSelectedColor: "hsl(0, 0%, 10%)",
  barBg: "hsl(0, 0%, 100%)",

  inputBg: "hsl(0, 0%, 100%)",
  inputBorder: "hsl(0, 0%, 20%)",
  inputTextColor: "hsl(0, 0%, 10%)",
  inputBorderRadius: 4,

  brandTitle: "Gram Design System",
  brandUrl: "https://getgram.ai",
});

addons.setConfig({
  theme: gramTheme,
});
