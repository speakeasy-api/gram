import type { ReactNode } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import SetupBoard from "./SetupBoard";

const mocks = vi.hoisted(() => ({
  platformAdmin: false,
  canAdmin: true,
  setupQuery: vi.fn(),
  update: vi.fn(),
  updatePending: false,
  invite: vi.fn(),
  invalidate: vi.fn(),
  toastInfo: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  showPylonChat: vi.fn(),
}));

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }
  function Part({ children }: { children?: ReactNode }) {
    return <div>{children}</div>;
  }
  const Header = Object.assign(Part, { Breadcrumbs: Part });
  const Section = Object.assign(Part, {
    Title: Part,
    Description: Part,
    Body: Part,
    CTA: Part,
  });
  const Toolbar = Object.assign(Part, {
    Leading: Part,
    Actions: Part,
  });
  return {
    Page: Object.assign(Page, { Header, Body: Part, Section, Toolbar }),
  };
});

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("./components/setup-shell", () => ({
  SetupShell: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/ui/MoreActions", () => ({
  MoreActions: ({
    actions,
  }: {
    actions: { label: string; onClick: () => void; disabled?: boolean }[];
  }) => (
    <>
      {actions.map((action) => (
        <button
          key={action.label}
          onClick={action.onClick}
          disabled={action.disabled}
        >
          {action.label}
        </button>
      ))}
    </>
  ),
}));

vi.mock("@/hooks/useOrganizationSetupTasks", () => ({
  useOrganizationSetupTasks: (...args: unknown[]) => mocks.setupQuery(...args),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
  useSession: () => ({
    user: { id: "user-current", email: "current@example.com" },
  }),
  useIsPlatformAdmin: () => mocks.platformAdmin,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.canAdmin }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useQueryClient: () => ({}) };
});

vi.mock("@gram/client/react-query/listSetupTasks.js", () => ({
  invalidateAllListSetupTasks: (...args: unknown[]) =>
    mocks.invalidate(...args),
}));

vi.mock("@gram/client/react-query/updateSetupTask.js", () => ({
  useUpdateSetupTaskMutation: () => ({
    mutateAsync: mocks.update,
    isPending: mocks.updatePending,
  }),
}));

vi.mock("@gram/client/react-query/sendInvite.js", () => ({
  useSendInviteMutation: () => ({
    mutateAsync: mocks.invite,
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: { roles: [] } }),
}));

vi.mock("@/lib/pylon", () => ({
  showPylonChat: mocks.showPylonChat,
}));

vi.mock("sonner", () => ({
  toast: {
    info: mocks.toastInfo,
    error: mocks.toastError,
    success: mocks.toastSuccess,
  },
}));

vi.mock("./components/setup-task-dialog", () => ({
  SetupTaskDialog: ({
    task,
    onClose,
    onComplete,
    onSupport,
  }: {
    task: SetupTask | null;
    onClose: () => void;
    onComplete: () => void;
    onSupport: () => void;
  }) =>
    task ? (
      <div role="dialog" aria-label={task.title}>
        <button onClick={onClose}>Back</button>
        <button onClick={onComplete}>Complete task</button>
        <button onClick={onSupport}>Get support</button>
      </div>
    ) : null,
}));

vi.mock("./components/setup-task-assignment-dialog", () => ({
  SetupTaskAssignmentDialog: ({
    task,
    onAssignMember,
    onAssignEmail,
    onUnassign,
  }: {
    task: SetupTask | null;
    onAssignMember: (id: string) => void;
    onAssignEmail: (email: string, role: string) => void;
    onUnassign: () => void;
  }) =>
    task ? (
      <div role="dialog" aria-label={`Assign ${task.title}`}>
        <button onClick={() => onAssignMember("user-member")}>
          Choose member
        </button>
        <button
          onClick={() => onAssignEmail("invitee@example.com", "role-member")}
        >
          Choose email and invite
        </button>
        <button onClick={onUnassign}>Unassign</button>
      </div>
    ) : null,
}));

