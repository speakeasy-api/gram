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
    expect(tasks).toHaveLength(13);
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
  it("does not silently omit unknown keys", () => {
    expect(() =>
      resolveBoardTasks([
        {
          key: "future-task",
          title: "Future",
          description: "",
          status: "todo",
          completedByFact: false,
          hidden: false,
          blockedBy: [],
        },
      ]),
    ).toThrow("Unsupported setup task");
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
