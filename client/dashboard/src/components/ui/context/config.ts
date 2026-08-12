import { createContext } from "react";
import { Theme } from "./theme";

export interface ConfigContextType {
  /** The current theme. */
  theme: Theme;
  /** Update the current theme. */
  setTheme: (theme: Theme) => void;
}

export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);
