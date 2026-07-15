import { createContext, useContext } from "react";

export type Theme = "light" | "dark";

export interface ThemeContextType {
  /*
   * The current theme
   */
  theme: Theme;

  /*
   * Update the current theme
   */
  setTheme: (theme: Theme) => void;
}

/**
 * Lives apart from the provider so that `Theme.tsx` exports nothing but
 * components — a file mixing the two breaks React Fast Refresh, which is
 * what `react/only-export-components` guards against.
 */
export const ThemeContext = createContext<ThemeContextType | undefined>(
  undefined,
);

export function useTheme(): ThemeContextType {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return context;
}
