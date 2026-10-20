import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { invalidateRemoteMcpSourceViews } from "./sourceInvalidation";

describe("invalidateRemoteMcpSourceViews", () => {
  it("refetches the scope pin view even while nothing observes it", async () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await invalidateRemoteMcpSourceViews(queryClient);

    expect(invalidate).toHaveBeenCalledWith({
      refetchType: "all",
      queryKey: ["@gram/client", "remoteMcp", "getServerScopes"],
    });
  });
});
