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
import SetupWizard from "./SetupWizard";
import { StepSection } from "./components/step-section";

const mocks = vi.hoisted(() => ({
  setupQuery: vi.fn(),
  update: vi.fn(),
  updatePending: false,
  invalidate: vi.fn(),
  goToBoard: vi.fn(),
  showPylonChat: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  navigate: vi.fn(),
  searchParams: new URLSearchParams(),
  setSearchParams: vi.fn(),
}));

vi.mock("react-router", () => ({
  useParams: () => ({ orgSlug: "org" }),
  useSearchParams: () => [mocks.searchParams, mocks.setSearchParams],
  useNavigate: () => mocks.navigate,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setup: { goTo: mocks.goToBoard, href: () => "/org/setup" },
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
  useOrganizationSetupTasks: (...args: unknown[]) => mocks.setupQuery(...args),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
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
    blockedBy: [],
    hidden: false,
  };
}

const tasks: SetupTask[] = [
  task("identity-provider", "Set up identity provider", "done"),
  task("anthropic-observability", "Set up Anthropic observability"),
  task("instrument-agents", "Set up observability in other platforms"),
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

/** The ?task= / ?step= the last navigation would have written. */
function lastParams(): URLSearchParams {
  const calls = mocks.setSearchParams.mock.calls;
  const [updater] = calls[calls.length - 1]!;
  return updater(new URLSearchParams("step=confirm-traffic"));
}

afterEach(cleanup);
beforeEach(() => {
  mocks.updatePending = false;
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

describe("SetupWizard", () => {
  it("lists every card in the rail and opens on the first one still open", () => {
    render(<SetupWizard />);

    expect(rail().textContent).toContain("Set up identity provider");
    expect(rail().textContent).toContain("Set up Anthropic observability");
    expect(rail().textContent).toContain(
      "Set up observability in other platforms",
    );
    expect(screen.getByText("1 of 3 tasks complete")).toBeTruthy();
    expect(
      screen.getByText("Content for anthropic-observability"),
    ).toBeTruthy();
  });

  it("walks the board's default list, without hidden cards", () => {
    render(<SetupWizard />);

    expect(mocks.setupQuery).toHaveBeenCalledWith("org-one", false, {
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

  it("falls back to the first open card when ?task= names nothing here", () => {
    mocks.searchParams = new URLSearchParams("task=no-such-card");
    render(<SetupWizard />);

    expect(
      screen.getByText("Content for anthropic-observability"),
    ).toBeTruthy();
  });

  it("lands on the last card once every card is done", () => {
    mocks.setupQuery.mockReturnValue(
      loaded(tasks.map((t) => ({ ...t, status: "done" }))),
    );
    render(<SetupWizard />);

    expect(screen.getByText("Content for instrument-agents")).toBeTruthy();
    expect(screen.getByText("3 of 3 tasks complete")).toBeTruthy();
  });

  it("moves between cards from the rail, dropping the outgoing card's step", () => {
    render(<SetupWizard />);

    fireEvent.click(
      screen.getByRole("button", { name: /Set up identity provider/ }),
    );

    const params = lastParams();
    expect(params.get("task")).toBe("idp");
    expect(params.get("step")).toBeNull();
    const [, options] = mocks.setSearchParams.mock.calls.at(-1)!;
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
    expect(mocks.navigate).toHaveBeenCalledWith("/org");
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

    await waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith("/org"));
  });

  it("holds the reader's own moves while a completion is settling", () => {
    mocks.updatePending = true;
    render(<SetupWizard />);

    // Completing advances once its mutation lands; a move made in that
    // window would be overwritten a moment later, so none is taken.
    const previous = screen.getByRole("button", { name: "Previous task" });
    const skip = screen.getByRole("button", { name: "Skip task" });
    expect(previous.hasAttribute("disabled")).toBe(true);
    expect(skip.hasAttribute("disabled")).toBe(true);
    fireEvent.click(previous);
    fireEvent.click(skip);
    fireEvent.click(
      screen.getByRole("button", { name: /Set up identity provider/ }),
    );

    expect(mocks.setSearchParams).not.toHaveBeenCalled();
  });

  it("stays put when completing fails", async () => {
    mocks.update.mockRejectedValueOnce(new Error("nope"));
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Complete" }));

    await waitFor(() => expect(mocks.toastError).toHaveBeenCalledWith("nope"));
    expect(mocks.setSearchParams).not.toHaveBeenCalled();
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
    expect(mocks.setSearchParams).not.toHaveBeenCalled();
  });

  it("points at the board when every card is hidden", () => {
    mocks.setupQuery.mockReturnValue(loaded([]));
    render(<SetupWizard />);

    expect(screen.getByText("Nothing to set up")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Setup board" }));
    expect(mocks.goToBoard).toHaveBeenCalledOnce();
  });

  it("offers a retry when the list fails to load", () => {
    const refetch = vi.fn();
    mocks.setupQuery.mockReturnValue({
      data: undefined,
      isPending: false,
      isSuccess: false,
      isError: true,
      refetch,
    });
    render(<SetupWizard />);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalledOnce();
  });
});
