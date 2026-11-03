import { z } from "zod";

// The list's filters live in the address so an entry page can carry them and
// the crumb back to the list returns to the view as it was left. A filter at
// its default is absent.
const searchSchema = z.object({
  cursors: z.array(z.string().min(1)).optional().catch(undefined),
  q: z.string().min(1).optional().catch(undefined),
  publication: z.enum(["published", "unpublished"]).optional().catch(undefined),
});

export type RegistrySearch = z.infer<typeof searchSchema>;

export function registrySearch(
  search: Record<string, unknown>,
): RegistrySearch {
  return searchSchema.parse(search);
}
