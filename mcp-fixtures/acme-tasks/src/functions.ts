import { Functions } from "@speakeasy-api/functions";
import { z } from "zod/mini";

import { tasks } from "./data.ts";

const readOnly = {
  readOnlyHint: true,
  destructiveHint: false,
  idempotentHint: true,
  openWorldHint: false,
};

const functions = new Functions()
  .tool({
    name: "list_tasks",
    description:
      "List tasks in the Acme team's tracker, optionally filtered by status or assignee.",
    inputSchema: {
      status: z.optional(z.enum(["todo", "in_progress", "done"])),
      assignee: z.optional(z.string()),
    },
    annotations: { title: "List Tasks", ...readOnly },
    async execute(ctx, input) {
      const assignee = input.assignee?.trim().toLowerCase();
      const matches = tasks.filter(
        (task) =>
          (input.status == null || task.status === input.status) &&
          (assignee == null ||
            assignee === "" ||
            task.assignee.toLowerCase() === assignee),
      );
      return ctx.json({ tasks: matches });
    },
  })
  .tool({
    name: "get_task",
    description:
      "Get one task from the Acme team's tracker by its ID, for example ACME-101.",
    inputSchema: { id: z.string() },
    annotations: { title: "Get Task", ...readOnly },
    async execute(ctx, input) {
      const id = input.id.trim().toUpperCase();
      const task = tasks.find((candidate) => candidate.id === id);
      if (task == null) {
        return ctx.fail(
          { error: `No task with ID ${input.id}.` },
          { status: 404 },
        );
      }
      return ctx.json({ task });
    },
  })
  .tool({
    name: "search_tasks",
    description:
      "Search the Acme team's tasks by words in their title or labels.",
    inputSchema: { query: z.string().check(z.minLength(1)) },
    annotations: { title: "Search Tasks", ...readOnly },
    async execute(ctx, input) {
      const words = input.query.toLowerCase().split(/\s+/).filter(Boolean);
      const matches = tasks.filter((task) => {
        const text = [task.title, ...task.labels].join(" ").toLowerCase();
        return words.every((word) => text.includes(word));
      });
      return ctx.json({ tasks: matches });
    },
  });

export default functions;
