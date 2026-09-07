import type { ReactNode } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import SetupTaskPage from "./SetupTaskPage";

const mocks = vi.hoisted(() => ({
  taskKey: "instrument-agents",
  platformAdmin: false,
  setupQuery: vi.fn(),
  update: vi.fn(),
  updatePending: false,
  invalidate: vi.fn(),
  goToTask: vi.fn(),
  goToBoard: vi.fn(),
  showPylonChat: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("react-router", () => ({
  useParams: () => ({ taskKey: mocks.taskKey }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setupTask: { goTo: mocks.goToTask },
    setup: {
      goTo: mocks.goToBoard,
      Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
    },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("./components/setup-shell", () => ({
  SetupShell: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("./components/setup-task-content", () => ({
  SetupTaskContent: ({
    taskKey,
    onComplete,
    onSkip,
    onBack,
    onSupport,
  }: {
    taskKey: string;
    onComplete: () => void;
    onSkip: () => void;
    onBack: () => void;
    onSupport: () => void;
  }) => (
    <div>
      <p>Content for {taskKey}</p>
      <button onClick={onComplete}>Complete</button>
      <button onClick={onSkip}>Skip</button>
      <button onClick={onBack}>Back</button>
      <button onClick={onSupport}>Get support</button>
    </div>
  ),
}));
vi.mock("@/hooks/useOrganizationSetupTasks", () => ({
  useOrganizationSetupTasks: (...args: unknown[]) => mocks.setupQuery(...args),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
  useIsPlatformAdmin: () => mocks.platformAdmin,
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
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
vi.mock("@/lib/pylon", () => ({ showPylonChat: mocks.showPylonChat }));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

const tasks: SetupTask[] = [
  {
    key: "identity-provider",
    title: "Set up identity provider",
    description: "Connect SSO and sync the directory",
    status: "done",
    completedByFact: true,
    blockedBy: [],
    hidden: false,
  },
  {
    key: "instrument-agents",
    title: "Set up observability in other platforms",
    description: "Connect coding agents",
    status: "in_progress",
    completedByFact: false,
    blockedBy: [],
    hidden: false,
    assignee: {
      userId: "user-priya",
      email: "priya@example.com",
      name: "Priya Raman",
    },
  },
  {
    key: "configure-policies",
    title: "Configure policies",
    description: "Pick the categories to flag",
    status: "awaiting_support",
    completedByFact: false,
    blockedBy: [],
    hidden: false,
  },
];

function loaded(list: SetupTask[] = tasks) {
  return {
    data: { tasks: list },
    isPending: false,
    isSuccess: true,
    isError: false,
    refetch: vi.fn(),
  };
}

afterEach(cleanup);
beforeEach(() => {
  mocks.taskKey = "instrument-agents";
  mocks.platformAdmin = false;
  mocks.updatePending = false;
  mocks.setupQuery.mockReset().mockReturnValue(loaded());
  mocks.update.mockReset().mockResolvedValue(tasks[1]);
  mocks.invalidate.mockReset();
  mocks.goToTask.mockReset();
  mocks.goToBoard.mockReset();
  mocks.showPylonChat.mockReset();
  mocks.toastSuccess.mockReset();
  mocks.toastError.mockReset();
});

describe("SetupTaskPage", () => {
  it("shows the task in the linear frame with a status timeline", () => {
    render(<SetupTaskPage />);

    expect(screen.getByText("Content for instrument-agents")).toBeTruthy();
    expect(screen.getByText("1 of 3 complete")).toBeTruthy();
    const timeline = screen.getByRole("navigation", { name: "Progress" });
    expect(timeline.textContent).toContain("Set up identity provider");
    expect(timeline.textContent).toContain("In progress");
    expect(timeline.textContent).toContain("Priya Raman");
    expect(timeline.textContent).toContain("Awaiting support");
    expect(
      screen.getByRole("button", { name: /Set up identity provider/ }),
    ).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("completes the task and moves on to the next one", async () => {
    render(<SetupTaskPage />);

    fireEvent.click(screen.getByRole("button", { name: "Complete" }));

    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateSetupTaskRequestBody: {
            taskKey: "instrument-agents",
            status: "done",
          },
        },
      }),
    );
    await waitFor(() =>
      expect(mocks.goToTask).toHaveBeenCalledWith("configure-policies"),
    );
    expect(mocks.invalidate).toHaveBeenCalled();
    expect(mocks.toastSuccess).toHaveBeenCalled();
  });

  it("returns to the board after the last task and from the first task's back", () => {
    mocks.taskKey = "configure-policies";
    const view = render(<SetupTaskPage />);
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    expect(mocks.goToBoard).toHaveBeenCalledOnce();
    expect(mocks.update).not.toHaveBeenCalled();
    view.unmount();

    mocks.taskKey = "identity-provider";
    render(<SetupTaskPage />);
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(mocks.goToBoard).toHaveBeenCalledTimes(2);
  });

  it("persists awaiting support before opening chat and stays on the page", async () => {
    let finishUpdate = (_value: SetupTask) => {};
    mocks.update.mockReturnValueOnce(
      new Promise<SetupTask>((resolve) => {
        finishUpdate = resolve;
      }),
    );
    render(<SetupTaskPage />);

    const support = screen.getByRole("button", { name: "Get support" });
    fireEvent.click(support);
    fireEvent.click(support);

    expect(mocks.update).toHaveBeenCalledOnce();
    expect(mocks.update).toHaveBeenCalledWith({
      request: {
        updateSetupTaskRequestBody: {
          taskKey: "instrument-agents",
          status: "awaiting_support",
        },
      },
    });
    expect(mocks.showPylonChat).not.toHaveBeenCalled();

    finishUpdate(tasks[1]!);
    await waitFor(() => expect(mocks.showPylonChat).toHaveBeenCalledOnce());
    expect(screen.getByText("Content for instrument-agents")).toBeTruthy();
    expect(mocks.goToTask).not.toHaveBeenCalled();
  });

  it("sends an unknown task key back to the board once the list has loaded", async () => {
    mocks.taskKey = "no-such-task";
    render(<SetupTaskPage />);

    await waitFor(() => expect(mocks.goToBoard).toHaveBeenCalledOnce());
  });

  it("jumps between tasks from the timeline", () => {
    render(<SetupTaskPage />);

    fireEvent.click(screen.getByRole("button", { name: /Configure policies/ }));

    expect(mocks.goToTask).toHaveBeenCalledWith("configure-policies");
  });
});
