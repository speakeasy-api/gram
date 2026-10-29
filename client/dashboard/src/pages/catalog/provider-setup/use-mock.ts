import { createContext, useContext, type Dispatch } from "react";
import type { Action, State } from "./state";

type MockContextValue = {
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
    recovery?: "signin" | "credentials" | "settings";
    settingsHref?: string;
    retryLoad?: () => void;
    cancelSignIn?: () => void;
  };
};
export const MockContext = createContext<MockContextValue | null>(null);
export function useMock(): MockContextValue {
  const context = useContext(MockContext);
  if (!context) throw new Error("Provider mock context missing");
  return context;
}
