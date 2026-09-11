import type { Query, QueryClient } from "@tanstack/react-query";

function isIssuerQuery(query: Query): boolean {
  return (
    query.queryKey[0] === "@gram/admin-client" &&
    query.queryKey[1] === "admin" &&
    typeof query.queryKey[2] === "string" &&
    query.queryKey[2].toLowerCase().includes("globalissuer")
  );
}

export function invalidateIssuerQueries(
  cache: QueryClient,
  deletedId?: string,
): Promise<void> {
  if (deletedId !== undefined) {
    // Evict before invalidating: detail observers can remain mounted until navigation.
    // Generated infinite keys insert "infinite" before the request parameters.
    cache.removeQueries({
      predicate: (query) =>
        isIssuerQuery(query) &&
        query.queryKey.some(
          (part) =>
            typeof part === "object" &&
            part !== null &&
            (("id" in part && part.id === deletedId) ||
              ("targetId" in part && part.targetId === deletedId)),
        ),
    });
  }
  return cache.invalidateQueries({ predicate: isIssuerQuery });
}
