import { describe, expect, it } from "vitest";
import { assignedTo, resolveBoardTasks } from "./board-store";
import { ONBOARDING_TASKS } from "./tasks";

describe("server task projection", () => {
  it("preserves every server field for all catalog keys", () => {
    const input = ONBOARDING_TASKS.map(({ id }) => ({
      key: id,
      title: `Server ${id}`,
      description: "Server copy",
      status: "todo" as const,
      completedByFact: false,
      hidden: true,
      blockedBy: ["instrument-agents"],
    }));
    const tasks = resolveBoardTasks(input);
    expect(tasks).toHaveLength(ONBOARDING_TASKS.length);
    tasks.forEach((task, index) => {
      expect(task).toMatchObject({
        id: input[index]!.key,
        title: input[index]!.title,
        description: "Server copy",
        hidden: true,
        status: "todo",
        blockedBy: ["instrument-agents"],
      });
    });
  });
  it("retains suggested owners and the optional platform task badge in the projection", () => {
    const tasks = resolveBoardTasks(
      ONBOARDING_TASKS.map(({ id }) => ({
        key: id,
        title: `Server ${id}`,
        description: "Server copy",
        status: "todo" as const,
        completedByFact: false,
        hidden: false,
        blockedBy: [],
      })),
    );
    expect(tasks).toHaveLength(ONBOARDING_TASKS.length);
    tasks.forEach((task, index) => {
      expect(task.suggestedOwner).toBe(ONBOARDING_TASKS[index]!.suggestedOwner);
      expect(task.suggestedOwner.length).toBeGreaterThan(0);
    });
    expect(tasks.find((task) => task.id === "platform-mcp")?.badge).toBe(
      "Optional",
    );
  });
  it("preserves known tasks while omitting unsupported executable IDs", () => {
    const fields = {
      title: "Task",
      description: "",
      status: "todo" as const,
      completedByFact: false,
      hidden: false,
      blockedBy: ["future-task"],
    };
    const tasks = resolveBoardTasks([
      { ...fields, key: "future-task" },
      { ...fields, key: "connect-idp" },
    ]);
    expect(tasks).toHaveLength(1);
    expect(tasks[0]).toMatchObject({
      id: "connect-idp",
      blockedBy: ["future-task"],
    });
    expect(resolveBoardTasks([{ ...fields, key: "future-task" }])).toEqual([]);
  });
  it("uses server-resolved assignment and fact completion", () => {
    const [task] = resolveBoardTasks([
      {
        key: "connect-idp",
        title: "SSO",
        description: "",
        status: "done",
        completedByFact: true,
        hidden: false,
        blockedBy: [],
        assignee: { userId: "user-a", email: "ADMIN@example.test" },
      },
    ]);
    expect(task!.verified).toBe(true);
    expect(
      assignedTo(task!, { id: "user-a", email: "other@example.test" }),
    ).toBe(true);
    expect(
      assignedTo(task!, { id: "user-b", email: "admin@example.test" }),
    ).toBe(true);
    expect(
      assignedTo(task!, { id: "user-b", email: "other@example.test" }),
    ).toBe(false);
  });
});
