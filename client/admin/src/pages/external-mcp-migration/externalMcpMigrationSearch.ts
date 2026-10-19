import { z } from "zod";

const searchSchema = z.object({
  // An organization id. Whether it names a candidate's organization is
  // settled by the page once the candidates have loaded.
  org: z.string().optional().catch(undefined),
  // A project id, narrowed the same way.
  project: z.string().optional().catch(undefined),
});

export type ExternalMcpMigrationSearch = z.infer<typeof searchSchema>;

export function externalMcpMigrationSearch(
  search: Record<string, unknown>,
): ExternalMcpMigrationSearch {
  return searchSchema.parse(search);
}
