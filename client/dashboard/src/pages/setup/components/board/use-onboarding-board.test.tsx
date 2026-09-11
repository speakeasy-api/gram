import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
} from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { useOnboardingBoard } from "./use-onboarding-board";

const state = vi.hoisted(() => ({
  org: "org-a",
  admin: true,
  staff: false,
  failRead: false,
  tasks: [] as SetupTask[],
  write: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({
    organization: { id: state.org },
    user: { id: "user-a", email: "owner@example.test", isAdmin: state.staff },
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => state.admin }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/listSetupTasks.js", () => ({
  buildListSetupTasksQuery: (_client: unknown, request: unknown) => ({
    queryKey: ["@gram/client", "organizations", "listSetupTasks", request],
    queryFn: async () => {
      if (state.failRead) throw new Error("Read failed");
      return { tasks: state.tasks };
    },
  }),
}));
vi.mock("@gram/client/react-query/updateSetupTask.js", () => ({
  useUpdateSetupTaskMutation: () => useMutation({ mutationFn: state.write }),
}));
beforeEach(() => {
  state.org = "org-a";
  state.admin = true;
  state.staff = false;
  state.failRead = false;
  state.tasks = [
    {
      key: "instrument-agents",
      title: "Server task",
      description: "Server description",
      status: "todo",
      hidden: false,
      completedByFact: false,
      blockedBy: [],
    },
  ];
  state.write.mockReset().mockImplementation(async ({ request }) => {
    state.tasks = state.tasks.map((task) => ({
      ...task,
      ...request.updateSetupTaskRequestBody,
    }));
  });
});
afterEach(cleanup);
function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const hook = renderHook(() => useOnboardingBoard(), {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  return { ...hook, client };
}

it("reads server tasks and refreshes this organization's queries after a write", async () => {
  const { result, client } = setup();
  const otherKey = [
    "@gram/client",
    "organizations",
    "listSetupTasks",
    {},
    { organizationId: "org-b" },
  ];
  client.setQueryData(otherKey, { tasks: [] });
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      true,
    );
  });
  await waitFor(() => expect(result.current.tasks[0]!.status).toBe("done"));
  expect(state.write).toHaveBeenCalledWith(
    {
      request: {
        updateSetupTaskRequestBody: {
          taskKey: "instrument-agents",
          status: "done",
        },
      },
    },
    expect.anything(),
  );
  expect(client.getQueryState(otherKey)?.isInvalidated).toBe(false);
});
it("retains server state on failed writes and permits retry", async () => {
  const { result } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  state.write.mockRejectedValueOnce(new Error("Write failed"));
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      false,
    );
  });
  expect(result.current.writeError).toBe("Write failed");
  expect(result.current.tasks[0]!.status).toBe("todo");
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      true,
    );
  });
  expect(result.current.writeError).toBeNull();
});
it("retries failed reads", async () => {
  state.failRead = true;
  const { result } = setup();
  await waitFor(() => expect(result.current.error).toBe("Read failed"));
  state.failRead = false;
  await act(async () => {
    await result.current.retry();
  });
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
});
it("allows assigned readers to change status but not assignment or visibility", async () => {
  state.admin = false;
  state.tasks[0]!.assignee = { userId: "user-a", email: "owner@example.test" };
  const { result } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  await act(async () => {
    expect(await result.current.assign("instrument-agents", undefined)).toBe(
      false,
    );
    expect(await result.current.setHidden("instrument-agents", true)).toBe(
      false,
    );
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      true,
    );
  });
  expect(state.write).toHaveBeenCalledTimes(1);
});
it("denies unassigned readers, fact completion and blocked transitions", async () => {
  state.admin = false;
  const { result, rerender } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      false,
    );
  });
  state.admin = true;
  state.tasks = [{ ...state.tasks[0]!, completedByFact: true }];
  await act(async () => {
    await result.current.retry();
  });
  rerender();
  expect(result.current.canSetStatus(result.current.tasks[0]!)).toBe(false);
  state.tasks = [
    { ...state.tasks[0]!, completedByFact: false, blockedBy: ["connect-idp"] },
  ];
  await act(async () => {
    await result.current.retry();
  });
  await waitFor(() =>
    expect(result.current.tasks[0]!.blockedBy).toHaveLength(1),
  );
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      false,
    );
  });
  expect(state.write).not.toHaveBeenCalled();
});
