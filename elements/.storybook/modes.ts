/**
 * Chromatic modes for the Elements library.
 * Used to test the library in different modes and viewports.
 */
export const allModes = {
  desktop: {
    viewport: 'large',
  },
  dark: {
    backgrounds: { value: 'hsl(0, 0%, 7%)' },
    theme: 'dark',
  },
  light: {
    backgrounds: { value: '#fff' },
    theme: 'light',
  },
  'dark desktop': {
    backgrounds: { value: 'hsl(0, 0%, 7%)' },
    theme: 'dark',
    viewport: 'large',
  },
  'light desktop': {
    backgrounds: { value: '#fff' },
    theme: 'light',
    viewport: 'large',
  },
}
