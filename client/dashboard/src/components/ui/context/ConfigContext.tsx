import { ConfigContext, ConfigContextType } from "./config";

export interface ConfigProviderProps extends ConfigContextType {
  children: React.ReactNode;
}

/**
 * Provides the design system with the current theme and a way to change it.
 */
export function ConfigProvider({
  children,
  theme,
  setTheme,
}: ConfigProviderProps): React.JSX.Element {
  return (
    <ConfigContext.Provider value={{ theme, setTheme }}>
      {children}
    </ConfigContext.Provider>
  );
}
