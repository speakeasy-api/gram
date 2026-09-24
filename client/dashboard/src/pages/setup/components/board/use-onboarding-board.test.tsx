import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
} from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { SetupWorkstream } from "@gram/client/models/components/setupworkstream.js";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { ONBOARDING_WORKSTREAMS } from "./workstream-fixtures";
import { useOnboardingBoard } from "./use-onboarding-board";

const state = vi.hoisted(() => ({
  org: "org-a",
  admin: true,
  staff: false,
  failRead: false,
  tasks: [] as SetupTask[],
  workstreams: [] as SetupWorkstream[],
  write: vi.fn(),
  assign: vi.fn(),
}));
vi.mock("@gram/client/react-query/assignSetupWorkstream.js", () => ({
  useAssignSetupWorkstreamMutation: () => ({ mutateAsync: state.assign }),
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
      return { tasks: state.tasks, workstreams: state.workstreams };
    },
  }),
}));
vi.mock("@gram/client/react-query/updateSetupTask.js", () => ({
  useUpdateSetupTaskMutation: () => useMutation({ mutationFn: state.write }),
}));
beforeEach(() => {
  state.assign.mockReset().mockResolvedValue(undefined);
  state.org = "org-a";
  state.admin = true;
  state.staff = false;
  state.failRead = false;
  state.workstreams = ONBOARDING_WORKSTREAMS;
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
  expect(result.current.workstreams).toEqual(ONBOARDING_WORKSTREAMS);
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
  expect(result.current.writeErrorTaskId).toBe("instrument-agents");
  expect(result.current.tasks[0]!.status).toBe("todo");
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      true,
    );
  });
  expect(result.current.writeError).toBeNull();
  expect(result.current.writeErrorTaskId).toBeNull();
});
it("refreshes a task committed before a network error and preserves the write error", async () => {
  const { result, client } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  const invalidate = vi.spyOn(client, "invalidateQueries");
  state.write.mockImplementationOnce(() => {
    state.tasks = state.tasks.map((task) => ({ ...task, status: "done" }));
    return Promise.reject(new Error("Response lost"));
  });
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      false,
    );
  });
  expect(invalidate).toHaveBeenCalled();
  await waitFor(() => expect(result.current.tasks[0]!.status).toBe("done"));
  expect(result.current.writeError).toBe("Response lost");
  expect(result.current.isPending).toBe(false);
});

it("keeps the original write error when its recovery refresh also fails", async () => {
  const { result, client } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  state.write.mockRejectedValueOnce(new Error("Response lost"));
  vi.spyOn(client, "invalidateQueries").mockRejectedValueOnce(
    new Error("Read failed"),
  );
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      false,
    );
  });
  expect(result.current.writeError).toBe("Response lost");
  expect(result.current.isPending).toBe(false);
});

