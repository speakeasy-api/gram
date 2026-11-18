import { useContext } from "react";
import type { ConfigContextType } from "@/components/ui/context/config";
import { ConfigContext } from "@/components/ui/context/config";

export function useConfig(): ConfigContextType {
  const context = useContext(ConfigContext);
  if (!context) {
    throw new Error("useConfig must be used within a ConfigProvider");
  }
  return context;
}
