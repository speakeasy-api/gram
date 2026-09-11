import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { OnboardingBoard } from "./onboarding-board";
import { resolveBoardTasks } from "./board-store";
import { ONBOARDING_TASKS } from "./tasks";

const state = vi.hoisted(() => ({
  board: {} as ReturnType<
    typeof import("./use-onboarding-board").useOnboardingBoard
  >,
  search: new URLSearchParams(),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, isLoading: false, error: null }),
}));
vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
  useParams: () => ({ orgSlug: "acme" }),
  useSearchParams: () => [state.search, vi.fn()],
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("./use-onboarding-board", () => ({
  useOnboardingBoard: () => state.board,
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({
    organization: { id: "org-a" },
    user: { id: "user-me", email: "dev@example.test", isAdmin: false },
    session: "session-a",
    organizationOverride: false,
  }),
}));
vi.mock("./task-dialog", () => ({
  TaskDialog: ({
    task,
    error,
  }: {
    task: { title: string } | null;
    error: string | null;
  }) =>
    task && (
      <div role="dialog">
        {task.title}
        {error && <p>{error}</p>}
      </div>
    ),
}));
vi.mock("./assignee-picker", () => ({ AssigneePicker: () => null }));
vi.mock("@/components/ui/MoreActions", () => ({ MoreActions: () => null }));
vi.mock("../onboarding-header", () => ({ OnboardingHeader: () => null }));
vi.mock("../onboarding-footer", () => ({ OnboardingFooter: () => null }));

beforeEach(() => {
  state.search = new URLSearchParams("view=workstreams");
  state.board = {
    tasks: resolveBoardTasks(
      ONBOARDING_TASKS.map(({ id }) => ({
        key: id,
        title: id,
        description: "Server description",
        status: "todo",
        completedByFact: false,
        hidden: false,
        blockedBy: [],
      })),
    ),
    isLoading: false,
    isPending: false,
    error: undefined,
    writeError: null,
    writeErrorTaskId: null,
    canAssign: true,
    canHideTasks: false,
    canSetStatus: () => true,
    retry: vi.fn(),
    setStatus: vi.fn(),
    assign: vi.fn(),
    setHidden: vi.fn(),
  };
});
afterEach(() => {
  cleanup();
  localStorage.clear();
});
function renderBoard() {
  return render(
    <TooltipProvider>
      <OnboardingBoard />
    </TooltipProvider>,
  );
}

describe("explicit workstreams", () => {
  it("does not carry another task's write error into a dialog", () => {
    state.board.writeError = "Task A failed";
    state.board.writeErrorTaskId = "instrument-agents";
    state.search.set("task", "connect-idp");
    renderBoard();
    expect(screen.getByRole("dialog").textContent).toBe("connect-idp");
  });
  it("excludes hidden and optional tasks from required progress", () => {
    state.board.tasks = state.board.tasks.map((task) => ({
      ...task,
      hidden: task.id === "identity-provider",
      status: "done",
    }));
    const required = state.board.tasks.filter(
      (task) => !task.hidden && !task.badge,
    ).length;
    renderBoard();
    expect(
      screen.getByText(`${required} of ${required} required tasks complete`),
    ).toBeTruthy();
    expect(screen.queryByText("identity-provider")).toBeNull();
  });
  it("renders the server selection without importing browser progress", () => {
    localStorage.setItem("gram-onboarding-board:acme", "preserved");
    renderBoard();
    expect(
      screen.getByRole("region", { name: "Setup workstreams" }),
    ).toBeTruthy();
    const requiredCount = ONBOARDING_TASKS.filter((task) => !task.badge).length;
    expect(
      screen.getByText(`0 of ${requiredCount} required tasks complete`),
    ).toBeTruthy();
    for (const task of ONBOARDING_TASKS)
      expect(screen.getByText(task.id)).toBeTruthy();
    expect(localStorage.getItem("gram-onboarding-board:acme")).toBe(
      "preserved",
    );
  });
  it("distinguishes empty assignment from an empty selection", () => {
    renderBoard();
    fireEvent.click(screen.getByRole("switch", { name: "Assigned to me" }));
    expect(screen.getByText("No tasks assigned to you")).toBeTruthy();
    expect(screen.queryByText("No selected tasks")).toBeNull();
  });
  it("reports an empty selection without a zero denominator", () => {
    state.board.tasks = [];
    renderBoard();
    expect(screen.getByText("No selected tasks")).toBeTruthy();
    expect(screen.getByText("No required tasks")).toBeTruthy();
  });
  it("opens legacy links but not hidden tasks", () => {
    state.search.set("step", "connect-idp");
    const rendered = renderBoard();
    expect(screen.getByRole("dialog").textContent).toBe("connect-idp");
    state.board.tasks = state.board.tasks.map((task) => ({
      ...task,
      hidden: true,
    }));
    rendered.rerender(
      <TooltipProvider>
        <OnboardingBoard />
      </TooltipProvider>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      screen.getByText("This task is not part of your current onboarding"),
    ).toBeTruthy();
  });
  it("offers retry on read failure rather than reporting empty success", () => {
    state.board.error = "Network unavailable";
    renderBoard();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.board.retry).toHaveBeenCalledOnce();
    expect(screen.queryByText("No selected tasks")).toBeNull();
  });
});
