import userEvent from "@testing-library/user-event";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { WorkstreamColumn } from "./workstream-column";
import { ONBOARDING_WORKSTREAMS } from "./workstream-fixtures";
import { resolveBoardTasks } from "./board-store";

vi.mock("./assignee-picker", () => ({
  AssigneePicker: (
    props: ComponentProps<typeof import("./assignee-picker").AssigneePicker>,
  ) => (
    <button
      data-owner-breakdown={props.ownerBreakdown}
      data-bulk-assignment={props.bulkAssignment}
      disabled={props.disabled}
      onClick={() => void props.onChange(undefined)}
    >
      {props.assignee?.email ?? props.placeholder}
    </button>
  ),
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

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

function column(description = workstream.description) {
  return (
    <WorkstreamColumn
      workstream={{ ...workstream, description }}
      tasks={tasks}
      allTasks={tasks}
      canAssign
      isPending={false}
      onAssign={vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>()}
    >
      Cards
    </WorkstreamColumn>
  );
}
const workstream = ONBOARDING_WORKSTREAMS[0]!;
const tasks = resolveBoardTasks(
  workstream.taskKeys.map((key, i) => ({
    key,
    title: key,
    description: "",
    status: "todo",
    completedByFact: false,
    hidden: i > 0,
    blockedBy: [],
    assignee: i === 0 ? { email: "owner@example.test" } : undefined,
  })),
);
it("does not promote a legacy card owner to the workstream", () => {
  const assign = vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>();
  render(
    <WorkstreamColumn
      workstream={workstream}
      tasks={tasks.slice(0, 1)}
      allTasks={tasks}
      canAssign
      isPending={false}
      onAssign={assign}
    >
      Card
    </WorkstreamColumn>,
  );
  expect(assign).not.toHaveBeenCalled();
  expect(
    screen.getByRole("button", { name: "Partially assigned" }),
  ).toBeTruthy();
  expect(screen.queryByText("Mixed owners")).toBeNull();
  expect(screen.queryByText("owner@example.test")).toBeNull();
  expect(screen.queryByText("Assign workstream")).toBeNull();
});
it.each([
  { canAssign: false, isPending: false },
  { canAssign: true, isPending: true },
])("locks assignment for %o", (props) => {
  const assign = vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>();
  render(
    <WorkstreamColumn
      workstream={workstream}
      tasks={tasks}
      allTasks={tasks}
      {...props}
      onAssign={assign}
    >
      Card
    </WorkstreamColumn>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Partially assigned" }));
  expect(assign).not.toHaveBeenCalled();
});

it.each([false, true])(
  "ignores partial card assignments, including hidden owners: %s",
  (hidden) => {
    const allTasks = [
      { ...tasks[0]!, assignee: undefined, hidden: false },
      { ...tasks[1]!, assignee: tasks[0]!.assignee, hidden },
    ];
    const assign = vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>();
    const { rerender } = render(
      <WorkstreamColumn
        workstream={workstream}
        tasks={allTasks.slice(0, 1)}
        allTasks={allTasks}
        canAssign
        isPending={false}
        onAssign={assign}
      >
        Cards
      </WorkstreamColumn>,
    );
    expect(
      screen.getByRole("button", { name: "Partially assigned" }),
    ).toBeTruthy();
    rerender(
      <WorkstreamColumn
        workstream={workstream}
        tasks={[]}
        allTasks={[...allTasks].reverse()}
        canAssign
        isPending={false}
        onAssign={assign}
      >
        Cards
      </WorkstreamColumn>,
    );
    expect(
      screen.getByRole("button", { name: "Partially assigned" }),
    ).toBeTruthy();
    expect(assign).not.toHaveBeenCalled();
  },
);

it("labels mixed legacy card owners explicitly", () => {
  const allTasks = [
    { ...tasks[0]!, hidden: false },
    {
      ...tasks[1]!,
      hidden: true,
      assignee: { kind: "email" as const, email: "other@example.test" },
    },
  ];
  const assign = vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>();
  render(
    <WorkstreamColumn
      workstream={workstream}
      tasks={allTasks.slice(0, 1)}
      allTasks={allTasks}
      canAssign
      isPending={false}
      onAssign={assign}
    >
      Cards
    </WorkstreamColumn>,
  );
  expect(screen.getByRole("button", { name: "Mixed owners" })).toBeTruthy();
  expect(
    screen.queryByRole("list", { name: "Workstream assignees" }),
  ).toBeNull();
  expect(screen.queryByText("owner@example.test")).toBeNull();
  expect(screen.queryByText("other@example.test")).toBeNull();
  expect(assign).not.toHaveBeenCalled();
});

it("uses user identity rather than differing profile details to count owners", () => {
  const allTasks = ["old@example.test", "current@example.test"].map(
    (email, index) => ({
      ...tasks[index]!,
      assignee: {
        kind: "user" as const,
        userId: "member",
        name: "Team member",
        email,
      },
    }),
  );
  render(
    <WorkstreamColumn
      workstream={workstream}
      tasks={[]}
      allTasks={allTasks}
      canAssign
      isPending={false}
      onAssign={vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>()}
    >
      Cards
    </WorkstreamColumn>,
  );
  expect(screen.getByRole("button", { name: "old@example.test" })).toBeTruthy();
  expect(screen.queryByText(workstream.suggestedOwner)).toBeNull();
});

it.each([0, 1, 2])(
  "shows %i completed required tasks without inventing partial task completion",
  (completed) => {
    const required = tasks.slice(0, 2).map((task, index) => ({
      ...task,
      hidden: false,
      badge: undefined,
      status: index < completed ? ("done" as const) : ("in_progress" as const),
      verified: index === 0 && completed > 0,
    }));
    render(
      <WorkstreamColumn
        workstream={workstream}
        tasks={required}
        allTasks={required}
        canAssign
        isPending={false}
        onAssign={vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>()}
      >
        Cards
      </WorkstreamColumn>,
    );
    expect(
      screen.getByText(`${completed} / 2 required tasks complete`),
    ).toBeTruthy();
    const progress = screen.getByRole("progressbar", {
      name: `${workstream.title} progress`,
    });
    expect(progress.getAttribute("aria-valuenow")).toBe(String(completed));
    expect(progress.getAttribute("aria-valuemax")).toBe("2");
    expect(progress.getAttribute("aria-valuetext")).toBe(
      `${completed} of 2 required tasks complete`,
    );
    expect((progress.firstElementChild as HTMLElement).style.width).toBe(
      `${completed * 50}%`,
    );
  },
);

it("excludes hidden and optional tasks even when their cards are displayed", () => {
  const allTasks = tasks.slice(0, 3).map((task, index) => ({
    ...task,
    status: index === 0 ? ("awaiting_support" as const) : ("done" as const),
    hidden: index === 1,
    badge: index === 2 ? "Optional" : undefined,
  }));
  render(
    <WorkstreamColumn
      workstream={workstream}
      tasks={allTasks}
      allTasks={allTasks}
      canAssign
      isPending={false}
      onAssign={vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>()}
    >
      Cards
    </WorkstreamColumn>,
  );
  expect(screen.getByText("0 / 1 required tasks complete")).toBeTruthy();
  expect(screen.getByRole("progressbar").getAttribute("aria-valuemax")).toBe(
    "1",
  );
});

it.each(["empty", "hidden", "optional"])(
  "does not show a misleading percentage for %s required tasks",
  (kind) => {
    const allTasks =
      kind === "empty"
        ? []
        : tasks.map((task) => ({
            ...task,
            hidden: kind === "hidden",
            badge: kind === "optional" ? "Optional" : undefined,
          }));
    render(
      <WorkstreamColumn
        workstream={workstream}
        tasks={allTasks}
        allTasks={allTasks}
        canAssign
        isPending={false}
        onAssign={vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>()}
      >
        Cards
      </WorkstreamColumn>,
    );
    expect(screen.getByText("No required tasks")).toBeTruthy();
    expect(screen.queryByRole("progressbar")).toBeNull();
  },
);

it.each(["hover", "keyboard"])(
  "reveals the full description on %s",
  async (method) => {
    mockDescriptionSize();
    const user = userEvent.setup();
    render(
      <WorkstreamColumn
        workstream={workstream}
        tasks={tasks}
        allTasks={tasks}
        canAssign
        isPending={false}
        onAssign={vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>()}
      >
        Cards
      </WorkstreamColumn>,
    );
    const description = screen.getByText(workstream.description);
    expect(description.tabIndex).toBe(0);
    if (method === "hover") await user.hover(description);
    else await user.tab();
    expect((await screen.findByRole("tooltip")).textContent).toBe(
      workstream.description,
    );
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("tooltip")).toBeNull();
  },
);

it.each([
  undefined,
  { kind: "email" as const, email: "invite@example.test" },
  {
    kind: "user" as const,
    userId: "member",
    name: "Team member",
    email: "member@example.test",
  },
])("shows the suggested role only without an owner: %o", (assignee) => {
  const allTasks = tasks.map((task) => ({ ...task, assignee }));
  render(
    <WorkstreamColumn
      workstream={workstream}
      tasks={allTasks.slice(0, 1)}
      allTasks={allTasks}
      canAssign
      isPending={false}
      onAssign={vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>()}
    >
      Cards
    </WorkstreamColumn>,
  );
  if (assignee) {
    expect(screen.queryByText(workstream.suggestedOwner)).toBeNull();
    expect(screen.getByRole("button", { name: assignee.email })).toBeTruthy();
  } else
    expect(
      screen.getByRole("button", { name: workstream.suggestedOwner }),
    ).toBeTruthy();
  expect(screen.queryByText("Mixed owners")).toBeNull();
});

it("does not expose a tooltip or tab stop for fully visible text", async () => {
  const size = mockDescriptionSize();
  size.scrollHeight = size.height;
  const user = userEvent.setup();
  render(column("Short description"));
  const description = screen.getByText("Short description");
  expect(description.hasAttribute("tabindex")).toBe(false);
  await user.hover(description);
  fireEvent.focus(description);
  expect(screen.queryByRole("tooltip")).toBeNull();
  await user.tab();
  expect(document.activeElement).not.toBe(description);
});

it("remeasures on resize, closes when no longer truncated, and cleans up", async () => {
  const size = mockDescriptionSize();
  size.scrollHeight = size.height;
  const observers: {
    callback: ResizeObserverCallback;
    observe: ReturnType<typeof vi.fn>;
    disconnect: ReturnType<typeof vi.fn>;
  }[] = [];
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe = vi.fn();
      unobserve = vi.fn();
      disconnect = vi.fn();
      callback: ResizeObserverCallback;
      constructor(callback: ResizeObserverCallback) {
        this.callback = callback;
        observers.push(this);
      }
    },
  );
  const user = userEvent.setup();
  const { unmount } = render(column());
  const description = screen.getByText(workstream.description);
  const observer = observers.find((item) =>
    item.observe.mock.calls.some(([target]) => target === description),
  )!;
  expect(description.tabIndex).toBe(-1);
  size.scrollHeight = 40;
  act(() => observer.callback([], observer as unknown as ResizeObserver));
  expect(description.tabIndex).toBe(0);
  await user.tab();
  expect(await screen.findByRole("tooltip")).toBeTruthy();
  size.scrollHeight = size.height;
  act(() => observer.callback([], observer as unknown as ResizeObserver));
  expect(screen.queryByRole("tooltip")).toBeNull();
  expect(description.hasAttribute("tabindex")).toBe(false);
  unmount();
  expect(observer.disconnect).toHaveBeenCalledOnce();
});

it("remeasures changed text and supports resize without ResizeObserver", () => {
  vi.stubGlobal("ResizeObserver", undefined);
  const size = mockDescriptionSize();
  const { rerender, unmount } = render(column());
  expect(screen.getByText(workstream.description).tabIndex).toBe(0);
  size.scrollHeight = size.height;
  rerender(column("Short"));
  const description = screen.getByText("Short");
  expect(description.hasAttribute("tabindex")).toBe(false);
  size.scrollWidth = 300;
  fireEvent(window, new Event("resize"));
  expect(description.tabIndex).toBe(0);
  const remove = vi.spyOn(window, "removeEventListener");
  unmount();
  expect(remove).toHaveBeenCalledWith("resize", expect.any(Function));
});

it.each([
  ["connect", "IT Admin"],
  ["observe", "Engineering Lead"],
  ["distribute", "Engineering Lead"],
  ["secure", "Security Lead"],
])("uses a title-case role placeholder for %s", (id, role) => {
  render(
    <WorkstreamColumn
      workstream={ONBOARDING_WORKSTREAMS.find((item) => item.id === id)!}
      tasks={[]}
      allTasks={[]}
      canAssign
      isPending={false}
      onAssign={vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>()}
    >
      Cards
    </WorkstreamColumn>,
  );
  expect(screen.getByRole("button", { name: role })).toBeTruthy();
});

it("labels partial legacy owners and keeps workstream assignment explicit", () => {
  const member = {
    kind: "user" as const,
    userId: "member",
    name: "Team member",
    email: "member@example.test",
  };
  const allTasks = [
    { ...tasks[0]!, assignee: member },
    { ...tasks[1]!, assignee: member },
    {
      ...tasks[0]!,
      assignee: { kind: "email" as const, email: "outside@example.test" },
    },
    { ...tasks[1]!, assignee: undefined },
  ];
  const assign = vi.fn<ComponentProps<typeof WorkstreamColumn>["onAssign"]>();
  render(
    <WorkstreamColumn
      workstream={workstream}
      tasks={[]}
      allTasks={allTasks}
      canAssign
      isPending={false}
      onAssign={assign}
    >
      Cards
    </WorkstreamColumn>,
  );
  expect(
    screen.queryByRole("list", { name: "Workstream assignees" }),
  ).toBeNull();
  expect(screen.queryByText("Team member")).toBeNull();
  expect(screen.queryByText("outside@example.test")).toBeNull();
  expect(assign).not.toHaveBeenCalled();
  expect(
    screen
      .getByRole("button", { name: "Partially assigned" })
      .getAttribute("data-owner-breakdown"),
  ).toBe("Team member: 2 · outside@example.test: 1 · Unassigned: 1");
  expect(
    screen
      .getByRole("button", { name: "Partially assigned" })
      .getAttribute("data-bulk-assignment"),
  ).toBe("true");
  fireEvent.click(screen.getByRole("button", { name: "Partially assigned" }));
  expect(assign).toHaveBeenCalledExactlyOnceWith(undefined);
});