it("stays pending until the post-write refresh settles", async () => {
  const { result, client } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  const refresh = Promise.withResolvers<void>();
  vi.spyOn(client, "invalidateQueries").mockReturnValue(refresh.promise);
  let saving: Promise<boolean>;
  await act(async () => {
    saving = result.current.setStatus("instrument-agents", "done");
  });
  await waitFor(() => expect(state.write).toHaveBeenCalledOnce());
  expect(result.current.isPending).toBe(true);
  await act(async () => {
    expect(await result.current.setStatus("instrument-agents", "todo")).toBe(
      false,
    );
    refresh.resolve();
    expect(await saving).toBe(true);
  });
  expect(result.current.isPending).toBe(false);
  expect(state.write).toHaveBeenCalledOnce();
});
it("restricts hidden-task access to authenticated staff", async () => {
  state.admin = true;
  const { result, rerender } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  expect(result.current.canHideTasks).toBe(false);
  state.staff = true;
  rerender();
  await waitFor(() => expect(result.current.isLoading).toBe(false));
  expect(result.current.canHideTasks).toBe(true);
  await act(async () => {
    expect(await result.current.setHidden("instrument-agents", true)).toBe(
      true,
    );
  });
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
    expect(
      await result.current.assignWorkstream(
        ONBOARDING_WORKSTREAMS[1]!,
        undefined,
      ),
    ).toBe(false);
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

it("assigns every workstream task, including tasks omitted from the read, with one pending lock and refresh", async () => {
  const { result, client } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  const write = Promise.withResolvers<void>();
  const refresh = Promise.withResolvers<void>();
  state.assign.mockImplementation(() => write.promise);
  const invalidate = vi
    .spyOn(client, "invalidateQueries")
    .mockReturnValue(refresh.promise);
  let saving: Promise<boolean>;
  await act(async () => {
    saving = result.current.assignWorkstream(ONBOARDING_WORKSTREAMS[1]!, {
      kind: "email",
      email: "owner@example.test",
    });
  });
  expect(state.assign).toHaveBeenCalledExactlyOnceWith({
    request: {
      assignSetupWorkstreamRequestBody: {
        workstream: "observe",
        assignee: { email: "owner@example.test" },
      },
    },
  });
  expect(result.current.isPending).toBe(true);
  await act(async () => {
    expect(
      await result.current.assignWorkstream(
        ONBOARDING_WORKSTREAMS[0]!,
        undefined,
      ),
    ).toBe(false);
    expect(await result.current.setStatus("instrument-agents", "done")).toBe(
      false,
    );
    write.resolve();
  });
  expect(invalidate).toHaveBeenCalledOnce();
  expect(result.current.isPending).toBe(true);
  await act(async () => {
    refresh.resolve();
    expect(await saving).toBe(true);
  });
  expect(result.current.isPending).toBe(false);
});
it("refreshes failed atomic assignments and permits unassign retry", async () => {
  const { result, client } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  state.assign
    .mockResolvedValue(undefined)
    .mockRejectedValueOnce(new Error("Failed"));
  const invalidate = vi.spyOn(client, "invalidateQueries");
  await act(async () => {
    expect(
      await result.current.assignWorkstream(
        ONBOARDING_WORKSTREAMS[0]!,
        undefined,
      ),
    ).toBe(false);
  });
  expect(result.current.writeError).toBe("Failed");
  expect(result.current.writeErrorTaskId).toBeNull();
  expect(invalidate).toHaveBeenCalledOnce();
  await act(async () => {
    expect(
      await result.current.assignWorkstream(
        ONBOARDING_WORKSTREAMS[0]!,
        undefined,
      ),
    ).toBe(true);
  });
  expect(state.assign).toHaveBeenCalledTimes(2);
  expect(
    state.assign.mock.calls.every(
      ([arg]) =>
        arg.request.assignSetupWorkstreamRequestBody.clearAssignee === true,
    ),
  ).toBe(true);
  expect(result.current.writeError).toBeNull();
});

it("assigns one member to every gateway task including the optional task", async () => {
  const { result } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  const workstream = ONBOARDING_WORKSTREAMS.find(
    (stream) => stream.id === "distribute",
  )!;
  await act(async () => {
    expect(
      await result.current.assignWorkstream(workstream, {
        kind: "user",
        userId: "member",
        name: "Team member",
        email: "member@example.test",
      }),
    ).toBe(true);
  });
  expect(state.assign).toHaveBeenCalledExactlyOnceWith({
    request: {
      assignSetupWorkstreamRequestBody: {
        workstream: "distribute",
        assignee: { userId: "member" },
      },
    },
  });
});

it("reports unsupported backend task keys without failing known tasks", async () => {
  state.tasks.push({ ...state.tasks[0]!, key: "future-task" });
  const { result } = setup();
  await waitFor(() =>
    expect(result.current.unsupportedTaskKeys).toEqual(["future-task"]),
  );
  expect(result.current.error).toBeUndefined();
  expect(result.current.tasks).toHaveLength(1);
});

it("reports refresh failure but returns saved assignment so invitations need no reassignment", async () => {
  const { result, client } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  vi.spyOn(client, "invalidateQueries").mockRejectedValueOnce(
    new Error("Offline"),
  );
  await act(async () => {
    expect(
      await result.current.assignWorkstream(
        ONBOARDING_WORKSTREAMS[0]!,
        undefined,
      ),
    ).toBe(true);
  });
  expect(result.current.writeError).toContain(
    "Assignment saved. Could not refresh",
  );
  expect(result.current.isPending).toBe(false);
});

it("refreshes server catalog after assigning a newly introduced workstream", async () => {
  state.workstreams = [
    {
      id: "new-stream",
      title: "Server title",
      taskKeys: ["future-task", "instrument-agents"],
    },
  ];
  state.tasks.push({ ...state.tasks[0]!, key: "future-task", hidden: true });
  const { result } = setup();
  await waitFor(() => expect(result.current.workstreams).toHaveLength(1));
  expect(result.current.workstreams[0]).toMatchObject({
    ...state.workstreams[0],
    suggestedOwner: "Assign workstream",
  });
  state.assign.mockImplementationOnce(async () => {
    state.workstreams = [
      {
        id: "new-stream",
        title: "Updated title",
        taskKeys: ["instrument-agents", "future-task"],
      },
    ];
  });
  await act(async () => {
    expect(
      await result.current.assignWorkstream(
        result.current.workstreams[0]!,
        undefined,
      ),
    ).toBe(true);
  });
  expect(state.assign).toHaveBeenCalledExactlyOnceWith({
    request: {
      assignSetupWorkstreamRequestBody: {
        workstream: "new-stream",
        clearAssignee: true,
      },
    },
  });
  await waitFor(() =>
    expect(result.current.workstreams[0]?.title).toBe("Updated title"),
  );
  expect(result.current.workstreams[0]?.taskKeys).toEqual([
    "instrument-agents",
    "future-task",
  ]);
});
