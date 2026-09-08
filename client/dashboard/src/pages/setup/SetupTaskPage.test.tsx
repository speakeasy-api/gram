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
import { StepSection } from "./components/step-section";

const mocks = vi.hoisted(() => ({
  taskSlug: "anthropic-observability",
  platformAdmin: false,
  setupQuery: vi.fn(),
  update: vi.fn(),
  updatePending: false,
  invalidate: vi.fn(),
  goToBoard: vi.fn(),
  showPylonChat: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  marketplacePublished: false,
}));

vi.mock("react-router", () => ({
  useParams: () => ({ taskSlug: mocks.taskSlug }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
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
// A stand-in card with two real StepSections, so the rail is fed the same
// way the real cards feed it.
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
      <StepSection
        index={1}
        title="Publish plugin marketplace"
        complete={mocks.marketplacePublished}
      >
        <span>marketplace body</span>
      </StepSection>
      <StepSection index={2} title="Confirm traffic">
        <span>traffic body</span>
      </StepSection>
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
    key: "anthropic-observability",
    title: "Set up Anthropic observability",
    description: "Connect Claude Code and Cowork",
    status: "in_progress",
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

function rail(): HTMLElement {
  return screen.getByRole("navigation", { name: "Progress" });
}

afterEach(cleanup);
beforeEach(() => {
  mocks.taskSlug = "anthropic-observability";
  mocks.platformAdmin = false;
  mocks.updatePending = false;
  mocks.marketplacePublished = false;
  mocks.setupQuery.mockReset().mockReturnValue(loaded());
  mocks.update.mockReset().mockResolvedValue(tasks[1]);
  mocks.invalidate.mockReset();
  mocks.goToBoard.mockReset();
  mocks.showPylonChat.mockReset();
  mocks.toastSuccess.mockReset();
  mocks.toastError.mockReset();
});

describe("SetupTaskPage", () => {
  it("resolves the slug to its task and lists only that task's own steps", () => {
    render(<SetupTaskPage />);

    expect(
      screen.getByText("Content for anthropic-observability"),
    ).toBeTruthy();
    expect(rail().textContent).toContain("Publish plugin marketplace");
    expect(rail().textContent).toContain("Confirm traffic");
    expect(rail().textContent).not.toContain("Set up identity provider");
    expect(screen.getByText("0 of 2 complete")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("ticks a step off in the rail once its outcome lands", () => {
    const view = render(<SetupTaskPage />);
    expect(screen.getByText("0 of 2 complete")).toBeTruthy();

    mocks.marketplacePublished = true;
    view.rerender(<SetupTaskPage />);

    expect(screen.getByText("1 of 2 complete")).toBeTruthy();
  });

  it("accepts the task key as a slug too", () => {
    mocks.taskSlug = "identity-provider";
    render(<SetupTaskPage />);

    expect(screen.getByText("Content for identity-provider")).toBeTruthy();
  });

  it("completes the task and returns to the board", async () => {
    render(<SetupTaskPage />);

    fireEvent.click(screen.getByRole("button", { name: "Complete" }));

    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateSetupTaskRequestBody: {
            taskKey: "anthropic-observability",
            status: "done",
          },
        },
      }),
    );
    await waitFor(() => expect(mocks.goToBoard).toHaveBeenCalledOnce());
    expect(mocks.invalidate).toHaveBeenCalled();
    expect(mocks.toastSuccess).toHaveBeenCalled();
  });

  it("returns to the board from back and skip without changing status", () => {
    render(<SetupTaskPage />);

    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));

    expect(mocks.goToBoard).toHaveBeenCalledTimes(2);
    expect(mocks.update).not.toHaveBeenCalled();
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
          taskKey: "anthropic-observability",
          status: "awaiting_support",
        },
      },
    });
    expect(mocks.showPylonChat).not.toHaveBeenCalled();

    finishUpdate(tasks[1]!);
    await waitFor(() => expect(mocks.showPylonChat).toHaveBeenCalledOnce());
    expect(
      screen.getByText("Content for anthropic-observability"),
    ).toBeTruthy();
    expect(mocks.goToBoard).not.toHaveBeenCalled();
  });

  it("sends an unknown slug back to the board once the list has loaded", async () => {
    mocks.taskSlug = "no-such-task";
    render(<SetupTaskPage />);

    await waitFor(() => expect(mocks.goToBoard).toHaveBeenCalledOnce());
  });
});
