export type UsersSearch = { q?: string; page?: number };
export function usersSearchSchema(
  search: Record<string, unknown>,
): UsersSearch {
  if (search.q !== undefined && typeof search.q !== "string")
    throw new Error("Search must be text.");
  if (
    search.page !== undefined &&
    typeof search.page !== "number" &&
    typeof search.page !== "string"
  )
    throw new Error("Invalid users page.");
  const page = search.page === undefined ? 1 : Number(search.page);
  if (!Number.isSafeInteger(page) || page < 1 || (page - 1) * 50 > 2147483647)
    throw new Error("Invalid users page.");
  return {
    q: search.q as string | undefined,
    page: search.page === undefined ? undefined : page,
  };
}
