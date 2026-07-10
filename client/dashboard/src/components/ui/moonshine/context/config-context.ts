import { createContext } from "react";
import { Theme } from "./theme";

export interface ConfigContextType {
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
 * Lives apart from the provider so that `ConfigContext.tsx` exports nothing
 * but components — a file mixing the two breaks React Fast Refresh, which is
 * what `react/only-export-components` guards against.
 */
export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);
