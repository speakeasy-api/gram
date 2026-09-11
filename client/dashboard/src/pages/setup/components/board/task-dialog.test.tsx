import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useOrgRoutes } from "@/routes";
import { Suspense } from "react";
import { TaskDialog } from "./task-dialog";
import { ONBOARDING_TASKS, type OnboardingTaskId } from "./tasks";
import { StepSupportButton } from "../step-container";
import { showPylonChat } from "@/lib/pylon";

vi.mock("@/lib/pylon", () => ({ showPylonChat: vi.fn() }));

vi.mock("../../SetupTaskPage", () => ({
  default: () => <p>Standalone guided page</p>,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "example-org", projectSlug: "default" }),
}));
vi.mock("./assignee-picker", () => ({ AssigneePicker: () => null }));
vi.mock("./task-step", () => ({
  TaskStep: ({ onComplete }: { onComplete: () => void }) => (
    <div>
      <p>Inline task content</p>
      <button onClick={onComplete}>Finish</button>
      <StepSupportButton />
    </div>
  ),
}));

afterEach(cleanup);

function RouteState() {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.pathname + location.search}
    </output>
  );
}

function renderTask(id: OnboardingTaskId, projectSlug?: string) {
  const definition = ONBOARDING_TASKS.find((task) => task.id === id)!;
  return render(
    <MemoryRouter initialEntries={["/example-org/setup"]}>
      <RouteState />
      <TaskDialog
        task={{
          ...definition,
          title: id,
          description: "Setup task",
          blockedBy: [],
          status: "todo",
          verified: false,
          hidden: false,
        }}
        projectSlug={projectSlug}
        canAssign={true}
        canSetStatus={true}
        isPending={false}
        error={null}
        onClose={vi.fn<() => void>()}
        onSetStatus={vi.fn<() => Promise<boolean>>().mockResolvedValue(true)}
        onAssign={vi.fn<() => void>()}
      />
    </MemoryRouter>,
  );
}

function GuidedRoute() {
  const routes = useOrgRoutes();
  const Page = routes.setupTask.component!;
  return (
    <Suspense fallback={null}>
      <Page />
    </Suspense>
  );
}

describe("guided setup from the board dialog", () => {
  it("closes an already verified task without writing its status", () => {
    const onClose = vi.fn<() => void>();
    const onSetStatus = vi.fn<() => Promise<boolean>>();
    render(
      <MemoryRouter>
        <TaskDialog
          task={{
            id: "create-marketplace",
            suggestedOwner: "Admin",
            title: "Create marketplace",
            description: "Publish the marketplace",
            blockedBy: [],
            status: "done",
            verified: true,
            hidden: false,
          }}
          canAssign
          canSetStatus={false}
          isPending={false}
          error={null}
          onClose={onClose}
          onSetStatus={onSetStatus}
          onAssign={vi.fn<() => void>()}
        />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Finish" }));
    expect(onClose).toHaveBeenCalledOnce();
    expect(onSetStatus).not.toHaveBeenCalled();
  });

  it.each([true, false])(
    "opens support for a verified task with status permission %s without a mutation",
    (canSetStatus) => {
      vi.mocked(showPylonChat).mockClear();
      const onSetStatus = vi
        .fn<() => Promise<boolean>>()
        .mockResolvedValue(false);
      render(
        <MemoryRouter>
          <TaskDialog
            task={{
              id: "create-marketplace",
              suggestedOwner: "Admin",
              title: "Create marketplace",
              description: "Publish",
              blockedBy: [],
              status: "done",
              verified: true,
              hidden: false,
            }}
            canAssign
            canSetStatus={canSetStatus}
            isPending={false}
            error={null}
            onClose={vi.fn<() => void>()}
            onSetStatus={onSetStatus}
            onAssign={vi.fn<() => void>()}
          />
        </MemoryRouter>,
      );
      fireEvent.click(screen.getByRole("button", { name: "Get support" }));
      expect(showPylonChat).toHaveBeenCalledOnce();
      expect(onSetStatus).not.toHaveBeenCalled();
    },
  );

  it("keeps the dialog open on a failed write and has no reminder action", async () => {
    const onClose = vi.fn<() => void>();
    const onSetStatus = vi
      .fn<() => Promise<boolean>>()
      .mockResolvedValue(false);
    render(
      <MemoryRouter>
        <TaskDialog
          task={{
            id: "instrument-agents",
            suggestedOwner: "Admin",
            title: "Task",
            description: "Description",
            blockedBy: [],
            status: "todo",
            verified: false,
            hidden: false,
          }}
          canAssign
          canSetStatus
          isPending={false}
          error="Save failed"
          onClose={onClose}
          onSetStatus={onSetStatus}
          onAssign={vi.fn<() => void>()}
        />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Finish" }));
    await waitFor(() => expect(onSetStatus).toHaveBeenCalledOnce());
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toBe("Save failed");
    expect(screen.queryByText(/remind/i)).toBeNull();
  });
  it.each([true, false])(
    "keeps the support status write for unverified tasks (saved: %s)",
    async (saved) => {
      vi.mocked(showPylonChat).mockClear();
      const onSetStatus = vi
        .fn<() => Promise<boolean>>()
        .mockResolvedValue(saved);
      render(
        <MemoryRouter>
          <TaskDialog
            task={{
              id: "instrument-agents",
              suggestedOwner: "Admin",
              title: "Task",
              description: "Setup",
              blockedBy: [],
              status: "todo",
              verified: false,
              hidden: false,
            }}
            canAssign
            canSetStatus
            isPending={false}
            error={null}
            onClose={vi.fn<() => void>()}
            onSetStatus={onSetStatus}
            onAssign={vi.fn<() => void>()}
          />
        </MemoryRouter>,
      );
      fireEvent.click(screen.getByRole("button", { name: "Get support" }));
      await waitFor(() =>
        expect(onSetStatus).toHaveBeenCalledWith(
          "instrument-agents",
          "awaiting_support",
        ),
      );
      expect(showPylonChat).toHaveBeenCalledTimes(saved ? 1 : 0);
    },
  );
  it("retains the standalone guided page in the actual route definition", async () => {
    render(
      <MemoryRouter>
        <GuidedRoute />
      </MemoryRouter>,
    );
    expect(await screen.findByText("Standalone guided page")).toBeTruthy();
  });

  it.each(ONBOARDING_TASKS.map((task) => task.id))(
    "keeps %s in the shared dialog without a redirect loop",
    (id) => {
      renderTask(id);
      expect(
        screen.queryByRole("link", { name: "Open guided setup" }),
      ).toBeNull();
      expect(screen.getByText("Inline task content")).toBeTruthy();
    },
  );
});
