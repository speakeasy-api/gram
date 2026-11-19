import type { ProviderConnection } from "@/pages/mcp/x/setup/useProviderConnection";
import { createContext, useContext } from "react";
import type { State } from "./state";

type SetupContextValue = {
  s: State;
  dispatch: ProviderConnection["dispatch"];
  live: ProviderConnection & {
    settingsHref?: string;
    /** Records the finished setup, then completes it. */
    complete?: () => void;
    completing?: boolean;
    completionError?: string | null;
  };
};
export const SetupContext = createContext<SetupContextValue | null>(null);
export function useSetup(): SetupContextValue {
  const context = useContext(SetupContext);
  if (!context) throw new Error("Provider setup context missing");
  return context;
}
