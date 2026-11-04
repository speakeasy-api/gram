import { renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  invalidate: vi.fn(),
  onError: undefined as undefined | ((error: Error) => Promise<void>),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "test-session" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-one",
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/builtinMcpServers.js", () => ({
  invalidateAllBuiltinMcpServers: state.invalidate,
}));
vi.mock("@gram/client/react-query/updateBuiltinMcpServer.js", () => ({
  useUpdateBuiltinMcpServerMutation: (options: {
    onError: (error: Error) => Promise<void>;
  }) => {
    state.onError = options.onError;
    return { mutate: vi.fn() };
  },
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { useUpdateBuiltinMcpServer } from "./useUpdateBuiltinMcpServer";

it("waits for the refreshed version before completing a failed save", async () => {
  const refresh = Promise.withResolvers<void>();
  state.invalidate.mockReturnValueOnce(refresh.promise);
  renderHook(() => useUpdateBuiltinMcpServer());
  let completed = false;
  const failedSave = state.onError!(new Error("Version conflict")).then(() => {
    completed = true;
  });
  await Promise.resolve();
  expect(state.invalidate).toHaveBeenCalledOnce();
  expect(completed).toBe(false);
  refresh.resolve();
  await failedSave;
  expect(completed).toBe(true);
});
