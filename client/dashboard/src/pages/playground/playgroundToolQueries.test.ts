import { queryKeyInstance } from "@gram/client/react-query/instance.js";
import { queryKeyListTools } from "@gram/client/react-query/listTools.js";
import { queryKeyToolset } from "@gram/client/react-query/toolset.js";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { invalidatePlaygroundToolQueries } from "./playgroundToolQueries";

describe("invalidatePlaygroundToolQueries", () => {
  it("invalidates the global tools list and both toolset views", async () => {
    const queryClient = new QueryClient();
    const queryKeys = [
      queryKeyListTools({}),
      queryKeyToolset({ slug: "weather-tools" }),
      queryKeyInstance({ toolsetSlug: "weather-tools" }),
    ];

    for (const queryKey of queryKeys) {
      queryClient.setQueryData(queryKey, { cached: true });
      expect(queryClient.getQueryState(queryKey)?.isInvalidated).toBe(false);
    }

    await invalidatePlaygroundToolQueries(queryClient, "weather-tools");

    for (const queryKey of queryKeys) {
      expect(queryClient.getQueryState(queryKey)?.isInvalidated).toBe(true);
    }
  });
});
