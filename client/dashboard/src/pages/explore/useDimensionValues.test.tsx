import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { useDimensionValues } from "./useDimensionValues";

vi.mock("@gram/client/funcs/analyticsDimensionValues.js", () => ({
  analyticsDimensionValues: () =>
    Promise.reject(new Error("memory limit exceeded")),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project" }),
}));

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    // The app's client throws query errors to the route boundary.
    defaultOptions: { queries: { retry: false, throwOnError: true } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("useDimensionValues", () => {
  it("reports a failed values query rather than throwing it to the page", async () => {
    const { result } = renderHook(
      () => useDimensionValues("sessions", "user", { window: "1d" }, true),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error?.message).toBe("memory limit exceeded");
  });
});
