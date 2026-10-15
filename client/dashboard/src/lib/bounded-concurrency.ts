/**
 * Maps items through fn with at most limit calls in flight, keeping results
 * in input order. A rejection rejects the whole map; catch inside fn to keep
 * the other results.
 */
export async function mapWithConcurrency<T, R>(
  items: readonly T[],
  limit: number,
  fn: (item: T) => Promise<R>,
): Promise<R[]> {
  const results: R[] = [];
  let next = 0;
  const worker = async () => {
    while (next < items.length) {
      const index = next++;
      results[index] = await fn(items[index] as T);
    }
  };
  await Promise.all(
    Array.from({ length: Math.min(Math.max(limit, 1), items.length) }, worker),
  );
  return results;
}