const tasks: SetupTask[] = [
  {
    key: "connect-idp",
    title: "Connect identity provider",
    description: "Connect SSO",
    status: "todo",
    completedByFact: false,
    blockedBy: [],
    hidden: false,
  },
  {
    key: "instrument-agents",
    title: "Instrument agents",
    description: "Install hooks",
    status: "in_progress",
    completedByFact: false,
    blockedBy: [],
    hidden: false,
    assignee: {
      userId: "user-current",
      email: "current@example.com",
      name: "Current User",
    },
  },
  {
    key: "confirm-traffic",
    title: "Confirm traffic",
    description: "See hook traffic",
    status: "awaiting_support",
    completedByFact: false,
    blockedBy: ["instrument-agents"],
    hidden: false,
  },
  {
    key: "configure-policies",
    title: "Configure policies",
    description: "Set policy defaults",
    status: "done",
    completedByFact: false,
    blockedBy: [],
    hidden: false,
  },
];

const hiddenTask: SetupTask = {
  key: "platform-mcp",
  title: "Set up Platform MCP",
  description: "Connect Platform MCP",
  status: "todo",
  completedByFact: false,
  blockedBy: [],
  hidden: true,
};

beforeEach(() => {
  cleanup();
  mocks.platformAdmin = false;
  mocks.canAdmin = true;
  mocks.setupQuery.mockReset();
  mocks.setupQuery.mockReturnValue({
    data: { tasks },
    isPending: false,
    isError: false,
  });
  mocks.update.mockReset();
  mocks.update.mockResolvedValue(tasks[0]);
  mocks.updatePending = false;
  mocks.invite.mockReset();
  mocks.invite.mockResolvedValue({});
  mocks.invalidate.mockReset();
  mocks.invalidate.mockResolvedValue(undefined);
  mocks.toastInfo.mockReset();
  mocks.toastError.mockReset();
  mocks.toastSuccess.mockReset();
  mocks.showPylonChat.mockReset();
});

