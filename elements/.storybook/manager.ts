import { addons } from 'storybook/manager-api'
import { create } from 'storybook/theming/create'

const theme = create({
  base: 'dark',

  // Brand
  brandTitle: 'Gram Elements',
  brandUrl: 'https://getgram.ai',
  brandTarget: '_blank',

  // Colors - dark minimal aesthetic
  colorPrimary: '#ffffff',
  colorSecondary: '#a1a1aa', // zinc-400

  // UI
  appBg: '#09090b', // zinc-950
  appContentBg: '#18181b', // zinc-900
  appBorderColor: '#27272a', // zinc-800
  appBorderRadius: 8,

  // Text
  textColor: '#fafafa', // zinc-50
  textInverseColor: '#09090b',
  textMutedColor: '#71717a', // zinc-500

  // Toolbar
  barTextColor: '#a1a1aa',
  barSelectedColor: '#ffffff',
  barHoverColor: '#ffffff',
  barBg: '#18181b',

  // Form
  inputBg: '#27272a',
  inputBorder: '#3f3f46', // zinc-700
  inputTextColor: '#fafafa',
  inputBorderRadius: 6,

  // Buttons
  buttonBg: '#27272a',
  buttonBorder: '#3f3f46',

  // Fonts
  fontBase: '"Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
  fontCode: '"JetBrains Mono", "Fira Code", monospace',
})

addons.setConfig({
  theme,
  sidebar: {
    showRoots: true,
  },
  toolbar: {
    zoom: { hidden: true },
    backgrounds: { hidden: true },
    outline: { hidden: true },
    measure: { hidden: true },
  },
})
