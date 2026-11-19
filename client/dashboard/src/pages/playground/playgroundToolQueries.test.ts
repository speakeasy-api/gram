// @vitest-environment node
import { queryKeyListTools } from "@gram/client/react-query/listTools.js";
import { queryKeyToolset } from "@gram/client/react-query/toolset.js";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { invalidatePlaygroundToolQueries } from "./playgroundToolQueries";

describe("invalidatePlaygroundToolQueries", () => {
  it("invalidates the global tools list and the toolset view", async () => {
    const queryClient = new QueryClient();
    const queryKeys = [
      queryKeyListTools({}),
      queryKeyToolset({ slug: "weather-tools" }),
    ];

    for (const queryKey of queryKeys) {
      queryClient.setQueryData(queryKey, { cached: true });
      expect(queryClient.getQueryState(queryKey)?.isInvalidated).toBe(false);
    }

    await invalidatePlaygroundToolQueries(queryClient);

    for (const queryKey of queryKeys) {
      expect(queryClient.getQueryState(queryKey)?.isInvalidated).toBe(true);
    }
  });
});
