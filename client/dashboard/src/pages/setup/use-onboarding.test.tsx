import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
} from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import type { SetupWorkstream } from "@gram/client/models/components/setupworkstream.js";

// Representative API catalog for tests; production membership comes from the server.
const SETUP_WORKSTREAMS: SetupWorkstream[] = [
  {
    id: "connect",
    title: "Connect identity",
    taskKeys: [
      "domain-verification",
      "connect-idp",
      "directory-sync",
      "identity-provider",
    ],
  },
  {
    id: "observe",
    title: "Observe agents",
    taskKeys: [
      "enable-logging",
      "anthropic-observability",
      "instrument-agents",
      "litellm",
      "additional-agent-config",
      "confirm-traffic",
    ],
  },
  {
    id: "distribute",
    title: "MCP Gateway",
    taskKeys: ["create-marketplace", "distribute-servers", "platform-mcp"],
  },
  {
    id: "secure",
    title: "Secure agent traffic",
    taskKeys: ["anthropic-admin-controls", "configure-policies"],
  },
];

import { useOnboarding, useOnboardingActions } from "./use-onboarding";

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
  useAssignSetupWorkstreamMutation: () => ({
    mutateAsync: state.assign,
    isPending: false,
  }),
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
  state.workstreams = SETUP_WORKSTREAMS;
  state.tasks = [
    {
      key: "instrument-agents",
      title: "Server task",
      description: "Server description",
      status: "todo",
      hidden: false,
      completedByFact: false,
      countsTowardProgress: true,
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
  const hook = renderHook(
    () => {
      const onboarding = useOnboarding();
      return {
        ...onboarding,
        ...useOnboardingActions(onboarding),
        tasks: onboarding.model.tasks,
        workstreams: onboarding.model.workstreams,
      };
    },
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
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
  expect(result.current.workstreams.map((w) => w.id)).toEqual(
    SETUP_WORKSTREAMS.map((w) => w.id),
  );
  await act(async () => {
    expect(
      await result.current.setStatus("instrument-agents", "done"),
    ).toMatchObject({ status: "saved" });
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
    expect(await result.current.setStatus("instrument-agents", "done")).toEqual(
      {
        status: "failed",
        message: "Write failed",
      },
    );
  });
  expect(result.current.tasks[0]!.status).toBe("todo");
  await act(async () => {
    expect(
      await result.current.setStatus("instrument-agents", "done"),
    ).toMatchObject({ status: "saved" });
  });
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
    expect(await result.current.setStatus("instrument-agents", "done")).toEqual(
      {
        status: "failed",
        message: "Response lost",
      },
    );
  });
  expect(invalidate).toHaveBeenCalled();
  await waitFor(() => expect(result.current.tasks[0]!.status).toBe("done"));
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
    expect(await result.current.setStatus("instrument-agents", "done")).toEqual(
      {
        status: "failed",
        message: "Response lost",
      },
    );
  });
  expect(result.current.isPending).toBe(false);
});

