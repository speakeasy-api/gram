import assert from "node:assert/strict";
import { test } from "node:test";

import functions from "./functions.ts";

async function call(name: string, input: Record<string, unknown>) {
  // ctx.fail throws its error response rather than returning it.
  const response: Response = await functions
    .handleToolCall({ name, input } as never)
    .catch((thrown: unknown) => {
      if (thrown instanceof Response) return thrown;
      throw thrown;
    });
  return {
    status: response.status,
    body: (await response.json()) as Record<string, unknown>,
  };
}

test("list_tasks filters by status and assignee", async () => {
  const { status, body } = await call("list_tasks", {
    status: "todo",
    assignee: "sam okafor",
  });
  assert.equal(status, 200);
  assert.deepEqual(
    (body["tasks"] as { id: string }[]).map((task) => task.id),
    ["ACME-102", "ACME-105"],
  );
});

test("get_task finds a task by ID regardless of case", async () => {
  const { status, body } = await call("get_task", { id: "acme-103" });
  assert.equal(status, 200);
  assert.equal(
    (body["task"] as { title: string }).title,
    "Publish the October release notes",
  );
});

test("get_task reports an unknown ID", async () => {
  const { status } = await call("get_task", { id: "ACME-999" });
  assert.equal(status, 404);
});

test("search_tasks matches title and label words", async () => {
  const { body } = await call("search_tasks", { query: "security" });
  assert.deepEqual(
    (body["tasks"] as { id: string }[]).map((task) => task.id),
    ["ACME-102", "ACME-105"],
  );
});
