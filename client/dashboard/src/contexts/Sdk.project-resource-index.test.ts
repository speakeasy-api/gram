import { describe, expect, it } from "vitest";
import { queryClient } from "./Sdk";

describe("project resource index mutation invalidation", () => {
  it("invalidates cached indexes after every successful mutation", async () => {
    const indexKey = ["project-resource-index", "user", "org", "project"];
    const unrelatedKey = ["unrelated-query"];
    queryClient.setQueryData(indexKey, { schemaVersion: "1" });
    queryClient.setQueryData(unrelatedKey, { ok: true });

    const mutation = queryClient.getMutationCache().build(queryClient, {
      mutationFn: async () => ({ ok: true }),
    });
    await mutation.execute(undefined);

    expect(queryClient.getQueryState(indexKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(unrelatedKey)?.isInvalidated).toBe(false);
    queryClient.removeQueries({ queryKey: indexKey });
    queryClient.removeQueries({ queryKey: unrelatedKey });
  });
});
