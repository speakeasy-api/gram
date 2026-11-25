import { z } from "zod";

// The list's filters live in the address so a product page can carry them back.
// A filter at its default is absent.
const searchSchema = z.object({
  q: z.string().min(1).optional().catch(undefined),
  deleted: z.literal(true).optional().catch(undefined),
});

export type AIProductsSearch = z.infer<typeof searchSchema>;

export function aiProductsSearch(
  search: Record<string, unknown>,
): AIProductsSearch {
  return searchSchema.parse(search);
}
