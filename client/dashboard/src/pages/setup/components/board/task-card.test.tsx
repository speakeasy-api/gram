import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { Action } from "@/components/ui/MoreActions";
import type { BoardTask } from "./board-store";
import { ONBOARDING_TASKS } from "./tasks";
import { TaskCard } from "./task-card";
import userEvent from "@testing-library/user-event";

vi.mock("./assignee-picker", () => ({ AssigneePicker: () => null }));
vi.mock("@/components/ui/MoreActions", () => ({
  MoreActions: ({ actions }: { actions: Action[] }) => (
    <div data-testid="task-menu">
      {actions.map((action) => (
        <button
          key={action.label}
          disabled={action.disabled}
          onClick={action.onClick}
        >
          {action.label}
          {action.description && <span>{action.description}</span>}
        </button>
      ))}
    </div>
  ),
}));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function renderCard(
  overrides: Partial<BoardTask> = {},
  isPending = false,
  canSetStatus = true,
  canHide = false,
  canOpen = true,
) {
  const onOpen = vi.fn<() => void>();
  const onGoToTask = vi.fn<(id: string) => void>();
  const onToggleHidden = vi.fn<() => void>();
  const task: BoardTask = {
    ...ONBOARDING_TASKS[0]!,
    title: "Task",
    description: "Task description",
    blockedBy: [],
    status: "todo",
    verified: false,
    hidden: false,
    assignee: { kind: "email", email: "owner@example.com" },
    ...overrides,
  };
  render(
    <TaskCard
      task={task}
      canOpen={canOpen}
      canHide={canHide}
      isPending={isPending}

      canSetStatus={canSetStatus}
      onOpen={onOpen}
      onGoToTask={onGoToTask}
      reachableTaskIds={ONBOARDING_TASKS.map((item) => item.id)}
      onSetStatus={vi.fn<() => void>()}

      onToggleHidden={onToggleHidden}
    />,
  );
  return { onOpen, onToggleHidden, onGoToTask };
}

function mockDescriptionSize() {
  const size = { width: 200, height: 20, scrollWidth: 200, scrollHeight: 40 };
  for (const [property, key] of [
    ["clientWidth", "width"],
    ["clientHeight", "height"],
    ["scrollWidth", "scrollWidth"],
    ["scrollHeight", "scrollHeight"],
  ] as const) {
    vi.spyOn(HTMLElement.prototype, property, "get").mockImplementation(
      function (this: HTMLElement) {
        return this.tagName === "P" ? size[key] : 0;
      },
    );
  }
  return size;
}

