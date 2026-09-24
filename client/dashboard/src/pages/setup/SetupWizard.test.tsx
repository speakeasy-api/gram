import type { ReactNode } from "react";
import {
  cleanup,
  fireEvent,
  render as renderView,
  within,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { MemoryRouter, Routes, Route, useLocation } from "react-router";
import SetupWizard from "./SetupWizard";
import SetupTaskPage from "./SetupTaskPage";
import { OnboardingBoard } from "./components/board/onboarding-board";
import { StepSection } from "./components/step-section";

const mocks = vi.hoisted(() => ({
  setupQuery: vi.fn(),
  update: vi.fn(),
  platformAdmin: false,
  realRouter: false,
  invalidate: vi.fn(),
  goToBoard: vi.fn(),
  showPylonChat: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  toastWarning: vi.fn(),
  navigate: vi.fn(),
  searchParams: new URLSearchParams(),
  setSearchParams: vi.fn(),
}));

vi.mock("react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-router")>();
  return {
    ...actual,
    useParams: () =>
      mocks.realRouter ? actual.useParams() : { orgSlug: "org" },
    useSearchParams: () =>
      mocks.realRouter
        ? actual.useSearchParams()
        : [mocks.searchParams, mocks.setSearchParams],
    useNavigate: () =>
      mocks.realRouter ? actual.useNavigate() : mocks.navigate,
  };
});
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setup: { goTo: mocks.goToBoard, href: () => "/org/setup" },
    setupWizard: { href: () => "/org/setup/wizard" },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("./components/onboarding-header", () => ({
  OnboardingHeader: ({ children }: { children: ReactNode }) => (
    <header>{children}</header>
  ),
}));
vi.mock("./components/onboarding-footer", () => ({
  OnboardingFooter: () => null,
}));

