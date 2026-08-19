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
  organizationId: "org-active",
  mutate: vi.fn(),
  mutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: () => Promise<void>;
      }
    | undefined,
  productFeaturesQuery: vi.fn(),
  refetch: vi.fn(),
}));

const handleAPIError = vi.hoisted(() => vi.fn());
const invalidateAllProductFeatures = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: testState.organizationId }),
}));

vi.mock("@/lib/errors", () => ({ handleAPIError }));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: (options: {
    onError?: (error: Error) => void;
    onSuccess?: () => Promise<void>;
  }) => {
    testState.mutationOptions = options;
    return { isPending: false, mutate: testState.mutate };
  },
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures,
  useProductFeatures: (...args: unknown[]) => {
    testState.productFeaturesQuery(...args);
    return {
      data: testState.data,
      error: testState.error,
      isFetching: testState.isFetching,
      isLoading: testState.isLoading,
      refetch: testState.refetch,
    };
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: toastSuccess },
}));

import { ConsentToolFilteringSetting } from "./ConsentToolFilteringSetting";

beforeEach(() => {
  testState.data = { consentToolFilteringEnabled: true };
  testState.error = null;
  testState.isFetching = false;
  testState.isLoading = false;
  testState.organizationId = "org-active";
  testState.mutate.mockReset();
  testState.mutationOptions = undefined;
  testState.productFeaturesQuery.mockReset();
  testState.refetch.mockReset();
  handleAPIError.mockReset();
  invalidateAllProductFeatures.mockReset();
  toastSuccess.mockReset();
});

afterEach(cleanup);

describe("ConsentToolFilteringSetting", () => {
  it("reads and writes the active organization setting", () => {
    render(<ConsentToolFilteringSetting />);

    expect(testState.productFeaturesQuery).toHaveBeenCalledWith(
      { organizationId: "org-active" },
      undefined,
      expect.anything(),
    );

    fireEvent.click(
      screen.getByRole("switch", {
        name: "Tool filtering on consent screens",
      }),
    );

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          organizationId: "org-active",
          featureName: "consent_tool_filtering",
          enabled: false,
        },
      },
    });
  });

  it("ignores a deferred mutation completion after an organization switch", async () => {
    const { rerender } = render(<ConsentToolFilteringSetting />);
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    testState.organizationId = "org-next";
    rerender(<ConsentToolFilteringSetting />);
    await activeMutation!.onSuccess!();

    expect(invalidateAllProductFeatures).not.toHaveBeenCalled();
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(testState.productFeaturesQuery).toHaveBeenLastCalledWith(
      { organizationId: "org-next" },
      undefined,
      expect.anything(),
    );
  });

  it("ignores a mutation completion when the organization switches during invalidation", async () => {
    let resolveInvalidation: (() => void) | undefined;
    invalidateAllProductFeatures.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveInvalidation = resolve;
      }),
    );
    const { rerender } = render(<ConsentToolFilteringSetting />);
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    const completion = activeMutation!.onSuccess!();
    testState.organizationId = "org-next";
    rerender(<ConsentToolFilteringSetting />);
    resolveInvalidation!();
    await completion;

    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("ignores a stale mutation error after an organization switch", () => {
    const { rerender } = render(<ConsentToolFilteringSetting />);
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onError).toBeTypeOf("function");

    testState.organizationId = "org-next";
    rerender(<ConsentToolFilteringSetting />);
    activeMutation!.onError!(new Error("stale failure"));

    expect(handleAPIError).not.toHaveBeenCalled();
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
