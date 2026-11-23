import { vi } from "vitest";
import type { Dispatch } from "react";
import type { Action } from "./state";
import type { ProviderConnection } from "@/pages/mcp/x/setup/useProviderConnection";

export function connectionFixture(
  dispatch: Dispatch<Action>,
  overrides: Partial<ProviderConnection> = {},
): ProviderConnection {
  return {
    dispatch,
    next: vi.fn<() => void>(),
    changeField: ({ field, value }) =>
      dispatch({ type: "field", field, value }),
    signIn: vi.fn<() => void>(),
    testConnection: vi.fn<() => void>(),
    cancelSignIn: vi.fn<() => void>(),
    retryLoad: vi.fn<() => void>(),
    pending: null,
    error: null,
    errorStep: "save",
    toolCount: null,
    loading: false,
    ...overrides,
  };
}
