import { describe, expect, it } from "vitest";
import { groupOnboardingTasks } from "./onboarding-workstreams";

const tasks = ["alpha", "beta", "gamma", "unknown-first", "unknown-last"].map(
  (key) => ({ key, title: key, description: key, hidden: true }),
);

describe("groupOnboardingTasks", () => {
  it("derives group identity, titles, membership, and both orders from the response", () => {
    const groups = groupOnboardingTasks(tasks, [
      {
        id: "new-group",
        title: "New server title",
        taskKeys: ["gamma", "alpha"],
      },
      { id: "missing", title: "Unavailable tasks", taskKeys: ["absent"] },
      { id: "empty", title: "Empty", taskKeys: [] },
      { id: "second", title: "Another title", taskKeys: ["beta"] },
    ]);
    expect(groups).toEqual([
      {
        id: "new-group",
        title: "New server title",
        tasks: [tasks[2], tasks[0]],
      },
      { id: "second", title: "Another title", tasks: [tasks[1]] },
      { id: "other", title: "Other tasks", tasks: [tasks[3], tasks[4]] },
    ]);
    expect(groups[0]!.tasks[0]).toBe(tasks[2]);
  });

  it("keeps all tasks editable when the response catalog is empty", () => {
    expect(groupOnboardingTasks(tasks, [])).toEqual([
      { id: "other", title: "Other tasks", tasks },
    ]);
  });

  it("omits an empty fallback group and handles no tasks", () => {
    const workstreams = [
      { id: "all", title: "All", taskKeys: tasks.map((task) => task.key) },
    ];
    expect(groupOnboardingTasks(tasks, workstreams)).toEqual([
      { id: "all", title: "All", tasks },
    ]);
    expect(groupOnboardingTasks([], workstreams)).toEqual([]);
  });
});
