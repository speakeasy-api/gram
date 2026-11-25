import { z } from "zod";

// The API accepts at most this many characters of search text.
export const MAX_QUERY_LENGTH = 256;

export function capQuery(text: string): string {
  const chars = Array.from(text);
  return chars.length > MAX_QUERY_LENGTH
    ? chars.slice(0, MAX_QUERY_LENGTH).join("")
    : text;
}

// The list's filters live in the address so a product page can carry them back.
// A filter at its default is absent.
const searchSchema = z.object({
  q: z.string().min(1).transform(capQuery).optional().catch(undefined),
  deleted: z.literal(true).optional().catch(undefined),
});

export type AIProductsSearch = z.infer<typeof searchSchema>;

export function aiProductsSearch(
  search: Record<string, unknown>,
): AIProductsSearch {
  return searchSchema.parse(search);
}
