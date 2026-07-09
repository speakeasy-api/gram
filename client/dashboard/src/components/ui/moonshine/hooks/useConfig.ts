import { useContext } from "react";
import { ConfigContext, ConfigContextType } from "../context/ConfigContext";

export function useConfig(): ConfigContextType {
  const context = useContext(ConfigContext);
  if (!context) {
    throw new Error("useConfig must be used within a MoonshineConfigProvider");
  }
  return context;
}
