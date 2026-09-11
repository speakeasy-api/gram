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
  navigate: vi.fn(),
  searchParams: new URLSearchParams(),
  setSearchParams: vi.fn(),
}));

vi.mock("react-router", () => ({
  useParams: () => ({ taskSlug: mocks.taskSlug }),
  useSearchParams: () => [mocks.searchParams, mocks.setSearchParams],
  useNavigate: () => mocks.navigate,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setup: {
      goTo: mocks.goToBoard,
      href: () => "/org/setup",
      Link: ({
        children,
        className,
      }: {
        children: ReactNode;
        className?: string;
      }) => <a className={className}>{children}</a>,
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
    onSupport,
  }: {
    taskKey: string;
    onComplete: () => void;
    onSupport: () => void;
  }) => (
    <div>
      <p>Content for {taskKey}</p>
      {/* Only this card registers sub-steps, so the others exercise the
          rail's single-row fallback. */}
      {taskKey !== "anthropic-observability" ? null : (
        <>
          <StepSection
            index={1}
            slug="publish-marketplace"
            title="Publish plugin marketplace"
            complete={mocks.marketplacePublished}
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
});

describe("SetupTaskPage", () => {
  it("opens support for verified tasks without changing their status", async () => {
    mocks.taskSlug = "idp";
    render(<SetupTaskPage />);
    fireEvent.click(screen.getByRole("button", { name: "Get support" }));
    await waitFor(() => expect(mocks.showPylonChat).toHaveBeenCalledOnce());
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("returns from verified tasks without rewriting their completion", async () => {
    mocks.taskSlug = "idp";
    render(<SetupTaskPage />);
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() => expect(mocks.goToBoard).toHaveBeenCalledOnce());
    expect(mocks.update).not.toHaveBeenCalled();
  });

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

  it("shows one step at a time and moves between them from the rail", () => {
    render(<SetupTaskPage />);

    // The first open step is on screen; the rest are mounted but hidden.
    expect(
      screen.getByText("marketplace body").closest("section")?.hidden,
    ).toBe(false);
    expect(screen.getByText("traffic body").closest("section")?.hidden).toBe(
      true,
    );

    fireEvent.click(screen.getByRole("button", { name: /Confirm traffic/ }));

    expect(
      screen.getByText("marketplace body").closest("section")?.hidden,
    ).toBe(true);
    expect(screen.getByText("traffic body").closest("section")?.hidden).toBe(
      false,
    );
  });

  it("opens on the step named by ?step=", () => {
    mocks.searchParams = new URLSearchParams("step=confirm-traffic");
    render(<SetupTaskPage />);

    expect(screen.getByText("traffic body").closest("section")?.hidden).toBe(
      false,
    );
    expect(
      screen.getByText("marketplace body").closest("section")?.hidden,
    ).toBe(true);
  });

  it("falls back to the first open step when ?step= names nothing here", () => {
    mocks.searchParams = new URLSearchParams("step=not-a-step-on-this-card");
    render(<SetupTaskPage />);

    expect(
      screen.getByText("marketplace body").closest("section")?.hidden,
    ).toBe(false);
  });

  it("rewrites ?step= as the reader walks the rail", () => {
    render(<SetupTaskPage />);

    fireEvent.click(screen.getByRole("button", { name: /Confirm traffic/ }));

    expect(mocks.setSearchParams).toHaveBeenCalled();
    const [updater, options] = mocks.setSearchParams.mock.calls[0]!;
    expect(updater(new URLSearchParams()).get("step")).toBe("confirm-traffic");
    // Back belongs to the board, not to each step passed through.
    expect(options).toEqual({ replace: true });
  });

  it("leaves a step unticked when the reader jumps past it", () => {
    render(<SetupTaskPage />);

    fireEvent.click(screen.getByRole("button", { name: /Confirm traffic/ }));

    // Jumping ahead is a preview, not progress: the marketplace step still
    // shows its number rather than a check mark.
    expect(
      screen.getByRole("button", { name: /Publish plugin marketplace/ })
        .textContent,
    ).toContain("1");
    expect(screen.getByText("0 of 2 complete")).toBeTruthy();
  });

  it("ticks a step off in the rail once its outcome lands", () => {
    const view = render(<SetupTaskPage />);
    expect(screen.getByText("0 of 2 complete")).toBeTruthy();

    mocks.marketplacePublished = true;
    view.rerender(<SetupTaskPage />);

    expect(screen.getByText("1 of 2 complete")).toBeTruthy();
  });

  it("keeps a board link reachable where the rail is hidden", () => {
    render(<SetupTaskPage />);

    // The rail holds one, and it is hidden below md, so the content column
    // carries a second that only shows there.
    const links = screen.getAllByText("Setup board");
    expect(links).toHaveLength(2);
    expect(
      links.filter((link) =>
        link.closest("a")?.className.includes("md:hidden"),
      ),
    ).toHaveLength(1);
  });

  it("reports a done task with no sub-steps as complete", () => {
    mocks.taskSlug = "idp";
    render(<SetupTaskPage />);

    // The stand-in card registers no sections for this task, so the rail
    // falls back to a single row that has to carry the task's own status.
    expect(screen.getByText("1 of 1 complete")).toBeTruthy();
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

    // Replaced, not pushed: Back must not land on the dead slug and redirect
    // again.
    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith("/org/setup", {
        replace: true,
      }),
    );
  });
});
