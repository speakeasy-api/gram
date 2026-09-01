import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  data: { logsEnabled: false, toolIoLogsEnabled: false } as
    | { logsEnabled: boolean; toolIoLogsEnabled: boolean }
    | undefined,
  organizationId: "org-active",
  mutate: vi.fn(),
  mutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: (
          data: unknown,
          variables: {
            request: {
              setProductFeatureRequestBody: {
                featureName: string;
                enabled: boolean;
              };
            };
          },
        ) => Promise<void>;
      }
    | undefined,
  productFeaturesQuery: vi.fn(),
  isAdmin: true,
}));

const handleAPIError = vi.hoisted(() => vi.fn());
const invalidateAllProductFeatures = vi.hoisted(() => vi.fn());

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) =>
    testState.isAdmin ? <>{children}</> : null,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: testState.organizationId }),
}));

vi.mock("@/lib/errors", () => ({ handleAPIError }));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: (options: {
    onError?: (error: Error) => void;
    onSuccess?: (
      data: unknown,
      variables: {
        request: {
          setProductFeatureRequestBody: {
            featureName: string;
            enabled: boolean;
          };
        };
      },
    ) => Promise<void>;
  }) => {
    testState.mutationOptions = options;
    return { isPending: false, mutate: testState.mutate };
  },
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures,
  useProductFeatures: (...args: unknown[]) => {
    testState.productFeaturesQuery(...args);
    return { data: testState.data };
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

import { EnableLogsSetting } from "./EnableLogsSetting";

beforeEach(() => {
  testState.data = { logsEnabled: false, toolIoLogsEnabled: false };
  testState.organizationId = "org-active";
  testState.isAdmin = true;
  testState.mutate.mockReset();
  testState.mutationOptions = undefined;
  testState.productFeaturesQuery.mockReset();
  handleAPIError.mockReset();
  invalidateAllProductFeatures.mockReset();
  invalidateAllProductFeatures.mockResolvedValue(undefined);
});

afterEach(cleanup);

describe("EnableLogsSetting", () => {
  it("enables the same logs product feature as Logging & Telemetry", () => {
    render(<EnableLogsSetting />);

    expect(testState.productFeaturesQuery).toHaveBeenCalledWith(
      { organizationId: "org-active" },
      undefined,
      expect.anything(),
    );

    fireEvent.click(screen.getByRole("switch", { name: "Enable logs" }));

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

  it("turns off tool I/O logs when logging is disabled", () => {
    testState.data = { logsEnabled: true, toolIoLogsEnabled: true };

    render(<EnableLogsSetting />);
    fireEvent.click(screen.getByRole("switch", { name: "Enable logs" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          organizationId: "org-active",
          featureName: "logs",
          enabled: false,
        },
      },
    });
    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          organizationId: "org-active",
          featureName: "tool_io_logs",
          enabled: false,
        },
      },
    });
  });

  it("notifies the parent after a successful enable", async () => {
    const onEnabledChange = vi.fn();
    render(
      <EnableLogsSetting
        onEnabledChange={(enabled) => {
          onEnabledChange(enabled);
        }}
      />,
    );

    await testState.mutationOptions!.onSuccess!(undefined, {
      request: {
        setProductFeatureRequestBody: {
          featureName: "logs",
          enabled: true,
        },
      },
    });

    expect(onEnabledChange).toHaveBeenCalledWith(true);
    expect(invalidateAllProductFeatures).toHaveBeenCalledOnce();
  });

  it("ignores a deferred mutation completion after an organization switch", async () => {
    const onEnabledChange = vi.fn();
    const notifyEnabled = (enabled: boolean) => {
      onEnabledChange(enabled);
    };
    const { rerender } = render(
      <EnableLogsSetting onEnabledChange={notifyEnabled} />,
    );
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    testState.organizationId = "org-next";
    rerender(<EnableLogsSetting onEnabledChange={notifyEnabled} />);
    await activeMutation!.onSuccess!(undefined, {
      request: {
        setProductFeatureRequestBody: {
          featureName: "logs",
          enabled: true,
        },
      },
    });

    expect(onEnabledChange).not.toHaveBeenCalled();
    expect(invalidateAllProductFeatures).not.toHaveBeenCalled();
  });

  it("ignores a stale mutation error after an organization switch", () => {
    const { rerender } = render(<EnableLogsSetting />);
    const activeMutation = testState.mutationOptions;
    expect(activeMutation?.onError).toBeTypeOf("function");

    testState.organizationId = "org-next";
    rerender(<EnableLogsSetting />);
    activeMutation!.onError!(new Error("stale failure"));

    expect(handleAPIError).not.toHaveBeenCalled();
  });

  it("does not render the switch for non-admins", () => {
    testState.isAdmin = false;

    render(<EnableLogsSetting />);

    expect(screen.queryByRole("switch", { name: "Enable logs" })).toBeNull();
  });
});
