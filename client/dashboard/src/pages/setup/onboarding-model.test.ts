import { describe, expect, it } from "vitest";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { SETUP_WORKSTREAMS } from "./components/board/workstream-fixtures";
import { assignedTo, buildOnboardingModel } from "./onboarding-model";
import { ONBOARDING_TASK_IDS, ONBOARDING_TASKS } from "./onboarding-tasks";

function task(key: string, overrides: Partial<SetupTask> = {}): SetupTask {
  return {
    key,
    title: `Server ${key}`,
    description: "Server copy",
    status: "todo",
    completedByFact: false,
    countsTowardProgress: true,
    hidden: false,
    blockedBy: [],
    ...overrides,
  };
}

describe("buildOnboardingModel", () => {
  it("orders tasks by workstream membership, not the task array", () => {
    // Catalog order on the server differs from workstream order.
    const tasks = [...ONBOARDING_TASK_IDS].reverse().map((key) => task(key));
    const model = buildOnboardingModel(tasks, SETUP_WORKSTREAMS);
    expect(model.tasks.map((item) => item.id)).toEqual(
      SETUP_WORKSTREAMS.flatMap((workstream) => workstream.taskKeys),
    );
    expect(model.workstreams.map((workstream) => workstream.id)).toEqual([
      "connect",
      "observe",
      "distribute",
      "secure",
    ]);
  });

  it("keeps a task no workstream claims, after the workstreams", () => {
    const model = buildOnboardingModel(
      [task("platform-mcp"), task("domain-verification")],
      [{ id: "connect", title: "Connect", taskKeys: ["domain-verification"] }],
    );
    expect(model.tasks.map((item) => item.id)).toEqual([
      "domain-verification",
      "platform-mcp",
    ]);
  });

  it("separates hidden tasks from the walk and from progress", () => {
    const model = buildOnboardingModel(
      [
        task("domain-verification", { status: "done" }),
        task("identity-provider", { hidden: true, status: "done" }),
        task("connect-idp"),
      ],
      SETUP_WORKSTREAMS,
    );
    expect(model.tasks).toHaveLength(3);
    expect(model.visibleTasks.map((item) => item.id)).toEqual([
      "domain-verification",
      "connect-idp",
    ]);
    expect(model.progress).toEqual({ done: 1, total: 2 });
    expect(model.workstreams[0]!.tasks.map((item) => item.id)).toEqual([
      "domain-verification",
      "identity-provider",
      "connect-idp",
    ]);
  });

  it("keeps empty workstreams and missing members out of task lists", () => {
    const model = buildOnboardingModel(
      [task("configure-policies")],
      SETUP_WORKSTREAMS,
    );
    expect(model.workstreams).toHaveLength(4);
    expect(model.workstreams[0]!.tasks).toEqual([]);
    expect(model.workstreams[3]!.tasks.map((item) => item.id)).toEqual([
      "configure-policies",
    ]);
    expect(model.progress).toEqual({ done: 0, total: 1 });
  });

  it("projects unsupported keys explicitly instead of dropping them", () => {
    const model = buildOnboardingModel(
      [task("future-task", { status: "done" }), task("connect-idp")],
      [
        {
          id: "connect",
          title: "Connect",
          taskKeys: ["connect-idp", "future-task"],
        },
      ],
    );
    expect(model.unsupportedTaskKeys).toEqual(["future-task"]);
    expect(model.task("future-task")).toMatchObject({
      supported: false,
      suggestedOwner: "Unassigned",
      title: "Server future-task",
    });
    expect(model.task("connect-idp")?.supported).toBe(true);
    // The server still counts it, so progress does too.
    expect(model.progress).toEqual({ done: 1, total: 2 });
  });

  it("derives progress and the optional badge from server metadata", () => {
    const model = buildOnboardingModel(
      [
        task("create-marketplace", { status: "done" }),
        task("distribute-servers"),
        task("platform-mcp", { countsTowardProgress: false, status: "done" }),
      ],
      SETUP_WORKSTREAMS,
    );
    expect(model.progress).toEqual({ done: 1, total: 2 });
    expect(model.task("platform-mcp")?.badge).toBe("Optional");
    expect(model.task("distribute-servers")?.badge).toBeUndefined();
  });

  it("counts fact completion as done", () => {
    const model = buildOnboardingModel(
      [task("connect-idp", { completedByFact: true, status: "done" })],
      SETUP_WORKSTREAMS,
    );
    expect(model.task("connect-idp")?.verified).toBe(true);
    expect(model.progress).toEqual({ done: 1, total: 1 });
  });

  it("resolves dependency titles from server metadata with a fallback", () => {
    const model = buildOnboardingModel(
      [task("domain-verification", { title: "Verify your domain" })],
      SETUP_WORKSTREAMS,
    );
    expect(model.titleFor("domain-verification")).toBe("Verify your domain");
    expect(model.titleFor("future_task-key")).toBe("Future task key");
  });

  it("carries registry presentation for every known task", () => {
    const model = buildOnboardingModel(
      ONBOARDING_TASK_IDS.map((key) => task(key)),
      SETUP_WORKSTREAMS,
    );
    for (const id of ONBOARDING_TASK_IDS) {
      expect(model.task(id)?.suggestedOwner).toBe(
        ONBOARDING_TASKS[id].suggestedOwner,
      );
    }
  });

  it("uses server-resolved assignment", () => {
    const model = buildOnboardingModel(
      [
        task("connect-idp", {
          assignee: { userId: "user-a", email: "ADMIN@example.test" },
        }),
      ],
      SETUP_WORKSTREAMS,
    );
    const item = model.task("connect-idp")!;
    expect(
      assignedTo(item, { id: "user-a", email: "other@example.test" }),
    ).toBe(true);
    expect(
      assignedTo(item, { id: "user-b", email: "admin@example.test" }),
    ).toBe(true);
    expect(
      assignedTo(item, { id: "user-b", email: "other@example.test" }),
    ).toBe(false);
  });
});
