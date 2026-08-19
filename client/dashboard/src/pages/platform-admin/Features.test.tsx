import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import PlatformAdminFeatures from "./Features";
import type { ReactNode } from "react";
import { queryKeyChatAnalysisSettings } from "@gram/client/react-query/chatAnalysisSettings.js";
import { queryKeyProductFeatures } from "@gram/client/react-query/productFeatures.js";

type ProductFeatureMutationState = {
  error: Error | null;
  isPending: boolean;
  variables?: {
    request: {
      setProductFeatureRequestBody: {
        featureName: string;
        organizationId: string;
      };
    };
  };
};

const invalidateAllProductFeatures = vi.hoisted(() => vi.fn());

const mocks = vi.hoisted(() => ({
  businessMemoryError: null as Error | null,
  businessMemoryMutate: vi.fn(),
  organizationId: "org-target",
  productFeatureMutate: vi.fn(),
  productFeatureMutationInstances: [] as {
    completeError: (error: Error) => void;
    completeSuccess: () => Promise<void>;
    organizationId: string;
  }[],
  productFeaturesQuery: vi.fn(),
  query: vi.fn(),
  triggerMutate: vi.fn(),
  workUnitsMutate: vi.fn(),
  workUnitsPending: false,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.organizationId }),
}));
vi.mock("./AdminSection", () => ({
  AdminSection: ({ children }: { children: ReactNode }) => <>{children}</>,
  AdminRow: ({
    action,
    children,
  }: {
    action?: ReactNode;
    children?: ReactNode;
  }) => (
    <div>
      {action}
      {children}
    </div>
  ),
}));
vi.mock("./PlatformAdminGate", () => ({
  PlatformAdminGate: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <>{children}</>;
  }
  function Header({ children }: { children?: ReactNode }) {
    return <>{children}</>;
  }
  Header.Breadcrumbs = () => null;
  function Section({ children }: { children: ReactNode }) {
    return <>{children}</>;
  }
  Section.Title = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Description = ({ children }: { children: ReactNode }) => (
    <>{children}</>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Page.Header = Header;
  Page.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Page.Section = Section;
  return { Page };
});
vi.mock(
  "@gram/client/react-query/chatAnalysisSettings.js",
  async (original) => ({
    ...(await original<
      typeof import("@gram/client/react-query/chatAnalysisSettings.js")
    >()),
    invalidateAllChatAnalysisSettings: vi.fn(),
    useChatAnalysisSettings: mocks.query,
  }),
);
vi.mock("@gram/client/react-query/upsertChatAnalysisSettings.js", async () => {
  const { useState } = await import("react");
  return {
    useUpsertChatAnalysisSettingsMutation: () =>
      useState(() => ({
        error: null,
        isPending: mocks.workUnitsPending,
        mutate: mocks.workUnitsMutate,
      }))[0],
  };
});
vi.mock(
  "@gram/client/react-query/upsertBusinessMemoryAnalysisSettings.js",
  async () => {
    const { useState } = await import("react");
    return {
      useUpsertBusinessMemoryAnalysisSettingsMutation: () =>
        useState(() => ({
          error: mocks.businessMemoryError,
          isPending: false,
          mutate: mocks.businessMemoryMutate,
        }))[0],
    };
  },
);
vi.mock("@gram/client/react-query/triggerChatAnalysis.js", () => ({
  useTriggerChatAnalysisMutation: () => ({
    isPending: false,
    mutate: mocks.triggerMutate,
  }),
}));
vi.mock(
  "@gram/client/react-query/productFeatures.js",
  async (importOriginal) => ({
    ...(await importOriginal<
      typeof import("@gram/client/react-query/productFeatures.js")
    >()),
    invalidateAllProductFeatures,
    useProductFeatures: mocks.productFeaturesQuery,
  }),
);
vi.mock("@gram/client/react-query/featuresSet.js", async () => {
  const { useState } = await import("react");
  return {
    useFeaturesSetMutation: (options: {
      onError?: (error: Error) => void;
      onSuccess?: () => void | Promise<void>;
    }) => {
      const [state, setState] = useState<ProductFeatureMutationState>(() =>
        mocks.organizationId === "org-a"
          ? {
              error: new Error("A update failed"),
              isPending: true,
              variables: {
                request: {
                  setProductFeatureRequestBody: {
                    featureName: "authz_challenge_logging",
                    organizationId: "org-a",
                  },
                },
              },
            }
          : { error: null, isPending: false, variables: undefined },
      );
      useState(() => {
        const instance = {
          organizationId: mocks.organizationId,
          completeSuccess: async () => {
            setState({ error: null, isPending: false, variables: undefined });
            await options.onSuccess?.();
          },
          completeError: (error: Error) => {
            setState((current) => ({ ...current, error, isPending: false }));
            options.onError?.(error);
          },
        };
        mocks.productFeatureMutationInstances.push(instance);
        return instance;
      });
      return { ...state, mutate: mocks.productFeatureMutate };
    },
  };
});
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "disabled" }),
}));
vi.mock("@/hooks/useOrgMemoryDeveloperToggle", () => ({
  useOrgMemoryDeveloperToggle: () => [false, vi.fn()],
}));
vi.mock("@tanstack/react-query", async (original) => ({
  ...(await original<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.businessMemoryError = null;
  mocks.organizationId = "org-target";
  mocks.productFeatureMutate.mockReset();
  mocks.productFeatureMutationInstances = [];
  mocks.productFeaturesQuery.mockReset();
  mocks.productFeaturesQuery.mockReturnValue({
    data: null,
    error: null,
    isLoading: true,
  });
  mocks.workUnitsPending = false;
});

afterEach(cleanup);

describe("PlatformAdminFeatures", () => {
  it("scopes every chat analysis request to the selected organization", () => {
    mocks.query.mockReturnValue({
      data: {
        businessMemoryDailyCap: 20,
        businessMemoryEnabled: true,
        workUnitsDailyCap: 10,
        workUnitsEnabled: true,
      },
      error: null,
      isLoading: false,
    });

    render(<PlatformAdminFeatures />);

    expect(mocks.query).toHaveBeenCalledTimes(2);
    for (const call of mocks.query.mock.calls) {
      expect(call[0]).toEqual({ organizationId: "org-target" });
    }

    const disableButtons = screen.getAllByRole("button", { name: "Disable" });
    fireEvent.click(disableButtons[0]!);
    fireEvent.click(disableButtons[1]!);
    fireEvent.click(screen.getAllByRole("button", { name: "Run now" })[0]!);

    expect(mocks.workUnitsMutate).toHaveBeenCalledWith(
      {
        request: {
          upsertWorkUnitsSettingsRequestBody: {
            organizationId: "org-target",
            workUnitsDailyCap: 10,
            workUnitsEnabled: false,
          },
        },
      },
      expect.anything(),
    );
    expect(mocks.businessMemoryMutate).toHaveBeenCalledWith(
      {
        request: {
          upsertBusinessMemorySettingsRequestBody: {
            businessMemoryDailyCap: 20,
            businessMemoryEnabled: false,
            organizationId: "org-target",
          },
        },
      },
      expect.anything(),
    );
    expect(mocks.triggerMutate).toHaveBeenCalledWith({
      request: {
        triggerAnalysisRequestBody: { organizationId: "org-target" },
      },
    });
  });

  it("discards an edited cap when the selected organization changes", () => {
    mocks.organizationId = "org-a";
    mocks.query.mockImplementation(({ organizationId }) => ({
      data: {
        businessMemoryDailyCap: organizationId === "org-a" ? 20 : 21,
        businessMemoryEnabled: true,
        workUnitsDailyCap: organizationId === "org-a" ? 10 : 11,
        workUnitsEnabled: true,
      },
      error: null,
      isLoading: false,
    }));

    const { rerender } = render(<PlatformAdminFeatures />);
    fireEvent.change(
      screen.getByRole("textbox", {
        name: "Work delivered daily evaluation cap",
      }),
      { target: { value: "999" } },
    );

    mocks.organizationId = "org-b";
    rerender(<PlatformAdminFeatures />);

    const capInput = screen.getByRole("textbox", {
      name: "Work delivered daily evaluation cap",
    });
    expect((capInput as HTMLInputElement).value).toBe("11");
    fireEvent.click(screen.getAllByRole("button", { name: "Disable" })[0]!);

    expect(mocks.workUnitsMutate).toHaveBeenCalledWith(
      {
        request: {
          upsertWorkUnitsSettingsRequestBody: {
            organizationId: "org-b",
            workUnitsDailyCap: 11,
            workUnitsEnabled: false,
          },
        },
      },
      expect.anything(),
    );
  });

  it("discards mutation state when the selected organization changes", () => {
    mocks.organizationId = "org-a";
    mocks.workUnitsPending = true;
    mocks.businessMemoryError = new Error("Update failed");
    mocks.query.mockReturnValue({
      data: {
        businessMemoryDailyCap: 20,
        businessMemoryEnabled: true,
        workUnitsDailyCap: 10,
        workUnitsEnabled: true,
      },
      error: null,
      isLoading: false,
    });

    const { rerender } = render(<PlatformAdminFeatures />);
    expect(
      (
        screen.getAllByRole("button", {
          name: "Disable",
        })[0] as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(screen.getByText("Update failed")).toBeTruthy();

    mocks.organizationId = "org-b";
    mocks.workUnitsPending = false;
    mocks.businessMemoryError = null;
    rerender(<PlatformAdminFeatures />);

    expect(
      (
        screen.getAllByRole("button", {
          name: "Disable",
        })[0] as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    expect(screen.queryByText("Update failed")).toBeNull();
  });

  it("scopes product-feature reads and writes to the selected organization", () => {
    mocks.productFeaturesQuery.mockReturnValue({
      data: {},
      error: null,
      isLoading: false,
    });

    render(<PlatformAdminFeatures />);

    expect(mocks.productFeaturesQuery).toHaveBeenCalledWith({
      organizationId: "org-target",
    });
    fireEvent.click(
      screen.getByRole("switch", { name: "Toggle Authz Challenge Logging" }),
    );
    expect(mocks.productFeatureMutate).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          enabled: true,
          featureName: "authz_challenge_logging",
          organizationId: "org-target",
        },
      },
    });
  });

  it("isolates pending product-feature mutation completions across an organization switch", async () => {
    mocks.organizationId = "org-a";
    mocks.productFeaturesQuery.mockReturnValue({
      data: {},
      error: null,
      isLoading: false,
    });

    const { rerender } = render(<PlatformAdminFeatures />);
    const orgAMutation = mocks.productFeatureMutationInstances[0];
    const toggle = () =>
      screen.getByRole("switch", { name: "Toggle Authz Challenge Logging" });

    expect(orgAMutation?.organizationId).toBe("org-a");
    expect((toggle() as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("A update failed")).toBeTruthy();

    mocks.organizationId = "org-b";
    rerender(<PlatformAdminFeatures />);

    expect(mocks.productFeatureMutationInstances).toHaveLength(2);
    expect(mocks.productFeatureMutationInstances[1]?.organizationId).toBe(
      "org-b",
    );
    expect((toggle() as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByText("A update failed")).toBeNull();
    expect(mocks.productFeaturesQuery).toHaveBeenLastCalledWith({
      organizationId: "org-b",
    });

    await act(async () => {
      await orgAMutation?.completeSuccess();
      orgAMutation?.completeError(new Error("Late A failure"));
    });

    expect(invalidateAllProductFeatures).not.toHaveBeenCalled();
    expect((toggle() as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByText("A update failed")).toBeNull();
    expect(screen.queryByText("Late A failure")).toBeNull();
  });

  it("includes organization parameters in generated product-feature query keys", () => {
    const first = queryKeyProductFeatures({ organizationId: "org-a" });
    const second = queryKeyProductFeatures({ organizationId: "org-b" });
    expect(first).not.toEqual(second);
    expect(first).toEqual([
      "@gram/client",
      "features",
      "get",
      { organizationId: "org-a" },
    ]);
  });

  it("includes organization scope in the generated settings query key", () => {
    expect(
      queryKeyChatAnalysisSettings({ organizationId: "org-target" }),
    ).toEqual([
      "@gram/client",
      "adminChatAnalysis",
      "getSettings",
      { organizationId: "org-target" },
    ]);
  });
});
