import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useOrgRoutes } from "@/routes";
import { Suspense } from "react";
import type { BoardTask } from "./board-store";
import { TaskDialog } from "./task-dialog";
import { ONBOARDING_TASKS, type OnboardingTaskId } from "./tasks";
import { StepContainer, StepSupportButton } from "../step-container";
import { StepSection } from "../step-section";
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
  TaskStep: ({
    onComplete,
    taskId,
  }: {
    onComplete: () => void;
    taskId: string;
  }) =>
    taskId === "anthropic-observability" ? (
      <StepContainer
        icon={null}
        title="Inline task content"
        description="Inference journey"
        onContinue={onComplete}
      >
        <StepSection index={1} slug="hook" title="Configure hook">
          Hook configuration
        </StepSection>
        <StepSection index={2} slug="traffic" title="Confirm traffic">
          Traffic confirmation
        </StepSection>
      </StepContainer>
    ) : (
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

function renderTask(
  id: OnboardingTaskId,
  projectSlug?: string,
  overrides: Partial<BoardTask> = {},
) {
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
          ...overrides,
        }}
        projectSlug={projectSlug}

        canSetStatus={true}
        isPending={false}
        error={null}
        onClose={vi.fn<() => void>()}
        onSetStatus={vi.fn<() => Promise<boolean>>().mockResolvedValue(true)}
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
  it("walks the inference journey and offers desktop navigation back", () => {
    renderTask("anthropic-observability");
    expect(
      screen.getByText("Hook configuration").closest("section")?.hidden,
    ).toBe(false);
    expect(
      screen.getByText("Traffic confirmation").closest("section")?.hidden,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Next step" }));
    expect(
      screen.getByText("Hook configuration").closest("section")?.hidden,
    ).toBe(true);
    expect(
      screen.getByText("Traffic confirmation").closest("section")?.hidden,
    ).toBe(false);
    expect(screen.getByTestId("location").textContent).toContain(
      "step=traffic",
    );
    fireEvent.click(screen.getByRole("button", { name: "1. Configure hook" }));
    expect(
      screen.getByText("Hook configuration").closest("section")?.hidden,
    ).toBe(false);
  });
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

          canSetStatus={false}
          isPending={false}
          error={null}
          onClose={onClose}
          onSetStatus={onSetStatus}
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

            canSetStatus={canSetStatus}
            isPending={false}
            error={null}
            onClose={vi.fn<() => void>()}
            onSetStatus={onSetStatus}
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

          canSetStatus
          isPending={false}
          error="Save failed"
          onClose={onClose}
          onSetStatus={onSetStatus}
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

            canSetStatus
            isPending={false}
            error={null}
            onClose={vi.fn<() => void>()}
            onSetStatus={onSetStatus}
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
  it("retains the legacy task entry in the actual route definition", async () => {
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

describe("TaskDialog presentation", () => {
  it.each([true, false])(
    "shows a single completed state (verified: %s)",
    (verified) => {
      renderTask("create-marketplace", undefined, { status: "done", verified });
      expect(screen.queryByText("Verified")).toBeNull();
      expect(screen.getAllByText("Done")).toHaveLength(1);
      expect(
        (screen.getByRole("combobox", { name: "Status" }) as HTMLButtonElement)
          .disabled,
      ).toBe(verified);
      expect(
        screen.queryByRole("list", { name: "Prerequisite tasks" }),
      ).toBeNull();
    },
  );

  it("shows named prerequisites rather than a joined warning", () => {
    renderTask("confirm-traffic", undefined, {
      blockedBy: ["instrument-agents", "create-marketplace"],
    });
    const dependencies = screen.getByRole("list", {
      name: "Prerequisite tasks",
    });
    expect(within(dependencies).getAllByRole("listitem")).toHaveLength(2);
    expect(
      within(dependencies).getByText("Set up observability in other platforms"),
    ).toBeTruthy();
    expect(within(dependencies).getByText("Create marketplace")).toBeTruthy();
    const callout = dependencies.parentElement!.parentElement!;
    expect(callout.classList.contains("text-default-warning")).toBe(true);
    expect(callout.className).not.toMatch(/(?:^| )(?:bg-|border|p[xy]-)/);
    expect(dependencies.classList.contains("min-w-0")).toBe(true);
    for (const item of within(dependencies).getAllByRole("listitem")) {
      expect(item.classList.contains("max-w-full")).toBe(true);
      expect(item.getAttribute("title")).toBe(item.textContent);
      expect(item.classList.contains("truncate")).toBe(true);
      expect(item.classList.contains("border")).toBe(true);
      expect(item.classList.contains("bg-warning-softest")).toBe(true);
      expect(item.classList.contains("border-warning-softest")).toBe(true);
    }
    expect(screen.queryByText(/Blocked by:/)).toBeNull();
    expect(screen.queryByText("instrument-agents")).toBeNull();
  });
});
