import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import type { SetupWorkstream } from "@gram/client/models/components/setupworkstream.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { OnboardingBoard } from "./onboarding-board";
import { SETUP_CONTAINER } from "../setup-container";
import { ONBOARDING_TASK_IDS } from "../../onboarding-tasks";
import type { Onboarding, OnboardingActions } from "../../use-onboarding";
import { SETUP_WORKSTREAMS } from "./workstream-fixtures";

const state = vi.hoisted(() => ({
  // Raw API data; the real model projection runs over it.
  tasks: [] as SetupTask[],
  workstreams: [] as SetupWorkstream[],
  onboarding: {} as Omit<Onboarding, "model">,
  actions: {} as OnboardingActions,
  search: new URLSearchParams(),
  hash: "",
  navigate: vi.fn(),
}));
vi.mock("../../use-onboarding", async () => {
  const { buildOnboardingModel } = await import("../../onboarding-model");
  return {
    useOnboarding: () => ({
      ...state.onboarding,
      model: buildOnboardingModel(state.tasks, state.workstreams),
    }),
    useOnboardingActions: () => state.actions,
  };
});
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

function serverTask(
  key: string,
  overrides: Partial<SetupTask> = {},
): SetupTask {
  return {
    key,
    title: key,
    description: "Server description",
    status: "todo",
    completedByFact: false,
    countsTowardProgress: key !== "platform-mcp",
    hidden: false,
    blockedBy: [],
    ...overrides,
  };
}

beforeEach(() => {
  state.search = new URLSearchParams();
  state.hash = "";
  state.navigate.mockReset();
  state.workstreams = SETUP_WORKSTREAMS;
  state.tasks = ONBOARDING_TASK_IDS.map((key) =>
    serverTask(key, {
      title: key === "domain-verification" ? "Verify your domain" : key,
    }),
  );
  state.onboarding = {
    isLoading: false,
    error: undefined,
    canAssign: true,
    canInspectHidden: false,
    retry: vi.fn(),
  };
  state.actions = {
    isPending: false,
    canSetStatus: () => true,
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
    state.onboarding.isLoading = true;
    state.workstreams = [];
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
    state.workstreams = [
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
    ];
    state.tasks.push(serverTask("future-task"));
    renderBoard();
    const board = screen.getByRole("region", { name: "Setup workstreams" });
    expect(
      within(board)
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Server first", "Renamed identity"]);
    const first = screen.getByRole("region", { name: "Server first" });
    const instrument = within(first).getByText("instrument-agents");
    const domain = within(first).getByText("Verify your domain");
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
    state.workstreams = [];
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
    expect(screen.getByText("Verify your domain")).toBeTruthy();
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
    state.search.set("task", "connect-idp");
    renderBoard();
    expect(state.navigate).toHaveBeenCalledWith(
      { pathname: "/acme/setup/wizard", search: "task=connect-idp", hash: "" },
      { replace: true },
    );
  });
  it("excludes hidden and optional tasks from required progress", () => {
    state.tasks = state.tasks.map((task) => ({
      ...task,
      hidden: task.key === "identity-provider",
      status: "done",
    }));
    const required = state.tasks.filter(
      (task) => !task.hidden && task.countsTowardProgress,
    ).length;
    renderBoard();
    expect(
      screen.getByText(`${required} of ${required} required tasks complete`),
    ).toBeTruthy();
    expect(screen.queryByText("identity-provider")).toBeNull();
  });
  it("counts fact-verified required tasks as complete even with todo status", () => {
    state.tasks = [
      serverTask("domain-verification", { completedByFact: true }),
    ];
    renderBoard();
    expect(screen.getByText("1 of 1 required tasks complete")).toBeTruthy();
  });
  it("renders the server selection without importing browser progress", () => {
    localStorage.setItem("gram-onboarding-board:acme", "preserved");
    renderBoard();
    expect(
      screen.getByRole("region", { name: "Setup workstreams" }),
    ).toBeTruthy();
    const requiredCount = state.tasks.filter(
      (task) => task.countsTowardProgress,
    ).length;
    expect(
      screen.getByText(`0 of ${requiredCount} required tasks complete`),
    ).toBeTruthy();
    for (const id of ONBOARDING_TASK_IDS.filter(
      (key) => key !== "domain-verification",
    ))
      expect(screen.getByText(id)).toBeTruthy();
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
    state.tasks = [];
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
    state.tasks = state.tasks.map((task) => ({ ...task, hidden: true }));
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
    state.onboarding.error = "Network unavailable";
    renderBoard();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.onboarding.retry).toHaveBeenCalledOnce();
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
  state.tasks = state.tasks.map((task) => ({
    ...task,
    status: task.key === "connect-idp" ? "done" : "todo",
    assignee:
      task.key === "identity-provider"
        ? { email: "dev@example.test" }
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
    state.onboarding.canInspectHidden = hidden;
    state.tasks = state.tasks.map((task) => ({
      ...task,
      hidden: task.key === "domain-verification" && hidden,
      blockedBy: task.key === "connect-idp" ? ["domain-verification"] : [],
      assignee:
        task.key === "connect-idp" ? { email: "dev@example.test" } : undefined,
    }));
    renderBoard();
    fireEvent.click(screen.getByRole("switch", { name: "Assigned to me" }));
    expect(
      screen.queryByRole("button", { name: "Verify your domain, To Do" }),
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
    expect(state.actions.setHidden).not.toHaveBeenCalled();
  },
);

it("does not expose actionable prerequisite links to readers", async () => {
  state.onboarding.canAssign = false;
  state.tasks = state.tasks.map((task) => ({
    ...task,
    blockedBy: task.key === "connect-idp" ? ["domain-verification"] : [],
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
  expect(state.actions.setHidden).not.toHaveBeenCalled();
});

it("shows unsupported server tasks as unopenable cards and still counts them", () => {
  state.workstreams = [
    {
      id: "connect",
      title: "Connect",
      taskKeys: ["future-task", "connect-idp"],
    },
  ];
  state.tasks = [
    serverTask("future-task"),
    serverTask("connect-idp", { status: "done" }),
  ];
  renderBoard();
  expect(screen.getByRole("alert").textContent).toContain("future-task");
  // Progress never claims completion while server work remains.
  expect(screen.getByText("1 of 2 required tasks complete")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "future-task, To Do" }));
  expect(state.navigate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "connect-idp, Done" }));
  expect(state.navigate).toHaveBeenCalledOnce();
});
it("keeps reader deep links on the board and explains admin-required opening", () => {
  state.onboarding.canAssign = false;
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
it("resolves a short ?task= alias written by the wizard", () => {
  state.search.set("task", "other-platforms");
  renderBoard();
  expect(state.navigate).toHaveBeenCalledWith(
    {
      pathname: "/acme/setup/wizard",
      search: "task=other-platforms",
      hash: "",
    },
    { replace: true },
  );
});
