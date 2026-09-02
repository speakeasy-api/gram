import { describe, expect, it } from "vitest";

import { resolveBoardTasks, verifiedTaskIds } from "./board-store";
import { ONBOARDING_TASKS } from "./tasks";

describe("verifiedTaskIds", () => {
  it("is empty until the server confirms something", () => {
    expect(verifiedTaskIds(undefined, undefined).size).toBe(0);
  });

  it("locks the tasks the server can vouch for", () => {
    const ids = verifiedTaskIds(
      { ssoConfigured: true, dsyncConfigured: false },
      { configured: true, connected: true },
    );
    expect([...ids]).toEqual(["connect-idp", "create-marketplace"]);
  });
});

describe("resolveBoardTasks", () => {
  it("starts every task in To Do", () => {
    const tasks = resolveBoardTasks({}, new Set());
    expect(tasks.map((task) => task.id)).toEqual(
      ONBOARDING_TASKS.map((task) => task.id),
    );
    expect(
      tasks.every(
        (task) => task.status === "todo" && !task.hidden && !task.verified,
      ),
    ).toBe(true);
  });

  it("applies the stored status, assignee, hidden flag and reminder", () => {
    const [task] = resolveBoardTasks(
      {
        "connect-idp": {
          status: "awaiting_support",
          assignee: { kind: "email", email: "it-admin@example.com" },
          hidden: true,
          lastRemindedAt: "2026-09-01T10:00:00.000Z",
        },
      },
      new Set(),
    );
    expect(task).toMatchObject({
      id: "connect-idp",
      status: "awaiting_support",
      hidden: true,
      assignee: { kind: "email", email: "it-admin@example.com" },
    });
    expect(task?.lastRemindedAt?.toISOString()).toBe(
      "2026-09-01T10:00:00.000Z",
    );
  });

  it("pins verified tasks to Done whatever the board says", () => {
    const [task] = resolveBoardTasks(
      { "connect-idp": { status: "todo" } },
      new Set(["connect-idp"]),
    );
    expect(task).toMatchObject({ status: "done", verified: true });
  });
});
