/** Drain SDK pagination without introducing a second transport or schema. */
export async function collectPageItems<T>(
  pages: Promise<AsyncIterable<{ result: { items: T[] } }>>,
): Promise<T[]> {
  const items: T[] = [];
  for await (const page of await pages) items.push(...page.result.items);
  return items;
}
