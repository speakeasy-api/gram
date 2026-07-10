import { ConfigContext, ConfigContextType } from "./config-context";

export interface MoonshineConfigProviderProps extends ConfigContextType {
  children: React.ReactNode;
}

/**
 * Configures Speakeasy's design system, Moonshine, within a consuming application.
 *
 * @param {React.ReactNode} children - The components to be wrapped by the MoonshineConfigProvider.
 * @param {Theme} theme - The current theme
 * @param {function(Theme): void} setTheme - Function to update the theme
 * @returns {React.ReactNode} - The components wrapped by the MoonshineConfigProvider.
 */
export function MoonshineConfigProvider({
  children,
  theme,
  setTheme,
}: MoonshineConfigProviderProps): React.JSX.Element {
  return (
    <ConfigContext.Provider value={{ theme, setTheme }}>
      {children}
    </ConfigContext.Provider>
  );
}
