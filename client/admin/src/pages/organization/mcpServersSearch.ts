import { z } from "zod";

const searchSchema = z.object({
  // A project id. Whether it names one of this organization's projects is
  // settled by the page once the projects have loaded.
  project: z.string().optional().catch(undefined),
});

export type McpServersSearch = z.infer<typeof searchSchema>;

export function mcpServersSearch(
  search: Record<string, unknown>,
): McpServersSearch {
  return searchSchema.parse(search);
}
