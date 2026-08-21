import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  isAdmin: true,
  organizationId: "org-active",
  mutate: vi.fn(),
  productFeaturesQuery: vi.fn(),
  mutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: () => Promise<void>;
      }
    | undefined,
  features: {
    remoteSessionAutoRefreshEnabled: true,
    remoteSessionAutoRefreshEnforcedEnabled: false,
  },
}));

const invalidateAllProductFeatures = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: testState.organizationId }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: (props: { disabled: boolean }) => React.ReactNode;
  }) => children({ disabled: !testState.isAdmin }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures,
  useProductFeatures: (...args: unknown[]) => {
    testState.productFeaturesQuery(...args);
    return {
      data: testState.features,
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    };
  },
}));

vi.mock(
  "@gram/client/react-query/setRemoteSessionAutoRefreshPolicy.js",
  () => ({
    useSetRemoteSessionAutoRefreshPolicyMutation: (options: {
      onError?: (error: Error) => void;
      onSuccess?: () => Promise<void>;
    }) => {
      testState.mutationOptions = options;
      return { mutate: testState.mutate, isPending: false };
    },
  }),
);

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: toastSuccess },
}));

import { RemoteSessionRefreshPolicySetting } from "./RemoteSessionRefreshPolicySetting";

beforeEach(() => {
  testState.isAdmin = true;
  testState.organizationId = "org-active";
  testState.mutate.mockReset();
  testState.productFeaturesQuery.mockReset();
  testState.mutationOptions = undefined;
  testState.features.remoteSessionAutoRefreshEnabled = true;
  testState.features.remoteSessionAutoRefreshEnforcedEnabled = false;
  invalidateAllProductFeatures.mockReset();
  toastSuccess.mockReset();
});

afterEach(cleanup);

describe("RemoteSessionRefreshPolicySetting", () => {
  it("ignores a deferred mutation completion after an organization switch", async () => {
    const { rerender } = render(<RemoteSessionRefreshPolicySetting />);
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    testState.organizationId = "org-next";
    rerender(<RemoteSessionRefreshPolicySetting />);
    await activeMutation!.onSuccess!();

    expect(invalidateAllProductFeatures).not.toHaveBeenCalled();
    expect(testState.productFeaturesQuery).toHaveBeenLastCalledWith(
      { organizationId: "org-next" },
      undefined,
      expect.anything(),
    );
  });

  it("shows the effective organization policy", () => {
    render(<RemoteSessionRefreshPolicySetting />);

    expect(testState.productFeaturesQuery).toHaveBeenCalledWith(
      { organizationId: "org-active" },
      undefined,
      expect.anything(),
    );
    expect(
      (screen.getByLabelText("User controlled") as HTMLInputElement).checked,
    ).toBe(true);
    expect(
      (screen.getByLabelText("Disabled") as HTMLInputElement).checked,
    ).toBe(false);
    expect(
      (screen.getByLabelText("Required") as HTMLInputElement).checked,
    ).toBe(false);
  });

  it("ignores a mutation completion when the organization switches during invalidation", async () => {
    let resolveInvalidation: (() => void) | undefined;
    invalidateAllProductFeatures.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveInvalidation = resolve;
      }),
    );
    const { rerender } = render(<RemoteSessionRefreshPolicySetting />);
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    const completion = activeMutation!.onSuccess!();
    testState.organizationId = "org-next";
    rerender(<RemoteSessionRefreshPolicySetting />);
    resolveInvalidation!();
    await completion;

    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("lets organization admins require automatic refresh", () => {
    render(<RemoteSessionRefreshPolicySetting />);

    fireEvent.click(screen.getByLabelText("Required"));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setRemoteSessionAutoRefreshPolicyRequestBody: {
          organizationId: "org-active",
          policy: "enforced",
        },
      },
    });
  });

  it("refreshes product features after saving", async () => {
    render(<RemoteSessionRefreshPolicySetting />);
    expect(testState.mutationOptions?.onSuccess).toBeTypeOf("function");

    await testState.mutationOptions!.onSuccess!();

    expect(invalidateAllProductFeatures).toHaveBeenCalledOnce();
    expect(toastSuccess).toHaveBeenCalledOnce();
  });

  it("is read-only for organization members", () => {
    testState.isAdmin = false;
    render(<RemoteSessionRefreshPolicySetting />);

    const fieldset = screen
      .getByLabelText("Automatic session refresh policy")
      .closest("fieldset");
    expect((fieldset as HTMLFieldSetElement).disabled).toBe(true);

    fireEvent.click(screen.getByLabelText("Required"));
    expect(testState.mutate).not.toHaveBeenCalled();
  });
});
