import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  data: { logsEnabled: false } as { logsEnabled: boolean } | undefined,
  error: null as Error | null,
  isFetching: false,
  isLoading: false,
  isAdmin: true,
  organizationId: "org-active",
  mutate: vi.fn(),
  mutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: () => Promise<void>;
      }
    | undefined,
  mutationStatus: "idle" as "idle" | "pending" | "success" | "error",
  productFeaturesQuery: vi.fn(),
  refetch: vi.fn(),
}));

const invalidateAllProductFeatures = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: testState.organizationId }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) =>
      scope === "org:admin" ? testState.isAdmin : true,
  }),
}));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: (options: {
    onError?: (error: Error) => void;
    onSuccess?: () => Promise<void>;
  }) => {
    testState.mutationOptions = options;
    return {
      mutate: testState.mutate,
      status: testState.mutationStatus,
    };
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

import { EnableLoggingStep } from "./enable-logging-step";

const onComplete = vi.fn();
const onSkip = vi.fn();
const onBack = vi.fn();

function stepProps() {
  return {
    onComplete: () => {
      onComplete();
    },
    onSkip: () => {
      onSkip();
    },
    onBack: () => {
      onBack();
    },
  };
}

function renderStep() {
  return render(<EnableLoggingStep {...stepProps()} />);
}

beforeEach(() => {
  testState.data = { logsEnabled: false };
  testState.error = null;
  testState.isFetching = false;
  testState.isLoading = false;
  testState.isAdmin = true;
  testState.organizationId = "org-active";
  testState.mutate.mockReset();
  testState.mutationOptions = undefined;
  testState.mutationStatus = "idle";
  testState.productFeaturesQuery.mockReset();
  testState.refetch.mockReset();
  invalidateAllProductFeatures.mockReset();
  invalidateAllProductFeatures.mockResolvedValue(undefined);
  onComplete.mockReset();
  onSkip.mockReset();
  onBack.mockReset();
});

afterEach(cleanup);

describe("EnableLoggingStep", () => {
  it("discloses collected data and keeps logging opt-in", () => {
    renderStep();

    expect(testState.productFeaturesQuery).toHaveBeenCalledWith(
      { organizationId: "org-active" },
      undefined,
      expect.anything(),
    );
    expect(screen.getByText("What enabling collects")).toBeTruthy();
    expect(
      screen.getByText("Tool call traces and execution metadata"),
    ).toBeTruthy();
    expect(
      screen.getByText(/Speakeasy will not start recording until you opt in/),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: /Enable logging/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Skip for now" })).toBeTruthy();
  });

  it("enables the logs feature for the active organization", () => {
    renderStep();

    fireEvent.click(screen.getByRole("button", { name: /Enable logging/ }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          organizationId: "org-active",
          featureName: "logs",
          enabled: true,
        },
      },
    });
  });

  it("advances after a successful opt-in", async () => {
    renderStep();
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    await activeMutation!.onSuccess!();

    expect(invalidateAllProductFeatures).toHaveBeenCalledOnce();
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("lets the admin skip without enabling logging", () => {
    renderStep();

    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }));

    expect(testState.mutate).not.toHaveBeenCalled();
    expect(onSkip).toHaveBeenCalledOnce();
  });

  it("shows a continue path when logging is already enabled", () => {
    testState.data = { logsEnabled: true };

    renderStep();

    expect(screen.getByText("Product logging is enabled")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Skip for now" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Continue/ }));
    expect(onComplete).toHaveBeenCalledOnce();
    expect(testState.mutate).not.toHaveBeenCalled();
  });

  it("disables enablement when the viewer is not an org admin", () => {
    testState.isAdmin = false;

    renderStep();

    expect(
      (
        screen.getByRole("button", {
          name: /Enable logging/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(screen.getByRole("button", { name: "Skip for now" })).toBeTruthy();
  });

  it("ignores a deferred mutation completion after an organization switch", async () => {
    const { rerender } = renderStep();
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    testState.organizationId = "org-next";
    rerender(<EnableLoggingStep {...stepProps()} />);
    await activeMutation!.onSuccess!();

    expect(invalidateAllProductFeatures).not.toHaveBeenCalled();
    expect(onComplete).not.toHaveBeenCalled();
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
    const { rerender } = renderStep();
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    const completion = activeMutation!.onSuccess!();
    testState.organizationId = "org-next";
    rerender(<EnableLoggingStep {...stepProps()} />);
    resolveInvalidation!();
    await completion;

    expect(onComplete).not.toHaveBeenCalled();
  });

  it("ignores a stale mutation error after an organization switch", () => {
    const { rerender } = renderStep();
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onError).toBeTypeOf("function");

    testState.organizationId = "org-next";
    rerender(<EnableLoggingStep {...stepProps()} />);
    activeMutation!.onError!(new Error("stale failure"));

    expect(screen.queryByText("stale failure")).toBeNull();
  });

  it("shows a retry state when product features fail to load", () => {
    testState.data = undefined;
    testState.error = new Error("features unavailable");

    renderStep();

    expect(screen.getByRole("alert").textContent).toContain(
      "Couldn't load the current logging setting.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.refetch).toHaveBeenCalledOnce();
    expect(
      (
        screen.getByRole("button", {
          name: /Enable logging/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("keeps the retry action disabled while refetching", () => {
    testState.data = undefined;
    testState.error = new Error("features unavailable");
    testState.isFetching = true;

    renderStep();

    expect(
      (screen.getByRole("button", { name: "Retrying…" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });
});
