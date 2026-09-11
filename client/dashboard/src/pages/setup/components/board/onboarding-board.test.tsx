import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { OnboardingBoard } from "./onboarding-board";
import { resolveBoardTasks } from "./board-store";
import { ONBOARDING_TASKS, ONBOARDING_WORKSTREAMS } from "./tasks";

const state = vi.hoisted(() => ({
  board: {} as ReturnType<
    typeof import("./use-onboarding-board").useOnboardingBoard
  >,
  search: new URLSearchParams(),
  session: {
    organization: { id: "org-a" },
    user: { id: "user-me", email: "dev@example.test", isAdmin: false },
    session: "session-a",
    organizationOverride: false,
  },
  admin: true,
  loading: false,
  error: null as Error | null,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => state.admin,
    isLoading: state.loading,
    error: state.error,
  }),
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
  useSession: () => state.session,
}));
vi.mock("./task-dialog", () => ({
  TaskDialog: ({ task }: { task: { title: string } | null }) =>
    task && <div role="dialog">{task.title}</div>,
}));
vi.mock("./assignee-picker", () => ({ AssigneePicker: () => null }));
vi.mock("@/components/ui/MoreActions", () => ({ MoreActions: () => null }));
vi.mock("../onboarding-header", () => ({ OnboardingHeader: () => null }));
vi.mock("../onboarding-footer", () => ({ OnboardingFooter: () => null }));