describe("TaskCard controls", () => {
  it.each([true, false])(
    "only adds a non-interactive crosshatch when blocked (%s)",
    (blocked) => {
      renderCard({ blockedBy: blocked ? ["instrument-agents"] : [] });
      const pattern = screen
        .getByRole("article")
        .querySelector('div[aria-hidden="true"]');
      if (blocked) {
        expect(pattern).not.toBeNull();
        expect(pattern!.classList.contains("pointer-events-none")).toBe(true);
        expect(pattern!.classList.contains("opacity-[0.06]")).toBe(true);
        expect((pattern as HTMLElement).style.backgroundImage).toContain(
          "repeating-linear-gradient",
        );
      } else {
        expect(pattern).toBeNull();
      }
    },
  );
  it.each(["Engineering Lead", "IT Admin"])(
    "does not show the suggested owner %s on individual cards",
    (suggestedOwner) => {
      renderCard({ suggestedOwner });
      expect(screen.queryByText(suggestedOwner, { exact: false })).toBeNull();
      expect(screen.getByRole("heading", { name: "Task" })).toBeTruthy();
      expect(screen.getByTestId("task-menu")).toBeTruthy();
    },
  );
  it("opens the task when its card activation button is clicked", () => {
    const { onOpen } = renderCard();
    fireEvent.click(screen.getByRole("button", { name: "Task, To Do" }));
    expect(onOpen).toHaveBeenCalledOnce();
  });
  it.each([true, false])(
    "does not show completion progress on cards (verified: %s)",
    (verified) => {
      renderCard({ verified, status: "done" });
      expect(screen.queryByText("Done")).toBeNull();
      expect(screen.queryByText("Verified")).toBeNull();
      expect(screen.getByRole("button", { name: "Task, Done" })).toBeTruthy();
      expect(screen.queryByRole("progressbar")).toBeNull();
    },
  );
  it("does not show an individual status footer", () => {
    renderCard({ status: "in_progress" });
    expect(screen.queryByText("In Progress")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Task, In Progress" }),
    ).toBeTruthy();
    expect(screen.queryByText("Requires")).toBeNull();
  });
  it("opens an interactive prerequisite list on hover and navigates to the selected task", async () => {
    const { onOpen, onGoToTask } = renderCard({
      blockedBy: ["instrument-agents", "create-marketplace", "future-task"],
    });
    expect(
      screen.getByText(
        "Requires: Set up observability in other platforms, Create marketplace, Future task",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("Task description")).toBeNull();
    const activation = screen.getByRole("button", { name: "Task, To Do" });
    await userEvent.hover(activation);
    const popover = await screen.findByRole("dialog", {
      name: "Required tasks",
    });
    expect(document.activeElement).not.toBe(
      within(popover).getAllByRole("button")[0],
    );
    expect(within(popover).getAllByRole("listitem")).toHaveLength(3);
    expect(
      within(popover).getByText(/Not available on this board/),
    ).toBeTruthy();
    const goToTask = within(popover).getByRole("button", {
      name: "Go to task: Set up observability in other platforms",
    });
    await userEvent.hover(goToTask);
    await userEvent.click(goToTask);
    expect(onGoToTask).toHaveBeenCalledWith("instrument-agents");
    expect(onOpen).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("opens prerequisites from the keyboard and returns focus on Escape", async () => {
    const { onOpen, onGoToTask } = renderCard({
      blockedBy: ["instrument-agents"],
    });
    const activation = screen.getByRole("button", { name: "Task, To Do" });
    expect(activation.getAttribute("aria-haspopup")).toBe("dialog");
    await userEvent.tab();
    await userEvent.keyboard("{Enter}");
    const popover = await screen.findByRole("dialog", {
      name: "Required tasks",
    });
    expect(document.activeElement).toBe(within(popover).getByRole("button"));
    await userEvent.keyboard("{Escape}");
    expect(document.activeElement).toBe(activation);
    await userEvent.keyboard(" ");
    await userEvent.keyboard("{Enter}");
    expect(onGoToTask).toHaveBeenCalledWith("instrument-agents");
    expect(onOpen).not.toHaveBeenCalled();
  });
  it("shows the full truncated description on hover and keyboard focus without blocking opening", async () => {
    mockDescriptionSize();
    const { onOpen } = renderCard();
    const activation = screen.getByRole("button", { name: "Task, To Do" });
    await userEvent.hover(activation);
    expect((await screen.findByRole("tooltip")).textContent).toBe(
      "Task description",
    );
    await userEvent.unhover(activation);
    await userEvent.tab();
    expect((await screen.findByRole("tooltip")).textContent).toBe(
      "Task description",
    );
    expect(activation.getAttribute("aria-describedby")).toBeTruthy();
    await userEvent.keyboard("{Enter}");
    expect(onOpen).toHaveBeenCalledOnce();
    await userEvent.click(activation);
    expect(onOpen).toHaveBeenCalledTimes(2);
  });
  it("only shows description tooltips while truncated and reacts to resizing", async () => {
    const size = mockDescriptionSize();
    size.scrollHeight = 20;
    let resize = () => {};
    const disconnect = vi.fn();
    vi.stubGlobal(
      "ResizeObserver",
      class {
        callback: () => void;
        constructor(callback: () => void) {
          this.callback = callback;
        }
        observe(element: Element) {
          if (element.tagName === "P") resize = this.callback;
        }
        unobserve() {}
        disconnect = disconnect;
      },
    );
    renderCard();
    const activation = screen.getByRole("button", { name: "Task, To Do" });
    await userEvent.hover(activation);
    expect(screen.queryByRole("tooltip")).toBeNull();
    await userEvent.unhover(activation);
    await userEvent.tab();
    expect(screen.queryByRole("tooltip")).toBeNull();
    size.scrollWidth = 300;
    act(() => resize());
    await userEvent.hover(activation);
    expect(await screen.findByRole("tooltip")).toBeTruthy();
    size.scrollWidth = 200;
    act(() => resize());
    expect(screen.queryByRole("tooltip")).toBeNull();
    size.scrollHeight = 40;
    fireEvent(window, new Event("resize"));
    await userEvent.unhover(activation);
    await userEvent.hover(activation);
    expect(await screen.findByRole("tooltip")).toBeTruthy();
    cleanup();
    expect(disconnect).toHaveBeenCalled();
  });
  it("prioritizes prerequisites over truncated descriptions for blocked cards", async () => {
    mockDescriptionSize();
    renderCard({ blockedBy: ["instrument-agents"] });
    await userEvent.tab();
    await userEvent.keyboard("{Enter}");
    const popover = await screen.findByRole("dialog", {
      name: "Required tasks",
    });
    expect(within(popover).queryByText("Complete first")).toBeNull();
    expect(
      within(popover).getByRole("button", {
        name: "Go to task: Set up observability in other platforms",
      }),
    ).toBeTruthy();
    expect(screen.queryByText("Task description")).toBeNull();
  });
  it("retains hide management for blocked cards", async () => {
    const { onToggleHidden, onOpen } = renderCard(
      { blockedBy: ["instrument-agents"] },
      false,
      true,
      true,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Hide from board" }),
    );
    expect(onToggleHidden).toHaveBeenCalledOnce();
    expect(onOpen).not.toHaveBeenCalled();
  });
  it("removes reminders and retains native keyboard opening", async () => {
    const { onOpen } = renderCard();
    expect(screen.queryByText(/remind/i)).toBeNull();
    screen.getByRole("button", { name: "Task, To Do" }).focus();
    await userEvent.keyboard("{Enter} ");
    expect(onOpen).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
  it("keeps menu controls outside the card activation button", () => {
    const { onOpen } = renderCard();
    const activation = screen.getByRole("button", { name: "Task, To Do" });
    expect(activation.querySelector("button")).toBeNull();
    fireEvent.click(screen.getByText("Move to Done"));
    expect(onOpen).not.toHaveBeenCalled();
  });
  it("hides unauthorized transitions", () => {
    renderCard({}, false, false);
    expect(screen.queryByText("Move to Done")).toBeNull();
  });
  it("locks fact completion", () => {
    renderCard({ verified: true, status: "done" });
    expect(screen.queryByText("Move to To Do")).toBeNull();
  });
  it("disables blocked transitions", () => {
    renderCard({ blockedBy: ["instrument-agents"] });
    expect(
      (screen.getByText("Move to Done") as HTMLButtonElement).disabled,
    ).toBe(true);
  });
  it("disables transitions while saving", () => {
    renderCard({}, true);
    expect(
      (
        screen.getByRole("button", {
          name: "Move to Done",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});

describe("card parity", () => {
  it.each([
    ["todo", "To Do"],
    ["in_progress", "In Progress"],
    ["awaiting_support", "Awaiting Support"],
    ["done", "Done"],
  ] as const)("shows a compact visible %s signal", (status, label) => {
    renderCard({ status });
    const signal = screen.getByLabelText(`Status: ${label}`);
    expect(signal.getAttribute("title")).toBe(label);
    expect(signal.querySelector("svg")).not.toBeNull();
    expect(signal.className).not.toContain("sr-only");
  });
  it("distinguishes verified completion", () => {
    renderCard({ verified: true, status: "done" });
    expect(
      screen.getByLabelText("Status: Done, verified").getAttribute("title"),
    ).toBe("Done · Verified");
  });
  it("lets admins inspect blocked tasks without enabling advancement", () => {
    const { onOpen } = renderCard({ blockedBy: ["instrument-agents"] });
    fireEvent.click(screen.getByRole("button", { name: "Task, To Do" }));
    expect(onOpen).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Open task" }));
    expect(onOpen).toHaveBeenCalledOnce();
    expect(
      (
        screen.getByRole("button", {
          name: "Move to In Progress",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
  it("explains admin-required opening without removing assigned status controls", () => {
    const { onOpen } = renderCard({}, false, true, false, false);
    fireEvent.click(screen.getByRole("button", { name: "Task, To Do" }));
    expect(onOpen).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog").textContent).toContain(
      "An organization admin is required",
    );
    expect(
      (screen.getByRole("button", { name: /Open task/ }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      (
        screen.getByRole("button", {
          name: "Move to In Progress",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
  });
});
