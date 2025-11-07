import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const gram = new Gram({
  envSchema: {
    PYLON_API_KEY: z.string(),
  },
})
  .tool({
    name: "greet",
    description: "Greet someone special",
    inputSchema: { name: z.string() },
    async execute(ctx, input) {
      return ctx.json({ message: `Hello, ${input["name"]}!` });
    },
  })
  .tool({
    name: "create_pylon_task",
    description: "Create a new task in Pylon",
    inputSchema: {
      title: z.string(),
      body_html: z.optional(z.string()),
      status: z.optional(z.enum(["not_started", "in_progress", "completed"])),
      customer_portal_visible: z.optional(z.boolean()),
      due_date: z.optional(z.string()),
      assignee_id: z.optional(z.string()),
      account_id: z.optional(z.string()),
      project_id: z.optional(z.string()),
      milestone_id: z.optional(z.string()),
    },
    async execute(ctx, input) {
      const apiKey = ctx.env["PYLON_API_KEY"];

      // Build query parameters
      const params = new URLSearchParams();
      params.append("title", input["title"]);

      if (input["body_html"]) params.append("body_html", input["body_html"]);
      if (input["status"]) params.append("status", input["status"]);
      if (input["customer_portal_visible"] !== undefined)
        params.append("customer_portal_visible", String(input["customer_portal_visible"]));
      if (input["due_date"]) params.append("due_date", input["due_date"]);
      if (input["assignee_id"]) params.append("assignee_id", input["assignee_id"]);
      if (input["account_id"]) params.append("account_id", input["account_id"]);
      if (input["project_id"]) params.append("project_id", input["project_id"]);
      if (input["milestone_id"]) params.append("milestone_id", input["milestone_id"]);

      const url = `https://api.usepylon.com/tasks?${params.toString()}`;

      const response = await fetch(url, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${apiKey}`,
          Accept: "*/*",
        },
        signal: ctx.signal,
      });

      if (!response.ok) {
        const errorBody = await response.text();
        ctx.fail(
          {
            error: `Failed to create Pylon task: ${response.status} ${response.statusText}`,
            details: errorBody,
          },
          { status: response.status },
        );
      }

      const data = await response.json();
      return ctx.json(data);
    },
  });

export default gram;
