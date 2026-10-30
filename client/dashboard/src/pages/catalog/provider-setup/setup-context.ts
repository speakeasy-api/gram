import { createContext, useContext, type Dispatch } from "react";
import type { Action, State } from "./state";

type SetupContextValue = {
  s: State;
  dispatch: Dispatch<Action>;
  live?: {
    pending: string | null;
    error: string | null;
    errorStep: "save" | "signin" | "test";
    toolCount: number | null;
    loading: boolean;
    callbackUrl?: string;
    savedClientId?: string;
    recovery?: "signin" | "credentials" | "settings" | "permissions";
    settingsHref?: string;
    retryLoad?: () => void;
    cancelSignIn?: () => void;
  };
};
export const SetupContext = createContext<SetupContextValue | null>(null);
export function useSetup(): SetupContextValue {
  const context = useContext(SetupContext);
  if (!context) throw new Error("Provider setup context missing");
  return context;
}
