import { useEffect } from "react";
import { PREFERRED_THEME_STORAGE_KEY } from "@/lib/local-storage-keys";
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
  // Re-derive the theme when another tab/document writes the storage key —
  // without this, this document's <html> class (and so the whole UI) stays on
  // the old theme while storage says otherwise. Lives at the provider level
  // so every route stays in sync, not just those mounting a ThemeSwitcher.
  // Routing through setTheme keeps the class and React state in one code path.
  useEffect(() => {
    const onStorage = (event: StorageEvent): void => {
      if (event.key !== PREFERRED_THEME_STORAGE_KEY) return;
      setTheme(event.newValue === "dark" ? "dark" : "light");
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, [setTheme]);

  return (
    <ConfigContext.Provider value={{ theme, setTheme }}>
      {children}
    </ConfigContext.Provider>
  );
}