function render(view: ReactNode) {
  return renderView(view, {
    wrapper: ({ children }) => (
      <MemoryRouter
        initialEntries={[`/org/setup/wizard?${mocks.searchParams}`]}
      >
        {children}
      </MemoryRouter>
    ),
  });
}
// A stand-in card with two real StepSections, so the rail is fed the same
// way the real cards feed it.
vi.mock("./components/setup-task-content", () => ({
  SetupTaskContent: ({
    taskKey,
    onComplete,
    onSupport,
  }: {
    taskKey: string;
    onComplete: () => void;
    onSupport: () => void;
  }) => (
    <div>
      <p>Content for {taskKey}</p>
      {taskKey !== "anthropic-observability" ? null : (
        <>
          <StepSection
            index={1}
            slug="publish-marketplace"
            title="Publish plugin marketplace"
          >
            <span>marketplace body</span>
          </StepSection>
          <StepSection index={2} slug="confirm-traffic" title="Confirm traffic">
            <span>traffic body</span>
          </StepSection>
        </>
      )}
      <button onClick={onComplete}>Complete</button>
      <button onClick={onSupport}>Get support</button>
    </div>
  ),
}));
vi.mock("@/hooks/useOrganizationSetupTasks", () => ({
  buildOrganizationSetupTasksQuery: (...args: unknown[]) =>
    mocks.setupQuery(...args),
  invalidateOrganizationSetupTasks: (...args: unknown[]) =>
    mocks.invalidate(...args),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => "client",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
  useSession: () => ({
    organization: { id: "org-one" },
    user: { id: "user-one", isAdmin: mocks.platformAdmin },
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
  useQuery: (query: unknown) => query,
}));
vi.mock("@gram/client/react-query/updateSetupTask.js", () => ({
  useUpdateSetupTaskMutation: () => ({ mutateAsync: mocks.update }),
}));
vi.mock("@gram/client/react-query/assignSetupWorkstream.js", () => ({
  useAssignSetupWorkstreamMutation: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@/lib/pylon", () => ({ showPylonChat: mocks.showPylonChat }));
vi.mock("sonner", () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warning: mocks.toastWarning,
  },
}));

function task(
  key: string,
  title: string,
  status: SetupTask["status"] = "todo",
): SetupTask {
  return {
    key,
    title,
    description: `${title} description`,
    status,
    completedByFact: false,
    countsTowardProgress: true,
    blockedBy: [],
    hidden: false,
  };
}

const tasks: SetupTask[] = [
  task("identity-provider", "Set up identity provider", "done"),
  task("anthropic-observability", "Set up Anthropic observability"),
  task("instrument-agents", "Set up observability in other platforms"),
];

// One workstream in list order unless a test supplies its own membership.
function loaded(
  list: SetupTask[] = tasks,
  workstreams = [
    { id: "all", title: "All", taskKeys: list.map((item) => item.key) },
  ],
) {
  return {
    data: { tasks: list, workstreams },
    isPending: false,
    isSuccess: true,
    isError: false,
    refetch: vi.fn(),
  };
}

function rail(): HTMLElement {
  return screen.getByRole("navigation", { name: "Progress" });
}

/** The ?task= / ?step= the last navigation would have written. */
function lastParams(): URLSearchParams {
  return new URLSearchParams(mocks.navigate.mock.calls.at(-1)?.[0].search);
}

afterEach(cleanup);
beforeEach(() => {
  mocks.platformAdmin = false;
  mocks.realRouter = false;
  mocks.searchParams = new URLSearchParams();
  mocks.setSearchParams.mockReset();
  mocks.navigate.mockReset();
  mocks.setupQuery.mockReset().mockReturnValue(loaded());
  mocks.update.mockReset().mockResolvedValue(tasks[1]);
  mocks.invalidate.mockReset();
  mocks.goToBoard.mockReset();
  mocks.showPylonChat.mockReset();
  mocks.toastSuccess.mockReset();
  mocks.toastError.mockReset();
  mocks.toastWarning.mockReset();
});

describe("SetupWizard", () => {
  it("lists every card in the rail and opens on the first one still open", () => {
    render(<SetupWizard />);

    expect(rail().textContent).toContain("Set up identity provider");
    expect(rail().textContent).toContain("Set up Anthropic observability");
    expect(rail().textContent).toContain(
      "Set up observability in other platforms",
    );
    expect(screen.getByText("1 of 3 required tasks complete")).toBeTruthy();
    expect(
      screen.getByText("Content for anthropic-observability"),
    ).toBeTruthy();
  });

  it("walks the board's default list, without hidden cards", () => {
    render(<SetupWizard />);

    expect(mocks.setupQuery).toHaveBeenCalledWith("client", "org-one", false, {
      retry: false,
    });
  });

  it("nests the open card's own sub-steps under it in the rail", () => {
    render(<SetupWizard />);

    const steps = screen.getByRole("list", { name: "Steps in this task" });
    expect(steps.textContent).toContain("Publish plugin marketplace");
    expect(steps.textContent).toContain("Confirm traffic");
    expect(rail().contains(steps)).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: /Confirm traffic/ }));

    expect(screen.getByText("traffic body").closest("section")?.hidden).toBe(
      false,
    );
  });

  it("opens on the card named by ?task=, by its URL slug", () => {
    mocks.searchParams = new URLSearchParams("task=other-platforms");
    render(<SetupWizard />);

    expect(screen.getByText("Content for instrument-agents")).toBeTruthy();
  });

  it("reports an unavailable explicit task instead of falling back", () => {
    mocks.searchParams = new URLSearchParams("task=no-such-card");
    render(<SetupWizard />);

    expect(screen.getByText("Setup task unavailable")).toBeTruthy();
    expect(
      screen.queryByText("Content for anthropic-observability"),
    ).toBeNull();
  });

  it("lands on the last card once every card is done", () => {
    mocks.setupQuery.mockReturnValue(
      loaded(tasks.map((t) => ({ ...t, status: "done" }))),
    );
    render(<SetupWizard />);

    expect(screen.getByText("Content for instrument-agents")).toBeTruthy();
    expect(screen.getByText("3 of 3 required tasks complete")).toBeTruthy();
  });

  it("moves between cards from the rail, dropping the outgoing card's step", () => {
    render(<SetupWizard />);

    fireEvent.click(
      screen.getByRole("button", { name: /Set up identity provider/ }),
    );

    const params = lastParams();
    expect(params.get("task")).toBe("idp");
    expect(params.get("step")).toBeNull();
    const [, options] = mocks.navigate.mock.calls.at(-1)!;
    expect(options).toEqual({ replace: true });
  });

  it("skips to the next card without touching its status", () => {
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Skip task" }));

    expect(lastParams().get("task")).toBe("other-platforms");
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("steps back to the previous card", () => {
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Previous task" }));

    expect(lastParams().get("task")).toBe("idp");
  });

  it("has no previous on the first card and skips to the dashboard from the last", () => {
    mocks.searchParams = new URLSearchParams("task=idp");
    const view = render(<SetupWizard />);
    expect(screen.queryByRole("button", { name: "Previous task" })).toBeNull();

    mocks.searchParams = new URLSearchParams("task=other-platforms");
    view.rerender(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Skip to dashboard" }));
    expect(mocks.navigate).toHaveBeenCalledWith({
      pathname: "/org",
      search: "",
      hash: "",
    });
  });

  it("completes the card and moves on to the next one", async () => {
    render(<SetupWizard />);

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
    await waitFor(() =>
      expect(lastParams().get("task")).toBe("other-platforms"),
    );
    expect(mocks.invalidate).toHaveBeenCalled();
    expect(mocks.toastSuccess).toHaveBeenCalled();
    expect(mocks.goToBoard).not.toHaveBeenCalled();
  });

  it("completes the last card and leaves for the dashboard", async () => {
    mocks.searchParams = new URLSearchParams("task=other-platforms");
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Complete" }));

    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith({
        pathname: "/org",
        search: "",
        hash: "",
      }),
    );
  });

  it("holds the reader's own moves while a completion is settling", async () => {
    mocks.update.mockReturnValue(new Promise(() => {}));
    render(<SetupWizard />);
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledOnce());

    // Completing advances once its mutation lands; a move made in that
    // window would be overwritten a moment later, so none is taken.
    const previous = screen.getByRole("button", { name: "Previous task" });
    const skip = screen.getByRole("button", { name: "Skip task" });
    expect(previous.hasAttribute("disabled")).toBe(true);
    expect(skip.hasAttribute("disabled")).toBe(true);
    fireEvent.click(previous);
    fireEvent.click(skip);
    // Rail rows stop presenting as buttons rather than ignoring clicks.
    expect(
      screen.queryByRole("button", { name: /Set up identity provider/ }),
    ).toBeNull();
    fireEvent.click(screen.getByText("Set up identity provider"));
    // The nested sub-step list holds too.
    const subStep = screen.getByRole("button", { name: /Confirm traffic/ });
    expect(subStep.hasAttribute("disabled")).toBe(true);
    fireEvent.click(subStep);

    expect(mocks.navigate).not.toHaveBeenCalled();
  });

  it("advances a verified task without rewriting its server-derived status", () => {
    // Verified tasks count as done, so the walk only lands on one by link.
    mocks.searchParams = new URLSearchParams("task=anthropic-observability");
    mocks.setupQuery.mockReturnValue(
      loaded(tasks.map((t) => ({ ...t, completedByFact: true }))),
    );
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Complete" }));

    expect(lastParams().get("task")).toBe("other-platforms");
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("opens support for a verified task without changing its status", () => {
    mocks.setupQuery.mockReturnValue(
      loaded(tasks.map((t) => ({ ...t, completedByFact: true }))),
    );
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Get support" }));

    expect(mocks.showPylonChat).toHaveBeenCalledOnce();
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("keeps holding after the mutation resolves until the refetch lands", async () => {
    let finishInvalidate = () => {};
    mocks.invalidate.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        finishInvalidate = resolve;
      }),
    );
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() => expect(mocks.invalidate).toHaveBeenCalled());

    // The mutation is done but the completion is still awaiting the refetch before it advances, so a
    // move now would be overwritten a moment later.
    const skip = screen.getByRole("button", { name: "Skip task" });
    expect(skip.hasAttribute("disabled")).toBe(true);
    fireEvent.click(skip);
    expect(
      screen.queryByRole("button", { name: /Set up identity provider/ }),
    ).toBeNull();
    fireEvent.click(screen.getByText("Set up identity provider"));
    expect(mocks.navigate).not.toHaveBeenCalled();

    finishInvalidate();
    await waitFor(() =>
      expect(lastParams().get("task")).toBe("other-platforms"),
    );
    expect(mocks.navigate).toHaveBeenCalledOnce();
  });

  it("stays put when completing fails", async () => {
    mocks.update.mockRejectedValueOnce(new Error("nope"));
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Complete" }));

    await waitFor(() => expect(mocks.toastError).toHaveBeenCalledWith("nope"));
    expect(mocks.navigate).not.toHaveBeenCalled();
  });

  it("persists awaiting support before opening chat and stays on the card", async () => {
    let finishUpdate = (_value: SetupTask) => {};
    mocks.update.mockReturnValueOnce(
      new Promise<SetupTask>((resolve) => {
        finishUpdate = resolve;
      }),
    );
    render(<SetupWizard />);

    const support = screen.getByRole("button", { name: "Get support" });
    fireEvent.click(support);
    fireEvent.click(support);

    expect(mocks.update).toHaveBeenCalledOnce();
    expect(mocks.showPylonChat).not.toHaveBeenCalled();

    finishUpdate(tasks[1]!);
    await waitFor(() => expect(mocks.showPylonChat).toHaveBeenCalledOnce());
    expect(
      screen.getByText("Content for anthropic-observability"),
    ).toBeTruthy();
    expect(mocks.navigate).not.toHaveBeenCalled();
  });

  it("points at the board when every card is hidden", () => {
    mocks.setupQuery.mockReturnValue(loaded([]));
    render(<SetupWizard />);

    expect(screen.getByText("Nothing to set up")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Setup board" }));
    expect(mocks.navigate).toHaveBeenCalledWith({
      pathname: "/org/setup",
      search: "",
      hash: "",
    });
  });

  it("offers a retry when the list fails to load", () => {
    const refetch = vi.fn();
    mocks.setupQuery.mockReturnValue({
      data: undefined,
      isPending: false,
      isSuccess: false,
      isError: true,
      error: new Error("Read failed"),
      refetch,
    });
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalledOnce();
  });
});

it("places the workstreams return above rail progress and keeps only a mobile header return", () => {
  mocks.searchParams = new URLSearchParams(
    "from=workstreams&task=anthropic-observability&step=confirm-traffic&projectSlug=selected&filter=mine",
  );
  render(<SetupWizard />);
  const progress = screen.getByText("1 of 3 required tasks complete");
  const railReturn = within(progress.parentElement!).getByRole("link", {
    name: "Workstreams",
  });
  expect(
    railReturn.compareDocumentPosition(progress) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(railReturn.querySelector("svg.lucide-arrow-left")).not.toBeNull();
  expect(railReturn.classList.contains("px-3")).toBe(true);
  expect(railReturn.classList.contains("-ms-3")).toBe(true);
  expect(railReturn.getAttribute("href")).toBe(
    "/org/setup?projectSlug=selected&filter=mine",
  );
  expect(railReturn.closest(".md\\:block")?.classList.contains("hidden")).toBe(
    true,
  );
  const headerReturns = within(screen.getByRole("banner")).getAllByRole(
    "link",
    { name: "Workstreams" },
  );
  expect(headerReturns).toHaveLength(1);
  expect(headerReturns[0]!.classList.contains("pl-0")).toBe(false);
  expect(headerReturns[0]!.parentElement!.className).toBe("md:hidden");
});

it("keeps the ordinary wizard view switch in the desktop header only", () => {
  render(<SetupWizard />);
  const link = screen.getByRole("link", { name: "Workstreams" });
  expect(
    within(screen.getByRole("banner")).getByRole("link", {
      name: "Workstreams",
    }),
  ).toBe(link);
  expect(link.closest(".md\\:hidden")).toBeNull();
  expect(link.querySelector("svg.lucide-arrow-left")).toBeNull();
});

it("disables both rail and mobile return controls while a write is pending", async () => {
  mocks.searchParams = new URLSearchParams("from=workstreams");
  mocks.update.mockReturnValue(new Promise(() => {}));
  render(<SetupWizard />);
  fireEvent.click(screen.getByRole("button", { name: "Complete" }));
  await waitFor(() => expect(mocks.update).toHaveBeenCalledOnce());
  expect(screen.queryByRole("link", { name: "Workstreams" })).toBeNull();
  const controls = screen.getAllByRole("button", { name: "Workstreams" });
  expect(controls).toHaveLength(2);
  const progress = screen.getByText("1 of 3 required tasks complete");
  const railReturn = within(progress.parentElement!).getByRole("button", {
    name: "Workstreams",
  });
  expect(railReturn.classList.contains("px-3")).toBe(true);
  expect(railReturn.classList.contains("-ms-3")).toBe(true);
  for (const control of controls)
    expect(control.hasAttribute("disabled")).toBe(true);
});

function RouterLocation() {
  const location = useLocation();
  return (
    <output data-testid="router-location">
      {location.pathname + location.search + location.hash}
    </output>
  );
}

function renderNavigation(path: string) {
  mocks.realRouter = true;
  return renderView(
    <MemoryRouter initialEntries={[path]}>
      <RouterLocation />
      <Routes>
        <Route path="/:orgSlug/setup/wizard" element={<SetupWizard />} />
        <Route path="/:orgSlug/setup" element={<p>Workstreams board</p>} />
        <Route path="/:orgSlug" element={<p>Dashboard</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("composed wizard navigation", () => {
  it.each([false, true])(
    "returns board-origin completion to Workstreams (verified: %s)",
    async (verified) => {
      mocks.setupQuery.mockReturnValue(
        loaded(tasks.map((task) => ({ ...task, completedByFact: verified }))),
      );
      renderNavigation(
        "/org/setup/wizard?task=anthropic-observability&step=confirm-traffic&from=workstreams&projectSlug=selected&filter=mine#details",
      );
      fireEvent.click(screen.getByRole("button", { name: "Complete" }));
      await screen.findByText("Workstreams board");
      expect(screen.getByTestId("router-location").textContent).toBe(
        "/org/setup?projectSlug=selected&filter=mine#details",
      );
      expect(mocks.update).toHaveBeenCalledTimes(verified ? 0 : 1);
    },
  );

  it("advances standalone completion without losing project context or hash", async () => {
    renderNavigation(
      "/org/setup/wizard?task=anthropic-observability&step=confirm-traffic&projectSlug=selected&filter=mine#details",
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await screen.findByText("Content for instrument-agents");
    expect(screen.getByTestId("router-location").textContent).toBe(
      "/org/setup/wizard?task=other-platforms&projectSlug=selected&filter=mine#details",
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await screen.findByText("Dashboard");
    expect(screen.getByTestId("router-location").textContent).toBe(
      "/org?projectSlug=selected&filter=mine#details",
    );
  });

  it("keeps a failed board-origin completion in its task", async () => {
    mocks.update.mockRejectedValue(new Error("Save failed"));
    const path =
      "/org/setup/wizard?task=anthropic-observability&from=workstreams&projectSlug=selected#details";
    renderNavigation(path);
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() =>
      expect(mocks.toastError).toHaveBeenCalledWith("Save failed"),
    );
    expect(screen.getByTestId("router-location").textContent).toBe(path);
  });

  it.each(["", "&from=other", "&from=workstreams"])(
    "Workstreams leaves the selected task even with provenance %s",
    (from) => {
      renderNavigation(
        `/org/setup/wizard?task=idp&step=connect&projectSlug=selected&filter=mine${from}#details`,
      );
      fireEvent.click(screen.getAllByRole("link", { name: "Workstreams" })[0]!);
      expect(screen.getByText("Workstreams board")).toBeTruthy();
      expect(screen.getByTestId("router-location").textContent).toBe(
        "/org/setup?projectSlug=selected&filter=mine#details",
      );
    },
  );

  it("lets a platform admin inspect an explicitly selected hidden task outside the walk", async () => {
    mocks.platformAdmin = true;
    mocks.setupQuery.mockReturnValue(
      loaded(tasks.map((task, index) => ({ ...task, hidden: index === 1 }))),
    );
    renderNavigation(
      "/org/setup/wizard?task=anthropic-observability&projectSlug=selected#details",
    );
    expect(screen.getByText("Inspecting a hidden task")).toBeTruthy();
    expect(rail().querySelector("[aria-current=step]")).toBeNull();
    expect(
      screen.getByText("Content for anthropic-observability"),
    ).toBeTruthy();
    expect(
      within(rail()).queryByRole("button", {
        name: /Set up Anthropic observability/,
      }),
    ).toBeNull();
    expect(mocks.setupQuery).toHaveBeenCalledWith("client", "org-one", true, {
      retry: false,
    });
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await screen.findByText("Workstreams board");
    expect(screen.getByTestId("router-location").textContent).toBe(
      "/org/setup?projectSlug=selected#details",
    );
  });

  it("excludes hidden tasks from an admin's normal walk", () => {
    mocks.platformAdmin = true;
    mocks.setupQuery.mockReturnValue(
      loaded(tasks.map((task, index) => ({ ...task, hidden: index === 1 }))),
    );
    renderNavigation("/org/setup/wizard");
    expect(screen.getByText("Content for instrument-agents")).toBeTruthy();
    expect(
      screen.queryByText("Content for anthropic-observability"),
    ).toBeNull();
  });

  it.each(["anthropic-observability", "not-real", ""])(
    "never reveals a hidden task or falls back for non-platform-admin selector %s",
    (selector) => {
      mocks.setupQuery.mockReturnValue(
        loaded(tasks.map((task, index) => ({ ...task, hidden: index === 1 }))),
      );
      renderNavigation(`/org/setup/wizard?task=${selector}`);
      expect(screen.getByText("Setup task unavailable")).toBeTruthy();
      expect(
        screen.queryByText("Content for anthropic-observability"),
      ).toBeNull();
      expect(screen.queryByText("Content for instrument-agents")).toBeNull();
      expect(screen.queryByText("Inspecting a hidden task")).toBeNull();
      expect(mocks.setupQuery).toHaveBeenCalledWith(
        "client",
        "org-one",
        false,
        { retry: false },
      );
    },
  );
});

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, isLoading: false, error: null }),
}));
vi.mock("@/hooks/useOrgSetupStarted", () => ({
  useOrgSetupStarted: () => ({ markSetupStarted: () => {} }),
}));
vi.mock("./components/board/workstream-column", () => ({
  WorkstreamColumn: () => null,
}));

it("composes legacy task, board redirect, wizard and return without losing hash or reopening the task", async () => {
  mocks.realRouter = true;
  renderView(
    <MemoryRouter
      initialEntries={[
        "/org/setup/anthropic-observability?step=confirm-traffic&projectSlug=selected&filter=mine#details",
      ]}
    >
      <RouterLocation />
      <Routes>
        <Route path="/:orgSlug/setup/:taskSlug" element={<SetupTaskPage />} />
        <Route path="/:orgSlug/setup" element={<OnboardingBoard />} />
        <Route path="/:orgSlug/setup/wizard" element={<SetupWizard />} />
      </Routes>
    </MemoryRouter>,
  );
  await screen.findByText("Content for anthropic-observability");
  expect(screen.getByTestId("router-location").textContent).toBe(
    "/org/setup/wizard?step=confirm-traffic&projectSlug=selected&filter=mine&task=anthropic-observability#details",
  );
  fireEvent.click(screen.getAllByRole("link", { name: "Workstreams" })[0]!);
  await screen.findByText("Onboarding");
  expect(screen.queryByText("Content for anthropic-observability")).toBeNull();
  expect(screen.getByTestId("router-location").textContent).toBe(
    "/org/setup?projectSlug=selected&filter=mine#details",
  );
});

vi.mock("@/components/page-layout", () => {
  const Container = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  return {
    Page: {
      Toolbar: Object.assign(Container, {
        Leading: Container,
        Actions: Container,
      }),
    },
  };
});
vi.mock("./components/board/task-card", () => ({ TaskCard: () => null }));

describe("board and wizard parity", () => {
  // The task array order deliberately disagrees with workstream membership.
  const unordered = [
    task("instrument-agents", "Set up observability in other platforms"),
    task("platform-mcp", "Set up Platform MCP", "done"),
    task("identity-provider", "Set up identity provider", "done"),
    task("anthropic-observability", "Set up Anthropic observability"),
  ].map((item) =>
    item.key === "platform-mcp"
      ? { ...item, countsTowardProgress: false }
      : item,
  );
  const workstreams = [
    { id: "connect", title: "Connect", taskKeys: ["identity-provider"] },
    {
      id: "observe",
      title: "Observe",
      taskKeys: ["anthropic-observability", "instrument-agents"],
    },
    { id: "distribute", title: "Gateway", taskKeys: ["platform-mcp"] },
  ];

  it("walks tasks in API workstream order, not task-array order", () => {
    mocks.setupQuery.mockReturnValue(loaded(unordered, workstreams));
    render(<SetupWizard />);
    const text = rail().textContent ?? "";
    const positions = [
      "Set up identity provider",
      "Set up Anthropic observability",
      "Set up observability in other platforms",
      "Set up Platform MCP",
    ].map((title) => text.indexOf(title));
    expect(positions.every((position) => position >= 0)).toBe(true);
    expect([...positions].sort((a, b) => a - b)).toEqual(positions);
    // First open task by workstream order, not the array's first entry.
    expect(
      screen.getByText("Content for anthropic-observability"),
    ).toBeTruthy();
  });

  it("excludes optional tasks from progress, exactly like the board", () => {
    mocks.setupQuery.mockReturnValue(loaded(unordered, workstreams));
    render(<SetupWizard />);
    expect(screen.getByText("1 of 3 required tasks complete")).toBeTruthy();
  });

  it("advances when the save committed but the refresh failed", async () => {
    mocks.invalidate.mockRejectedValueOnce(new Error("Offline"));
    render(<SetupWizard />);
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() =>
      expect(lastParams().get("task")).toBe("other-platforms"),
    );
    expect(mocks.toastWarning).toHaveBeenCalledOnce();
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("refuses a blocked completion without calling the server", async () => {
    mocks.setupQuery.mockReturnValue(
      loaded(
        tasks.map((item) =>
          item.key === "anthropic-observability"
            ? { ...item, blockedBy: ["identity-provider"] }
            : item,
        ),
      ),
    );
    render(<SetupWizard />);
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() => expect(mocks.toastError).toHaveBeenCalledOnce());
    expect(mocks.update).not.toHaveBeenCalled();
    expect(mocks.navigate).not.toHaveBeenCalled();
  });
});
