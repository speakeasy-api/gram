import { ThemeContext, type ThemeContextType } from "@/contexts/theme-context";

export interface ThemeProviderProps extends ThemeContextType {
  children: React.ReactNode;
}

/**
 * Provides the current theme (light/dark) and a setter to the app.
 *
 * @param {React.ReactNode} children - The components to be wrapped by the ThemeProvider.
 * @param {Theme} theme - The current theme
 * @param {function(Theme): void} setTheme - Function to update the theme
 * @returns {React.ReactNode} - The components wrapped by the ThemeProvider.
 */
export function ThemeProvider({
  children,
  theme,
  setTheme,
}: ThemeProviderProps): React.JSX.Element {
  return (
    <ThemeContext.Provider value={{ theme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}