it("stays pending until the post-write refresh settles", async () => {
  const { result, client } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  const refresh = Promise.withResolvers<void>();
  vi.spyOn(client, "invalidateQueries").mockReturnValue(refresh.promise);
  let saving: Promise<unknown>;
  await act(async () => {
    saving = result.current.setStatus("instrument-agents", "done");
  });
  await waitFor(() => expect(state.write).toHaveBeenCalledOnce());
  expect(result.current.isPending).toBe(true);
  await act(async () => {
    expect(
      await result.current.setStatus("instrument-agents", "todo"),
    ).not.toMatchObject({ status: "saved" });
    refresh.resolve();
    expect(await saving).toEqual({ status: "saved" });
  });
  expect(result.current.isPending).toBe(false);
  expect(state.write).toHaveBeenCalledOnce();
});
it("restricts hidden-task access to authenticated staff", async () => {
  state.admin = true;
  const { result, rerender } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  expect(result.current.canInspectHidden).toBe(false);
  state.staff = true;
  rerender();
  await waitFor(() => expect(result.current.isLoading).toBe(false));
  expect(result.current.canInspectHidden).toBe(true);
  await act(async () => {
    expect(
      await result.current.setHidden("instrument-agents", true),
    ).toMatchObject({ status: "saved" });
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
        SETUP_WORKSTREAMS[1]!.id,
        undefined,
      ),
    ).toMatchObject({ status: "rejected" });
    expect(
      await result.current.setHidden("instrument-agents", true),
    ).not.toMatchObject({ status: "saved" });
    expect(
      await result.current.setStatus("instrument-agents", "done"),
    ).toMatchObject({ status: "saved" });
  });
  expect(state.write).toHaveBeenCalledTimes(1);
});
it("denies unassigned readers, fact completion and blocked transitions", async () => {
  state.admin = false;
  const { result, rerender } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  await act(async () => {
    expect(
      await result.current.setStatus("instrument-agents", "done"),
    ).not.toMatchObject({ status: "saved" });
  });
  state.admin = true;
  state.tasks = [{ ...state.tasks[0]!, completedByFact: true }];
  await act(async () => {
    await result.current.retry();
  });
  rerender();
  expect(
    result.current.canSetStatus(result.current.tasks[0]!),
  ).not.toMatchObject({ status: "saved" });
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
    expect(
      await result.current.setStatus("instrument-agents", "done"),
    ).not.toMatchObject({ status: "saved" });
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
  let saving: Promise<unknown>;
  await act(async () => {
    saving = result.current.assignWorkstream(SETUP_WORKSTREAMS[1]!.id, {
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
        SETUP_WORKSTREAMS[0]!.id,
        undefined,
      ),
    ).toMatchObject({ status: "rejected" });
    expect(
      await result.current.setStatus("instrument-agents", "done"),
    ).not.toMatchObject({ status: "saved" });
    write.resolve();
  });
  expect(invalidate).toHaveBeenCalledOnce();
  expect(result.current.isPending).toBe(true);
  await act(async () => {
    refresh.resolve();
    expect(await saving).toEqual({ status: "saved" });
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
        SETUP_WORKSTREAMS[0]!.id,
        undefined,
      ),
    ).toEqual({ status: "failed", message: "Failed" });
  });
  expect(invalidate).toHaveBeenCalledOnce();
  await act(async () => {
    expect(
      await result.current.assignWorkstream(
        SETUP_WORKSTREAMS[0]!.id,
        undefined,
      ),
    ).toEqual({ status: "saved" });
  });
  expect(state.assign).toHaveBeenCalledTimes(2);
  expect(
    state.assign.mock.calls.every(
      ([arg]) =>
        arg.request.assignSetupWorkstreamRequestBody.clearAssignee === true,
    ),
  ).toBe(true);
});

it("assigns one member to every gateway task including the optional task", async () => {
  const { result } = setup();
  await waitFor(() => expect(result.current.tasks).toHaveLength(1));
  const workstream = SETUP_WORKSTREAMS.find(
    (stream) => stream.id === "distribute",
  )!.id;
  await act(async () => {
    expect(
      await result.current.assignWorkstream(workstream, {
        kind: "user",
        userId: "member",
        name: "Team member",
        email: "member@example.test",
      }),
    ).toEqual({ status: "saved" });
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
    expect(result.current.model.unsupportedTaskKeys).toEqual(["future-task"]),
  );
  expect(result.current.error).toBeUndefined();
  expect(result.current.tasks.map((task) => task.id)).toEqual([
    "instrument-agents",
    "future-task",
  ]);
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
        SETUP_WORKSTREAMS[0]!.id,
        undefined,
      ),
    ).toEqual({
      status: "saved_stale",
      message: expect.stringContaining("Could not refresh"),
    });
  });
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
    id: "new-stream",
    title: "Server title",
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
        result.current.workstreams[0]!.id,
        undefined,
      ),
    ).toEqual({ status: "saved" });
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
  expect(result.current.workstreams[0]?.tasks.map((task) => task.id)).toEqual([
    "instrument-agents",
    "future-task",
  ]);
});