beforeEach(() => {
  state.admin = true;
  state.loading = false;
  state.error = null;
  state.session = {
    organization: { id: "org-a" },
    user: { id: "user-me", email: "dev@example.test", isAdmin: false },
    session: "session-a",
    organizationOverride: false,
  };
  state.search = new URLSearchParams();
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

describe("shared workstreams", () => {
  it.each([true, false])(
    "defaults customer admin=%s to all workstreams",
    (admin) => {
      state.admin = admin;
      renderBoard();
      expect(
        screen.getByRole("region", { name: "Setup workstreams" }),
      ).toBeTruthy();
      expect(screen.queryByRole("region", { name: "Setup Kanban" })).toBeNull();
      expect(
        screen
          .getByRole("switch", { name: "Assigned to me" })
          .getAttribute("aria-checked"),
      ).toBe("false");
      expect(Boolean(screen.queryByRole("button", { name: "Kanban" }))).toBe(
        admin,
      );
    },
  );
  it("uses authenticated support context, not platform admin, for its default", () => {
    state.session.organizationOverride = true;
    renderBoard();
    expect(screen.getByRole("region", { name: "Setup Kanban" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Workstreams" }));
    expect(
      screen.getByRole("region", { name: "Setup workstreams" }),
    ).toBeTruthy();
    expect(screen.getByText(/not member impersonation/)).toBeTruthy();
    expect(state.board.setStatus).not.toHaveBeenCalled();
  });
  it("keeps platform staff in workstreams outside support access", () => {
    state.session.user.isAdmin = true;
    renderBoard();
    expect(
      screen.getByRole("region", { name: "Setup workstreams" }),
    ).toBeTruthy();
  });
  it("preserves selected task and project when switching views", () => {
    state.search = new URLSearchParams(
      "task=enable-logging&projectSlug=selected",
    );
    renderBoard();
    fireEvent.click(screen.getByRole("button", { name: "Kanban" }));
    expect(screen.getByRole("dialog").textContent).toBe("enable-logging");
    expect(state.search.toString()).toBe(
      "task=enable-logging&projectSlug=selected",
    );
  });
  it("resets view choice when the session changes or admin access is lost", () => {
    const rendered = renderBoard();
    const rerender = () =>
      rendered.rerender(
        <TooltipProvider>
          <OnboardingBoard />
        </TooltipProvider>,
      );
    fireEvent.click(screen.getByRole("button", { name: "Kanban" }));
    state.session.session = "session-b";
    rerender();
    expect(
      screen.getByRole("region", { name: "Setup workstreams" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Kanban" }));
    state.admin = false;
    rerender();
    expect(
      screen.getByRole("region", { name: "Setup workstreams" }),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Kanban" })).toBeNull();
  });
  it("does not choose an audience while grants load or fail", () => {
    state.loading = true;
    const rendered = renderBoard();
    expect(screen.getByText("Loading onboarding access...")).toBeTruthy();
    expect(screen.queryByRole("region")).toBeNull();
    state.loading = false;
    state.error = new Error("unavailable");
    rendered.rerender(
      <TooltipProvider>
        <OnboardingBoard />
      </TooltipProvider>,
    );
    expect(screen.getByRole("alert").textContent).toContain(
      "Could not load onboarding access",
    );
    expect(screen.queryByRole("region")).toBeNull();
  });
  it("waits for organization and session identity", () => {
    state.session.organization.id = "";
    renderBoard();
    expect(screen.queryByRole("region")).toBeNull();
  });
  it("normalizes legacy task selection and shows unknown canonical tasks", () => {
    state.search = new URLSearchParams("step=enable-logging");
    const rendered = renderBoard();
    expect(screen.getByRole("dialog").textContent).toBe("enable-logging");
    state.search = new URLSearchParams("task=unknown&step=enable-logging");
    rendered.rerender(
      <TooltipProvider>
        <OnboardingBoard />
      </TooltipProvider>,
    );
    expect(screen.getByText("Setup task not found")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("shows excluded links and assignment empties in Kanban too", () => {
    state.session.organizationOverride = true;
    state.search.set("task", "platform-mcp");
    state.board.tasks.find((task) => task.id === "platform-mcp")!.hidden = true;
    renderBoard();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      screen.getByText("This task is not part of your current onboarding"),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("switch", { name: "Assigned to me" }));
    expect(screen.getByText("No tasks assigned to you")).toBeTruthy();
  });
  it("renders all server tasks in exactly one workstream", () => {
    renderBoard();
    expect(screen.getByText("0 of 12 required tasks complete")).toBeTruthy();
    for (const task of ONBOARDING_TASKS)
      expect(screen.getByText(task.id)).toBeTruthy();
    const ids = ONBOARDING_WORKSTREAMS.flatMap((stream) => stream.taskIds);
    expect(new Set(ids).size).toBe(13);
    expect(ids).toHaveLength(13);
  });
  it("ignores but retains browser records", () => {
    const record = JSON.stringify({ "connect-idp": { status: "done" } });
    localStorage.setItem("gram-onboarding-board:acme", record);
    renderBoard();
    expect(screen.getByText("0 of 12 required tasks complete")).toBeTruthy();
    expect(localStorage.getItem("gram-onboarding-board:acme")).toBe(record);
    expect(
      screen.getByText(/Browser-only progress is not imported/),
    ).toBeTruthy();
  });
  it("distinguishes empty assignment from no selected tasks", () => {
    renderBoard();
    fireEvent.click(screen.getByRole("switch", { name: "Assigned to me" }));
    expect(screen.getByText("No tasks assigned to you")).toBeTruthy();
    expect(
      screen.queryByRole("region", { name: "Connect identity" }),
    ).toBeNull();
  });
  it("shows no selected tasks and no required tasks without a zero denominator", () => {
    state.board.tasks = [];
    renderBoard();
    expect(screen.getByText("No selected tasks")).toBeTruthy();
    expect(screen.getByText("No required tasks")).toBeTruthy();
  });
  it("excludes hidden and optional tasks from progress", () => {
    state.board.tasks = state.board.tasks.filter(
      (task) => task.id === "platform-mcp" || task.id === "connect-idp",
    );
    state.board.tasks.find((task) => task.id === "connect-idp")!.hidden = true;
    renderBoard();
    expect(screen.getAllByText("No required tasks").length).toBeGreaterThan(0);
    expect(screen.queryByText("connect-idp")).toBeNull();
  });
  it("never opens a hidden task for a reader", () => {
    state.search.set("task", "connect-idp");
    state.board.tasks.find((task) => task.id === "connect-idp")!.hidden = true;
    renderBoard();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      screen.getByText("This task is not part of your current onboarding"),
    ).toBeTruthy();
  });
  it("shows retry instead of an empty success on read failure", () => {
    state.board.error = "Network unavailable";
    renderBoard();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.board.retry).toHaveBeenCalledOnce();
    expect(screen.queryByText("No selected tasks")).toBeNull();
  });
});
