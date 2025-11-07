import { Gram, assert } from "@gram-ai/functions";
import * as z from "zod/mini";

const gram = new Gram({
  envSchema: {
    PYLON_API_TOKEN: z.string(),
  },
})
  .tool({
    name: "list_recent_issues_for_customers_in_pylon",
    description:
      "List all issues from Pylon created in the last week. Returns issue details including ID, title, state, assignee, and creation date.",
    inputSchema: {},
    async execute(ctx) {
      const apiToken = ctx.env["PYLON_API_TOKEN"];

      // Calculate timestamps for last week
      const endTime = new Date();
      const startTime = new Date();
      startTime.setDate(startTime.getDate() - 7);

      const requestBody = {
        filter: {
          created_at: {
            gte: startTime.toISOString(),
            lte: endTime.toISOString(),
          },
        },
        limit: 100,
      };

      const response = await fetch("https://api.usepylon.com/issues/search", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${apiToken}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(requestBody),
        signal: ctx.signal,
      });

      assert(
        response.ok,
        {
          error: `Failed to fetch issues: ${response.statusText}`,
          status: response.status,
        },
        { status: response.status },
      );

      const data = await response.json();
      return ctx.json(data);
    },
  })
  .tool({
    name: "create_issue_for_account_pylon",
    description:
      "Create a new issue in Pylon for a specific account. Requires a title and body content in HTML format. Optionally assign to a team member, add tags, or set priority.",
    inputSchema: {
      title: z.string(),
      body_html: z.string(),
      account_id: z.optional(z.string()),
      assignee_id: z.optional(z.string()),
      requester_id: z.optional(z.string()),
      tags: z.optional(z.array(z.string())),
      priority: z.optional(z.number()),
      team_id: z.optional(z.string()),
    },
    async execute(ctx, input) {
      const apiToken = ctx.env["PYLON_API_TOKEN"];

      const requestBody = {
        title: input["title"],
        body_html: input["body_html"],
        ...(input["account_id"] && { account_id: input["account_id"] }),
        ...(input["assignee_id"] && { assignee_id: input["assignee_id"] }),
        ...(input["requester_id"] && { requester_id: input["requester_id"] }),
        ...(input["tags"] && { tags: input["tags"] }),
        ...(input["priority"] && { priority: input["priority"] }),
        ...(input["team_id"] && { team_id: input["team_id"] }),
      };

      const response = await fetch("https://api.usepylon.com/issues", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${apiToken}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(requestBody),
        signal: ctx.signal,
      });

      assert(
        response.ok,
        {
          error: `Failed to create issue: ${response.statusText}`,
          status: response.status,
        },
        { status: response.status },
      );

      const data = await response.json();
      return ctx.json(data);
    },
  });

export default gram;