describe("SetupBoard", () => {
  it("renders four status columns, blocks prerequisites, and completes dialog tasks", async () => {
    render(<SetupBoard />);

    for (const heading of [
      "To do",
      "In progress",
      "Awaiting support",
      "Done",
    ]) {
      expect(screen.getByRole("heading", { name: heading })).toBeTruthy();
    }
    expect(screen.getByRole("region", { name: "Setup board" })).toBeTruthy();
    expect(screen.getByText("Blocked by Instrument agents")).toBeTruthy();
    const blockedTask = screen.getByTestId("setup-task-confirm-traffic");
    const viewBlockedTask = within(blockedTask).getByRole("button", {
      name: "View task: Confirm traffic",
    });
    expect(viewBlockedTask.hasAttribute("disabled")).toBe(false);
    fireEvent.click(viewBlockedTask);
    expect(
      screen.getByRole("dialog", { name: "Confirm traffic" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByText("1 of 4 tasks complete")).toBeTruthy();
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(screen.queryByText("4 tasks")).toBeNull();

    fireEvent.click(
      within(screen.getByTestId("setup-task-connect-idp")).getByRole("button", {
        name: "Start: Connect identity provider",
      }),
    );
    expect(
      screen.getByRole("dialog", { name: "Connect identity provider" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(
      screen.queryByRole("dialog", { name: "Connect identity provider" }),
    ).toBeNull();

    fireEvent.click(
      within(screen.getByTestId("setup-task-connect-idp")).getByRole("button", {
        name: "Start: Connect identity provider",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete task" }));
    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateSetupTaskRequestBody: {
            taskKey: "connect-idp",
            status: "done",
          },
        },
      }),
    );
    expect(mocks.invalidate).toHaveBeenCalled();
  });

  it("lets admins view another owner's task without calling it continue", () => {
    mocks.setupQuery.mockReturnValue({
      data: {
        tasks: [
          {
            ...tasks[1],
            assignee: {
              userId: "user-other",
              email: "other@example.com",
              name: "Other User",
            },
          },
        ],
      },
      isPending: false,
      isError: false,
    });
    render(<SetupBoard />);

    const card = screen.getByTestId("setup-task-instrument-agents");
    const viewTask = within(card).getByRole("button", {
      name: "View task: Instrument agents",
    });
    expect(viewTask.hasAttribute("disabled")).toBe(false);
    expect(within(card).queryByText("Continue task")).toBeNull();
  });

  it("persists support status before opening chat and closing the modal", async () => {
    let finishUpdate = (_value: SetupTask) => {};
    mocks.update.mockReturnValueOnce(
      new Promise<SetupTask>((resolve) => {
        finishUpdate = resolve;
      }),
    );
    render(<SetupBoard />);

    fireEvent.click(
      within(screen.getByTestId("setup-task-connect-idp")).getByRole("button", {
        name: "Start: Connect identity provider",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Get support" }));

    expect(mocks.update).toHaveBeenCalledWith({
      request: {
        updateSetupTaskRequestBody: {
          taskKey: "connect-idp",
          status: "awaiting_support",
        },
      },
    });
    expect(
      screen.getByRole("dialog", { name: "Connect identity provider" }),
    ).toBeTruthy();
    expect(mocks.showPylonChat).not.toHaveBeenCalled();

    finishUpdate(tasks[0]!);
    await waitFor(() => expect(mocks.showPylonChat).toHaveBeenCalledOnce());
    expect(
      screen.queryByRole("dialog", { name: "Connect identity provider" }),
    ).toBeNull();
  });

  it("does not offer awaiting support in the generic status menu", () => {
    render(<SetupBoard />);
    expect(
      screen.queryByRole("button", { name: "Move to awaiting support" }),
    ).toBeNull();
  });

  it("moves a task to the status column it is dropped into", async () => {
    render(<SetupBoard />);

    const draggedTask = screen.getByTestId("setup-task-draggable-connect-idp");
    const destination = screen.getByTestId("setup-column-in_progress");
    const dataTransfer = {
      effectAllowed: "none",
      setData: vi.fn(),
    };

    fireEvent.dragStart(draggedTask, { dataTransfer });
    fireEvent.dragOver(
      screen.getByTestId("setup-task-draggable-instrument-agents"),
      { clientY: 0, dataTransfer },
    );
    const dropSpace = screen.getByTestId("setup-task-drop-space");
    expect(
      dropSpace.nextElementSibling?.contains(
        screen.getByTestId("setup-task-instrument-agents"),
      ),
    ).toBe(true);

    // The placeholder now sits under the pointer. It must not bubble an event
    // that relocates itself to the end of the column.
    fireEvent.dragOver(dropSpace, { dataTransfer });
    expect(
      dropSpace.nextElementSibling?.contains(
        screen.getByTestId("setup-task-instrument-agents"),
      ),
    ).toBe(true);
    fireEvent.drop(destination, { dataTransfer });

    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateSetupTaskRequestBody: {
            taskKey: "connect-idp",
            status: "in_progress",
          },
        },
      }),
    );
  });

  it("disables task openers without status permission", () => {
    mocks.canAdmin = false;
    render(<SetupBoard />);

    const unassignedTask = screen.getByTestId("setup-task-connect-idp");
    expect(
      within(unassignedTask)
        .getAllByRole("button", { name: /Connect identity provider/ })
        .every((button) => button.hasAttribute("disabled")),
    ).toBe(true);
  });

  it("disables task openers while an update is pending", () => {
    mocks.updatePending = true;
    render(<SetupBoard />);

    const task = screen.getByTestId("setup-task-connect-idp");
    expect(
      within(task)
        .getAllByRole("button", { name: /Connect identity provider/ })
        .every((button) => button.hasAttribute("disabled")),
    ).toBe(true);
  });

  it("hides manual status moves for fact-completed tasks", () => {
    mocks.setupQuery.mockReturnValue({
      data: { tasks: [{ ...tasks[0], completedByFact: true }] },
      isPending: false,
      isError: false,
    });
    render(<SetupBoard />);

    expect(screen.queryByRole("button", { name: /Move to/ })).toBeNull();
  });

  it("renders loading and query error states", () => {
    mocks.setupQuery.mockReturnValue({
      data: undefined,
      isPending: true,
      isError: false,
    });
    const view = render(<SetupBoard />);
    expect(document.querySelector(".h-80")).toBeTruthy();

    view.unmount();
    mocks.setupQuery.mockReturnValue({
      data: undefined,
      isPending: false,
      isError: true,
      refetch: vi.fn(),
    });
    render(<SetupBoard />);
    expect(screen.getByText("Could not load setup tasks")).toBeTruthy();
  });

  it("assigns before inviting, preserves assignment on invite failure, and retries only the invite", async () => {
    const order: string[] = [];
    mocks.update.mockImplementation(async () => {
      order.push("assign");
      return tasks[0];
    });
    mocks.invite.mockImplementationOnce(async () => {
      order.push("invite");
      throw new Error("delivery failed");
    });
    render(<SetupBoard />);

    fireEvent.click(
      within(screen.getByTestId("setup-task-connect-idp")).getByRole("button", {
        name: "Assign",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Choose email and invite" }),
    );

    await waitFor(() => expect(order).toEqual(["assign", "invite"]));
    expect(mocks.update).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Retry invite" })).toBeTruthy();

    mocks.invite.mockImplementationOnce(async () => {
      order.push("retry");
      return {};
    });
    fireEvent.click(screen.getByRole("button", { name: "Retry invite" }));
    await waitFor(() => expect(order).toEqual(["assign", "invite", "retry"]));
    expect(mocks.update).toHaveBeenCalledTimes(1);
  });

  it("supports assignment, unassignment, status permissions, reminder stubbing, and platform controls", async () => {
    mocks.platformAdmin = true;
    mocks.setupQuery.mockImplementation(
      (_organizationId: string, includeHidden: boolean) => ({
        data: { tasks: includeHidden ? [...tasks, hiddenTask] : tasks },
        isPending: false,
        isError: false,
      }),
    );
    const view = render(<SetupBoard />);

    expect(
      screen.getByRole("switch", { name: "Show hidden tasks" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("switch", { name: "Show hidden tasks" }));
    expect(mocks.setupQuery).toHaveBeenLastCalledWith("org-one", true, {
      retry: false,
    });

    const hiddenCard = screen.getByTestId("setup-task-platform-mcp");
    expect(within(hiddenCard).getByText("Hidden")).toBeTruthy();
    fireEvent.click(
      within(hiddenCard).getByRole("button", { name: "Restore task" }),
    );
    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateSetupTaskRequestBody: {
            taskKey: "platform-mcp",
            hidden: false,
          },
        },
      }),
    );

    const card = screen.getByTestId("setup-task-connect-idp");
    expect(
      within(screen.getByTestId("setup-task-instrument-agents")).getByRole(
        "button",
        { name: "Move to done" },
      ),
    ).toBeTruthy();
    fireEvent.click(within(card).getByRole("button", { name: "Assign" }));
    fireEvent.click(screen.getByRole("button", { name: "Choose member" }));
    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateSetupTaskRequestBody: {
            taskKey: "connect-idp",
            assignee: { userId: "user-member" },
          },
        },
      }),
    );

    fireEvent.click(within(card).getByRole("button", { name: "Assign" }));
    fireEvent.click(screen.getByRole("button", { name: "Unassign" }));
    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateSetupTaskRequestBody: {
            taskKey: "connect-idp",
            clearAssignee: true,
          },
        },
      }),
    );

    fireEvent.click(
      within(screen.getByTestId("setup-task-instrument-agents")).getByRole(
        "button",
        { name: "Send reminder" },
      ),
    );
    expect(mocks.toastInfo).toHaveBeenCalledWith(
      "Reminder delivery is not available yet",
    );
    const requestCount =
      mocks.update.mock.calls.length + mocks.invite.mock.calls.length;
    expect(requestCount).toBe(3);

    fireEvent.click(within(card).getByRole("button", { name: "Hide task" }));
    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateSetupTaskRequestBody: { taskKey: "connect-idp", hidden: true },
        },
      }),
    );

    mocks.canAdmin = false;
    mocks.platformAdmin = false;
    view.unmount();
    render(<SetupBoard />);
    expect(
      screen.queryByRole("switch", { name: "Show hidden tasks" }),
    ).toBeNull();
    expect(
      within(screen.getByTestId("setup-task-connect-idp")).queryByRole(
        "button",
        { name: "Move to in progress" },
      ),
    ).toBeNull();
  });
});
