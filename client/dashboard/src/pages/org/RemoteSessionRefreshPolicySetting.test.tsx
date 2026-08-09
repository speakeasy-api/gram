import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  isAdmin: true,
  mutate: vi.fn(),
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

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: (props: { disabled: boolean }) => React.ReactNode;
  }) => children({ disabled: !testState.isAdmin }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures,
  useProductFeatures: () => ({
    data: testState.features,
    error: null,
    isLoading: false,
    refetch: vi.fn(),
  }),
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
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { RemoteSessionRefreshPolicySetting } from "./RemoteSessionRefreshPolicySetting";

beforeEach(() => {
  testState.isAdmin = true;
  testState.mutate.mockReset();
  testState.mutationOptions = undefined;
  testState.features.remoteSessionAutoRefreshEnabled = true;
  testState.features.remoteSessionAutoRefreshEnforcedEnabled = false;
  invalidateAllProductFeatures.mockReset();
});

afterEach(cleanup);

describe("RemoteSessionRefreshPolicySetting", () => {
  it("shows the effective organization policy", () => {
    render(<RemoteSessionRefreshPolicySetting />);

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

  it("lets organization admins require automatic refresh", () => {
    render(<RemoteSessionRefreshPolicySetting />);

    fireEvent.click(screen.getByLabelText("Required"));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setRemoteSessionAutoRefreshPolicyRequestBody: {
          policy: "enforced",
        },
      },
    });
  });

  it("refreshes product features after saving", async () => {
    render(<RemoteSessionRefreshPolicySetting />);

    await testState.mutationOptions?.onSuccess?.();

    expect(invalidateAllProductFeatures).toHaveBeenCalledOnce();
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
