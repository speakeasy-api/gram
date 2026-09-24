import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { OnboardingBoard } from "./onboarding-board";
import { resolveBoardTasks } from "./board-store";
import { SETUP_CONTAINER } from "../setup-container";
import { ONBOARDING_TASKS, resolveWorkstreams } from "./tasks";
import { ONBOARDING_WORKSTREAMS } from "./workstream-fixtures";

const state = vi.hoisted(() => ({
  board: {} as ReturnType<
    typeof import("./use-onboarding-board").useOnboardingBoard
  >,
  search: new URLSearchParams(),
  hash: "",
  navigate: vi.fn(),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, isLoading: false, error: null }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ setupWizard: { href: () => "/acme/setup/wizard" } }),
}));
vi.mock("react-router", () => ({
  useNavigate: () => state.navigate,
  useLocation: () => ({ hash: state.hash }),
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
vi.mock("./assignee-picker", () => ({ AssigneePicker: () => null }));
vi.mock("@/components/ui/MoreActions", () => ({ MoreActions: () => null }));
vi.mock("../onboarding-header", () => ({ OnboardingHeader: () => null }));
vi.mock("../onboarding-footer", () => ({ OnboardingFooter: () => null }));

beforeEach(() => {
  state.search = new URLSearchParams();
  state.hash = "";
  state.navigate.mockReset();
  state.board = {
    workstreams: ONBOARDING_WORKSTREAMS,
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
    unsupportedTaskKeys: [],
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
    assignWorkstream: vi.fn(),
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

describe("workstreams-only onboarding", () => {
  it("renders loading placeholders before the catalog arrives", () => {
    state.board.isLoading = true;
    state.board.workstreams = [];
    renderBoard();
    // Four workstream placeholders, each with a heading and two cards.
    expect(screen.getByRole("main").querySelectorAll(".skeleton")).toHaveLength(
      12,
    );
    expect(
      screen.queryByRole("region", { name: "Setup workstreams" }),
    ).toBeNull();
  });
  it("uses server workstream titles, membership, and both levels of ordering", () => {
    state.board.workstreams = resolveWorkstreams([
      {
        id: "new-stream",
        title: "Server first",
        taskKeys: ["instrument-agents", "future-task", "domain-verification"],
      },
      {
        id: "connect",
        title: "Renamed identity",
        taskKeys: ["configure-policies"],
      },
      { id: "empty", title: "No supported tasks", taskKeys: ["future-task"] },
    ]);
    state.board.unsupportedTaskKeys = ["future-task"];
    renderBoard();
    const board = screen.getByRole("region", { name: "Setup workstreams" });
    expect(
      within(board)
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Server first", "Renamed identity"]);
    const first = screen.getByRole("region", { name: "Server first" });
    const instrument = within(first).getByText("instrument-agents");
    const domain = within(first).getByText("domain-verification");
    expect(
      instrument.compareDocumentPosition(domain) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      within(
        screen.getByRole("region", { name: "Renamed identity" }),
      ).getByText("configure-policies"),
    ).toBeTruthy();
    expect(screen.queryByText("Connect identity")).toBeNull();
    expect(screen.getByRole("alert").textContent).toContain("future-task");
  });
  it("does not recreate local workstreams for an empty server catalog", () => {
    state.board.workstreams = [];
    renderBoard();
    expect(
      within(
        screen.getByRole("region", { name: "Setup workstreams" }),
      ).queryAllByRole("heading"),
    ).toHaveLength(0);
  });
  it("uses the shared setup frame and includes domain verification", () => {
    renderBoard();
    const main = screen.getByRole("main");
    for (const name of SETUP_CONTAINER.split(" ")) {
      expect(main.firstElementChild?.classList.contains(name)).toBe(true);
    }
    expect(screen.getByText("domain-verification")).toBeTruthy();
  });
  it.each(["", "kanban", "workstreams", "wizard"])(
    "renders workstreams regardless of legacy view=%s",
    (view) => {
      if (view) state.search.set("view", view);
      renderBoard();
      expect(
        screen.getByRole("region", { name: "Setup workstreams" }),
      ).toBeTruthy();
    },
  );
  it("routes task deep links to the wizard", () => {
    state.board.writeError = "Task A failed";
    state.board.writeErrorTaskId = "instrument-agents";
    state.search.set("task", "connect-idp");
    renderBoard();
    expect(state.navigate).toHaveBeenCalledWith(
      { pathname: "/acme/setup/wizard", search: "task=connect-idp", hash: "" },
      { replace: true },
    );
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
  it("counts fact-verified required tasks as complete even with todo status", () => {
    state.board.tasks = state.board.tasks
      .filter((task) => task.id === "domain-verification")
      .map((task) => ({ ...task, status: "todo", verified: true }));
    expect(state.board.tasks).toHaveLength(1);
    renderBoard();
    expect(screen.getByText("1 of 1 required tasks complete")).toBeTruthy();
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
  it("routes legacy links to the wizard but not hidden tasks", () => {
    state.search.set("step", "connect-idp");
    const rendered = renderBoard();
    expect(state.navigate).toHaveBeenCalledWith(
      { pathname: "/acme/setup/wizard", search: "task=connect-idp", hash: "" },
      { replace: true },
    );
    state.board.tasks = state.board.tasks.map((task) => ({
      ...task,
      hidden: true,
    }));
    rendered.rerender(
      <TooltipProvider>
        <OnboardingBoard />
      </TooltipProvider>,
    );
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

it("opens a card in the linear wizard with its task and project context", () => {
  state.search.set("projectSlug", "example-project");
  state.search.set("view", "workstreams");
  renderBoard();
  fireEvent.click(
    screen.getByRole("button", { name: "instrument-agents, To Do" }),
  );
  expect(state.navigate).toHaveBeenCalledWith({
    pathname: "/acme/setup/wizard",
    hash: "",
    search:
      "projectSlug=example-project&task=instrument-agents&from=workstreams",
  });
});

it("keeps workstream progress stable when filtering to assigned cards", () => {
  state.board.tasks = state.board.tasks.map((task) => ({
    ...task,
    status: task.id === "connect-idp" ? "done" : "todo",
    assignee:
      task.id === "identity-provider"
        ? { kind: "email", email: "dev@example.test" }
        : undefined,
  }));
  renderBoard();
  const progress = screen.getByRole("progressbar", {
    name: "Connect identity progress",
  });
  expect(progress.getAttribute("aria-valuenow")).toBe("1");
  expect(progress.getAttribute("aria-valuemax")).toBe("4");
  fireEvent.click(screen.getByRole("switch", { name: "Assigned to me" }));
  expect(screen.queryByText("connect-idp")).toBeNull();
  expect(screen.getByText("1 / 4 required tasks complete")).toBeTruthy();
  expect(progress.getAttribute("aria-valuenow")).toBe("1");
  expect(progress.getAttribute("aria-valuemax")).toBe("4");
});

it.each([false, true])(
  "opens the prerequisite wizard directly (hidden: %s)",
  async (hidden) => {
    state.search = new URLSearchParams("project=project-a&view=workstreams");
    state.hash = "#details";
    state.board.canHideTasks = hidden;
    state.board.tasks = state.board.tasks.map((task) => ({
      ...task,
      hidden: task.id === "domain-verification" && hidden,
      blockedBy: task.id === "connect-idp" ? ["domain-verification"] : [],
      assignee:
        task.id === "connect-idp"
          ? { kind: "email", email: "dev@example.test" }
          : undefined,
    }));
    renderBoard();
    fireEvent.click(screen.getByRole("switch", { name: "Assigned to me" }));
    expect(
      screen.queryByRole("button", { name: "domain-verification, To Do" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "connect-idp, To Do" }));
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Go to task: Verify your domain",
      }),
    );
    expect(state.navigate).toHaveBeenCalledWith({
      pathname: "/acme/setup/wizard",
      search: "project=project-a&task=domain-verification&from=workstreams",
      hash: "#details",
    });
    expect(
      screen
        .getByRole("switch", { name: "Assigned to me" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(state.board.setHidden).not.toHaveBeenCalled();
  },
);

it("does not expose actionable prerequisite links to readers", async () => {
  state.board.canAssign = false;
  state.board.tasks = state.board.tasks.map((task) => ({
    ...task,
    blockedBy: task.id === "connect-idp" ? ["domain-verification"] : [],
  }));
  renderBoard();
  fireEvent.click(screen.getByRole("button", { name: "connect-idp, To Do" }));
  const prerequisites = await screen.findByRole("list", {
    name: "Prerequisite tasks",
  });
  expect(within(prerequisites).getByText("Verify your domain")).toBeTruthy();
  expect(within(prerequisites).queryByRole("button")).toBeNull();
  expect(
    within(prerequisites).getByText(/Ask an organization admin/),
  ).toBeTruthy();
  expect(state.navigate).not.toHaveBeenCalled();
  expect(state.board.setHidden).not.toHaveBeenCalled();
});

it("warns about unsupported server tasks rather than claiming onboarding completion", () => {
  state.board.unsupportedTaskKeys = ["future-task"];
  state.board.tasks = [];
  renderBoard();
  expect(screen.getByRole("alert").textContent).toContain("future-task");
  expect(screen.getByRole("alert").textContent).toContain(
    "not all onboarding work",
  );
});
it("keeps reader deep links on the board and explains admin-required opening", () => {
  state.board.canAssign = false;
  state.search.set("task", "connect-idp");
  renderBoard();
  expect(state.navigate).not.toHaveBeenCalled();
  expect(screen.getByText(/An organization admin is required/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "connect-idp, To Do" }));
  expect(state.navigate).not.toHaveBeenCalled();
});
it("preserves the hash when redirecting a task deep link", () => {
  state.hash = "#details";
  state.search.set("task", "connect-idp");
  renderBoard();
  expect(state.navigate).toHaveBeenCalledWith(
    expect.objectContaining({ hash: "#details" }),
    { replace: true },
  );
});
it("preserves the hash when opening a card", () => {
  state.hash = "#details";
  renderBoard();
  fireEvent.click(screen.getByRole("button", { name: "connect-idp, To Do" }));
  expect(state.navigate).toHaveBeenCalledWith(
    expect.objectContaining({ hash: "#details" }),
  );
});
