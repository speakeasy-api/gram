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
) {
  const onOpen = vi.fn<() => void>();
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
      canHide={canHide}
      isPending={isPending}

      canSetStatus={canSetStatus}
      onOpen={onOpen}
      onSetStatus={vi.fn<() => void>()}

      onToggleHidden={onToggleHidden}
    />,
  );
  return { onOpen, onToggleHidden };
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
  it("shows required task titles only in an orange outlined hover tooltip", async () => {
    renderCard({
      blockedBy: ["instrument-agents", "create-marketplace", "future-task"],
    });
    expect(screen.queryByText("Requires")).toBeNull();
    const activation = screen.getByRole("button", { name: "Task, To Do" });
    await userEvent.hover(activation);
    const tooltip = await screen.findByRole("tooltip");
    const dependencies = within(tooltip).getByRole("list", {
      name: "Prerequisite tasks",
    });
    expect(within(dependencies).getAllByRole("listitem")).toHaveLength(3);
    expect(
      within(dependencies).getByText("Set up observability in other platforms"),
    ).toBeTruthy();
    expect(within(dependencies).getByText("Create marketplace")).toBeTruthy();
    expect(within(dependencies).getByText("Future task")).toBeTruthy();
    const content = document.querySelector('[data-slot="tooltip-content"]')!;
    expect(content.classList.contains("border-warning-default")).toBe(true);
    expect(content.classList.contains("pointer-events-none")).toBe(true);
    expect(
      activation
        .closest("article")!
        .querySelector('[aria-hidden="true"]')!
        .classList.contains("pointer-events-none"),
    ).toBe(true);
    await userEvent.unhover(activation);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
  it("keeps blocked cards focusable for prerequisites but prevents mouse and keyboard activation", async () => {
    const { onOpen } = renderCard({ blockedBy: ["instrument-agents"] });
    const activation = screen.getByRole("button", { name: "Task, To Do" });
    expect(activation.getAttribute("aria-disabled")).toBe("true");
    expect((activation as HTMLButtonElement).disabled).toBe(false);
    await userEvent.tab();
    expect(document.activeElement).toBe(activation);
    expect(await screen.findByRole("tooltip")).toBeTruthy();
    expect(activation.getAttribute("aria-describedby")).toBeTruthy();
    await userEvent.keyboard("{Enter} ");
    await userEvent.click(activation);
    await userEvent.click(screen.getByRole("button", { name: "Open task" }));
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
    const tooltip = await screen.findByRole("tooltip");
    expect(within(tooltip).getByText("Requires")).toBeTruthy();
    expect(within(tooltip).queryByText("Task description")).toBeNull();
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
