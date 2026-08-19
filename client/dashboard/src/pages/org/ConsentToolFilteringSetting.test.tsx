import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  data: { consentToolFilteringEnabled: true } as
    | { consentToolFilteringEnabled: boolean }
    | undefined,
  error: null as Error | null,
  isFetching: false,
  isLoading: false,
  mutate: vi.fn(),
  refetch: vi.fn(),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/errors", () => ({ handleAPIError: vi.fn() }));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: () => ({
    isPending: false,
    mutate: testState.mutate,
  }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures: vi.fn(),
  useProductFeatures: () => ({
    data: testState.data,
    error: testState.error,
    isFetching: testState.isFetching,
    isLoading: testState.isLoading,
    refetch: testState.refetch,
  }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { ConsentToolFilteringSetting } from "./ConsentToolFilteringSetting";

beforeEach(() => {
  testState.data = { consentToolFilteringEnabled: true };
  testState.error = null;
  testState.isFetching = false;
  testState.isLoading = false;
  testState.mutate.mockReset();
  testState.refetch.mockReset();
});

afterEach(cleanup);

describe("ConsentToolFilteringSetting", () => {
  it("renders the current feature setting", () => {
    render(<ConsentToolFilteringSetting />);

    expect(
      screen
        .getByRole("switch", {
          name: "Tool filtering on consent screens",
        })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("shows a retry state when product features fail to load", () => {
    testState.data = undefined;
    testState.error = new Error("features unavailable");

    render(<ConsentToolFilteringSetting />);

    expect(screen.getByRole("alert").textContent).toContain(
      "Couldn't load the tool filtering setting.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.refetch).toHaveBeenCalledOnce();
  });

  it("keeps the retry action disabled while refetching", () => {
    testState.data = undefined;
    testState.error = new Error("features unavailable");
    testState.isFetching = true;

    render(<ConsentToolFilteringSetting />);

    expect(
      (
        screen.getByRole("button", {
          name: "Retrying…",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});
